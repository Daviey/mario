package pack

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGzipHeader(t *testing.T) {
	b := Gzip([]byte("payload"), gzip.BestCompression)
	// RFC 1952 header: magic, deflate, then FLG and MTIME. FLG and
	// MTIME must be zero — a name, comment or timestamp would make the
	// same input compress differently on every machine and at every
	// hour, breaking the byte-determinism contract.
	if !bytes.HasPrefix(b, []byte{0x1f, 0x8b}) {
		t.Errorf("gzip magic = % x, want 1f 8b", b[:2])
	}
	if b[2] != 8 { // CM: deflate (RFC 1952)
		t.Errorf("gzip CM = %d, want 8 (deflate)", b[2])
	}
	if b[3] != 0 {
		t.Errorf("gzip FLG = %#x, want 0 (no name/extra/comment)", b[3])
	}
	if !bytes.Equal(b[4:8], []byte{0, 0, 0, 0}) {
		t.Errorf("gzip MTIME = % x, want 00 00 00 00", b[4:8])
	}
}

func TestShortVersion(t *testing.T) {
	cases := []struct{ in, want string }{
		{"v0.3.3", "0.3.3"},
		{"v0.3.3-6-g84d833b", "0.3.3"},
		{"v0.3.3-6-g84d833b-dirty", "0.3.3"},
		{"0.4.0", "0.4.0"},
		{"v1.2", "0.0.0"}, // not X.Y.Z
		{"dev", "0.0.0"},  // git describe fallback
		{"", "0.0.0"},
	}
	for _, c := range cases {
		if got := ShortVersion(c.in); got != c.want {
			t.Errorf("shortVersion(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestSanitizeVersion(t *testing.T) {
	ok := map[string]string{
		"v0.3.0":                  "0.3.0",
		"0.3.0":                   "0.3.0",
		"0.3.0-dirty":             "0.3.0+dirty",
		"0.3.0-14-g1a2b3c4":       "0.3.0+14.g1a2b3c4",
		"0.3.0-14-g1a2b3c4-dirty": "0.3.0+14.g1a2b3c4+dirty",
	}
	for in, want := range ok {
		// Both package kinds share the mapping; only the error message
		// differs, and it must name the format that rejected.
		for _, kind := range []string{"Debian", "RPM"} {
			got, err := SanitizeVersion(in, kind)
			if err != nil || got != want {
				t.Errorf("sanitizeVersion(%q, %q) = %q, %v; want %q", in, kind, got, err, want)
			}
		}
	}
	for _, bad := range []string{"", "abc", "v", "1 2", "1:2.0", "1.2-3"} {
		_, err := SanitizeVersion(bad, "Debian")
		if err == nil {
			t.Errorf("sanitizeVersion(%q) accepted", bad)
			continue
		}
		if !strings.Contains(err.Error(), "Debian") {
			t.Errorf("sanitizeVersion(%q) error does not name the format kind: %v", bad, err)
		}
	}
}

func TestZipDirOrderAndEpoch(t *testing.T) {
	dir := t.TempDir()
	files := map[string]fs.FileMode{
		"z.txt":       0o600,
		"a/b/y.js":    0o755,
		"a/x.html":    0o644,
		"a/b/c/w.png": 0o444,
	}
	for name, mode := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
		// Chmod after create: the WriteFile mode is umask-masked and
		// would make the fixture host-dependent.
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
	}

	normalize := func(perms fs.FileMode) fs.FileMode {
		if perms&0o111 != 0 {
			return 0o755
		}
		return 0o644
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if err := ZipDir(dir, zw, normalize); err != nil {
		t.Fatalf("ZipDir: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a/b/c/w.png", "a/b/y.js", "a/x.html", "z.txt"}
	if len(zr.File) != len(want) {
		t.Fatalf("zip has %d entries, want %d (directories are omitted)", len(zr.File), len(want))
	}
	for i, f := range zr.File {
		if f.Name != want[i] {
			t.Errorf("entry %d = %q, want %q (lexicographic order)", i, f.Name, want[i])
		}
		if !f.Modified.Equal(ZipEpoch) {
			t.Errorf("entry %q mtime = %v, want the fixed ZipEpoch", f.Name, f.Modified)
		}
		wantMode := fs.FileMode(0o644)
		if files[f.Name]&0o111 != 0 {
			wantMode = 0o755
		}
		if got := f.Mode().Perm(); got != wantMode {
			t.Errorf("entry %q mode = %o, want %o (modeFor applied)", f.Name, got, wantMode)
		}
	}
}

func TestZipDirSymlink(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "real.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real.txt", filepath.Join(dir, "link.txt")); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	err := ZipDir(dir, zw, func(p fs.FileMode) fs.FileMode { return p })
	if err == nil {
		zw.Close()
		t.Fatal("ZipDir followed the symlink instead of rejecting it")
	}
	if !strings.Contains(err.Error(), "link.txt") {
		t.Errorf("error does not name the offending path: %v", err)
	}
}

func TestWriteNewcEntry(t *testing.T) {
	var buf bytes.Buffer
	WriteNewcEntry(&buf, NewcEntry{
		Name: "dev/console", Mode: 0o020000 | 0o0600,
		Ino: 2, Nlink: 1, RdevMajor: 5, RdevMinor: 1,
	})
	WriteNewcEntry(&buf, NewcEntry{
		Name: "init", Mode: 0o100755, Ino: 4, Nlink: 1,
		Size: 5, Data: []byte("HELLO"),
	})
	b := buf.Bytes()

	// hexField reads header field i at offset off; 6-byte magic +
	// 13 fixed-width fields = the 110-byte newc header.
	hexField := func(off, i int) string { return string(b[off+6+8*i : off+6+8*(i+1)]) }

	if string(b[:6]) != "070701" {
		t.Fatalf("magic = %q, want 070701", b[:6])
	}
	fields := []struct {
		i    int
		want string
	}{
		{0, "00000002"},  // c_ino
		{1, "00002180"},  // c_mode: S_IFCHR|0600 = 0x2180, MSB first
		{2, "00000000"},  // c_uid
		{3, "00000000"},  // c_gid
		{4, "00000001"},  // c_nlink
		{5, "00000000"},  // c_mtime
		{9, "00000005"},  // c_rdevmajor
		{10, "00000001"}, // c_rdevminor
		{11, "0000000c"}, // c_namesize: len("dev/console") + NUL
		{12, "00000000"}, // c_check
	}
	for _, f := range fields {
		if got := hexField(0, f.i); got != f.want {
			t.Errorf("field %d = %s, want %s", f.i, got, f.want)
		}
	}

	// name + NUL, then NUL padding to the next 4-byte boundary; the
	// second header starts aligned there.
	if string(b[110:121]) != "dev/console" || b[121] != 0 {
		t.Errorf("name = %q, want \"dev/console\" + NUL", b[110:122])
	}
	if b[122] != 0 || b[123] != 0 {
		t.Errorf("name padding = % x, want 00 00", b[122:124])
	}
	if string(b[124:130]) != "070701" {
		t.Errorf("second header starts at 124 with %q, want 070701", b[124:130])
	}
	if got := hexField(124, 1); got != "000081ed" { // S_IFREG|0755 = 0x81ed
		t.Errorf("second c_mode = %s, want 000081ed", got)
	}
	if got := hexField(124, 6); got != "00000005" {
		t.Errorf("second c_filesize = %s, want 00000005", got)
	}
	if got := hexField(124, 11); got != "00000005" {
		t.Errorf("second c_namesize = %s, want 00000005", got)
	}

	// second entry: header at 124, so name at 234 ("init" + NUL + one
	// pad byte), data at 240, padded to 248.
	if string(b[234:238]) != "init" || b[238] != 0 || b[239] != 0 {
		t.Errorf("name region = %q, want \"init\" + NUL + pad", b[234:240])
	}
	if string(b[240:245]) != "HELLO" {
		t.Errorf("data = %q, want HELLO", b[240:245])
	}
	if !bytes.Equal(b[245:248], []byte{0, 0, 0}) {
		t.Errorf("data padding = % x, want 00 00 00", b[245:248])
	}
	if len(b) != 248 {
		t.Errorf("archive length = %d, want 248", len(b))
	}
}

func TestParseInputs(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "pkg")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"mario.6":       "man",
		"mario.desktop": "desktop",
		"copyright":     "copyright",
	} {
		if err := os.WriteFile(filepath.Join(pkg, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(dir, "mario-bin")
	if err := os.WriteFile(bin, []byte("FAKE-ELF-BODY"), 0o755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "mario.pkg")

	in, err := ParseInputs("arch help", "out help",
		"-version", "v0.3.0", "-arch", "amd64", "-bin", bin, "-pkgdir", pkg, "-out", out)
	if err != nil {
		t.Fatalf("ParseInputs: %v", err)
	}
	if in.Version != "v0.3.0" || in.Arch != "amd64" || in.Bin != bin || in.Pkgdir != pkg || in.Out != out {
		t.Errorf("flag fields = %+v", in)
	}
	if string(in.Game) != "FAKE-ELF-BODY" || string(in.Man) != "man" ||
		string(in.Desktop) != "desktop" || string(in.Copyright) != "copyright" {
		t.Error("payload files not read into Inputs")
	}

	// A missing required flag is one error for every tool (the pkgdir
	// default covers -pkgdir).
	for name, args := range map[string][]string{
		"no version": {"-arch", "amd64", "-bin", bin, "-out", out},
		"no arch":    {"-version", "v0.3.0", "-bin", bin, "-out", out},
		"no bin":     {"-version", "v0.3.0", "-arch", "amd64", "-out", out},
		"no out":     {"-version", "v0.3.0", "-arch", "amd64", "-bin", bin},
	} {
		if _, err := ParseInputs("arch help", "out help", args...); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}

	// An unreadable payload file names the piece that failed.
	if _, err := ParseInputs("arch help", "out help",
		"-version", "v0.3.0", "-arch", "amd64", "-bin", bin,
		"-pkgdir", filepath.Join(dir, "missing"), "-out", out); err == nil {
		t.Error("missing pkgdir accepted")
	} else if !strings.Contains(err.Error(), "man page") {
		t.Errorf("error does not name the missing piece: %v", err)
	}
}
