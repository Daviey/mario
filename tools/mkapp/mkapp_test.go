package main

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"image/png"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeMacho builds a minimal little-endian 64-bit Mach-O header + filler
// so writeFat/checkArch exercise real slice sizes without a toolchain.
func fakeMacho(cputype uint32, size int) []byte {
	b := make([]byte, size)
	copy(b, machoMagic64[:])
	binary.LittleEndian.PutUint32(b[4:8], cputype)
	for i := 8; i < size; i++ {
		b[i] = byte(i*7 + i/251)
	}
	return b
}

func TestShortVersion(t *testing.T) {
	cases := []struct{ in, want string }{
		{"v0.3.3", "0.3.3"},
		{"v0.3.3-6-g84d833b", "0.3.3"},
		{"v0.3.3-6-g84d833b-dirty", "0.3.3"},
		{"0.4.0", "0.4.0"},
		{"dev", "0.0.0"},
		{"", "0.0.0"},
	}
	for _, c := range cases {
		if got := shortVersion(c.in); got != c.want {
			t.Errorf("shortVersion(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCheckArch(t *testing.T) {
	if err := checkArch("amd64", fakeMacho(archAMD64.cputype, 64), archAMD64); err != nil {
		t.Errorf("matching cputype rejected: %v", err)
	}
	if err := checkArch("amd64", fakeMacho(archARM64.cputype, 64), archAMD64); err == nil {
		t.Error("swapped slice accepted")
	}
	if err := checkArch("amd64", []byte("ELF\x00\x00\x00\x00\x00"), archAMD64); err == nil {
		t.Error("non-Mach-O slice accepted")
	}
	if err := checkArch("short", machoMagic64[:], archAMD64); err == nil {
		t.Error("truncated slice accepted")
	}
}

// TestFatRoundTrip parses the fat container back and checks it against
// <mach-o/fat.h>: big-endian header + records, slices byte-identical to
// the inputs, offsets aligned to 2^align and consistent with sizes.
func TestFatRoundTrip(t *testing.T) {
	amd64 := fakeMacho(archAMD64.cputype, 5000) // crosses the 4 KiB boundary
	arm64 := fakeMacho(archARM64.cputype, 300)

	var buf bytes.Buffer
	if err := writeFat(&buf, amd64, arm64); err != nil {
		t.Fatal(err)
	}
	d := buf.Bytes()

	if m := binary.BigEndian.Uint32(d[0:4]); m != 0xcafebabe {
		t.Fatalf("fat magic = %#x, want 0xcafebabe", m)
	}
	if d[0] != 0xca || d[1] != 0xfe || d[2] != 0xba || d[3] != 0xbe {
		t.Error("fat magic not stored big-endian")
	}
	if n := binary.BigEndian.Uint32(d[4:8]); n != 2 {
		t.Fatalf("nfat_arch = %d, want 2", n)
	}

	rec := d[8:28] // first fat_arch: x86_64
	if ct := binary.BigEndian.Uint32(rec[0:4]); ct != 0x01000007 {
		t.Errorf("x86_64 cputype = %#x, want 0x01000007", ct)
	}
	if st := binary.BigEndian.Uint32(rec[4:8]); st != 3 {
		t.Errorf("x86_64 cpusubtype = %d, want 3", st)
	}
	if al := binary.BigEndian.Uint32(rec[16:20]); al != 12 {
		t.Errorf("x86_64 align = %d, want 12", al)
	}
	off1 := binary.BigEndian.Uint32(rec[8:12])
	sz1 := binary.BigEndian.Uint32(rec[12:16])
	if sz1 != uint32(len(amd64)) {
		t.Errorf("x86_64 size = %d, want %d", sz1, len(amd64))
	}
	if off1%4096 != 0 || off1 < 48 {
		t.Errorf("x86_64 offset = %d, want 4 KiB-aligned and past the header", off1)
	}
	if !bytes.Equal(d[off1:off1+sz1], amd64) {
		t.Error("x86_64 slice differs from the input binary")
	}

	rec = d[28:48] // second fat_arch: arm64
	if ct := binary.BigEndian.Uint32(rec[0:4]); ct != 0x0100000C {
		t.Errorf("arm64 cputype = %#x, want 0x0100000c", ct)
	}
	if st := binary.BigEndian.Uint32(rec[4:8]); st != 0 {
		t.Errorf("arm64 cpusubtype = %d, want 0", st)
	}
	if al := binary.BigEndian.Uint32(rec[16:20]); al != 14 {
		t.Errorf("arm64 align = %d, want 14", al)
	}
	off2 := binary.BigEndian.Uint32(rec[8:12])
	sz2 := binary.BigEndian.Uint32(rec[12:16])
	if sz2 != uint32(len(arm64)) {
		t.Errorf("arm64 size = %d, want %d", sz2, len(arm64))
	}
	if off2%16384 != 0 || off2 < off1+sz1 {
		t.Errorf("arm64 offset = %d, want 16 KiB-aligned and past the x86_64 slice", off2)
	}
	if !bytes.Equal(d[off2:off2+sz2], arm64) {
		t.Error("arm64 slice differs from the input binary")
	}

	if len(d) != int(off2+sz2) {
		t.Errorf("file length = %d, want %d (no trailing bytes)", len(d), off2+sz2)
	}
	for _, pad := range [][]byte{d[48:off1], d[off1+sz1 : off2]} {
		for i, b := range pad {
			if b != 0 {
				t.Errorf("alignment padding byte %d = %#x, want 0", i, b)
			}
		}
	}
}

// TestIcnsStructure checks the icns container: magic + big-endian total
// size, every chunk type present in order, chunk lengths consistent and
// each payload a PNG of the pixel size the OSType promises.
func TestIcnsStructure(t *testing.T) {
	d := buildIcns()
	if !bytes.HasPrefix(d, []byte("icns")) {
		t.Fatalf("icns magic missing: %q", d[:4])
	}
	if total := binary.BigEndian.Uint32(d[4:8]); total != uint32(len(d)) {
		t.Errorf("icns total size = %d, want %d", total, len(d))
	}

	seen := map[string]int{}
	pos := 8
	for _, want := range icnsTypes {
		if pos+8 > len(d) {
			t.Fatalf("truncated at chunk %d", pos)
		}
		typ := string(d[pos : pos+4])
		size := binary.BigEndian.Uint32(d[pos+4 : pos+8])
		if typ != want.typ {
			t.Errorf("chunk type = %q, want %q", typ, want.typ)
		}
		body := d[pos+8 : pos+int(size)]
		if int(size) != len(body)+8 {
			t.Errorf("chunk %s length %d does not cover its %d payload bytes", typ, size, len(body))
		}
		if !bytes.HasPrefix(body, []byte("\x89PNG\r\n\x1a\n")) {
			t.Errorf("chunk %s payload is not a PNG", typ)
		}
		img, err := png.Decode(bytes.NewReader(body))
		if err != nil {
			t.Fatalf("chunk %s PNG undecodable: %v", typ, err)
		}
		if got := img.Bounds().Dx(); got != want.px {
			t.Errorf("chunk %s PNG is %dpx wide, want %d", typ, got, want.px)
		}
		if got := img.Bounds().Dy(); got != want.px {
			t.Errorf("chunk %s PNG is %dpx tall, want %d", typ, got, want.px)
		}
		seen[typ]++
		pos += int(size)
	}
	if pos != len(d) {
		t.Errorf("icns has %d trailing bytes", len(d)-pos)
	}
	if len(seen) != len(icnsTypes) {
		t.Errorf("duplicate chunk types: %v", seen)
	}
}

func TestInfoPlist(t *testing.T) {
	p := string(infoPlist("v0.3.3-6-g84d833b"))
	for _, want := range []string{
		"<key>CFBundleExecutable</key>\n\t<string>mario</string>",
		"<key>CFBundleIdentifier</key>\n\t<string>com.daviey.mario</string>",
		"<key>CFBundleName</key>\n\t<string>Mario</string>",
		"<key>CFBundlePackageType</key>\n\t<string>APPL</string>",
		"<key>CFBundleIconFile</key>\n\t<string>Mario</string>",
		"<key>LSMinimumSystemVersion</key>\n\t<string>10.13</string>",
		"<key>LSApplicationCategoryType</key>\n\t<string>public.app-category.action-games</string>",
		"<key>CFBundleShortVersionString</key>\n\t<string>0.3.3</string>",
		"<key>CFBundleVersion</key>\n\t<string>0.3.3-6-g84d833b</string>",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("Info.plist missing %q", want)
		}
	}
	// The bare-tag case: leading v stripped, both fields agree.
	p = string(infoPlist("v1.2.3"))
	if !strings.Contains(p, "<key>CFBundleVersion</key>\n\t<string>1.2.3</string>") {
		t.Error("Info.plist CFBundleVersion does not strip the leading v")
	}
}

// TestAppZipDeterministic runs the full assemble+zip pipeline twice from
// scratch and byte-compares, then parses the zip back: exact entry set,
// the exec bit on Contents/MacOS/mario, fixed epoch mtimes and the
// version inside the bundled Info.plist.
func TestAppZipDeterministic(t *testing.T) {
	fat := fakeMacho(archAMD64.cputype, 5000)

	buildZip := func() []byte {
		staging := t.TempDir()
		if err := assemble(filepath.Join(staging, "Mario.app"), fat, "v0.3.3-6-g84d833b"); err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		if err := zipDir(staging, zw); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		return buf.Bytes()
	}
	a, b := buildZip(), buildZip()
	if !bytes.Equal(a, b) {
		t.Error("zip is not byte-deterministic across builds")
	}

	zr, err := zip.NewReader(bytes.NewReader(a), int64(len(a)))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]struct {
		mode uint32
	}{
		"Mario.app/Contents/Info.plist":           {0o644},
		"Mario.app/Contents/MacOS/mario":          {0o755},
		"Mario.app/Contents/PkgInfo":              {0o644},
		"Mario.app/Contents/Resources/Mario.icns": {0o644},
	}
	if len(zr.File) != len(want) {
		t.Fatalf("zip has %d entries, want %d", len(zr.File), len(want))
	}
	for _, f := range zr.File {
		w, ok := want[f.Name]
		if !ok {
			t.Errorf("unexpected zip entry %q", f.Name)
			continue
		}
		delete(want, f.Name)
		if got := f.Mode().Perm(); uint32(got) != w.mode {
			t.Errorf("entry %q mode = %v, want %#o", f.Name, got, w.mode)
		}
		if !f.Modified.Equal(zipEpoch) {
			t.Errorf("entry %q has non-epoch mtime %v", f.Name, f.Modified)
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		switch f.Name {
		case "Mario.app/Contents/MacOS/mario":
			if !bytes.Equal(content, fat) {
				t.Error("bundled binary differs from the fat input")
			}
		case "Mario.app/Contents/Info.plist":
			if !strings.Contains(string(content), "<string>0.3.3-6-g84d833b</string>") {
				t.Error("bundled Info.plist is missing the version")
			}
		case "Mario.app/Contents/Resources/Mario.icns":
			if !bytes.HasPrefix(content, []byte("icns")) {
				t.Error("bundled icon is not an icns")
			}
		case "Mario.app/Contents/PkgInfo":
			if string(content) != "APPL????" {
				t.Errorf("PkgInfo = %q", content)
			}
		}
	}
	for name := range want {
		t.Errorf("missing zip entry %q", name)
	}
}

// TestZipDirNormalizesModes pins the archive's umask-independence: the
// runner builds with umask 077, and mirroring staged perms leaked 0600
// plists and 0700 binaries into the zip. Whatever the tree on disk
// carries, entries are 0644 — 0755 for executables.
func TestZipDirNormalizesModes(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, mode os.FileMode) {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o666); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, mode); err != nil {
			t.Fatal(err)
		}
	}
	write("plain", 0o600)
	write("exec", 0o700)
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if err := zipDir(dir, zw); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		want := fs.FileMode(0o644)
		if f.Name == "exec" {
			want = 0o755
		}
		if got := f.Mode().Perm(); got != want {
			t.Errorf("entry %q mode = %#o, want %#o", f.Name, got, want)
		}
	}
}
