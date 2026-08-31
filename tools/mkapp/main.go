// mkapp builds the macOS .app bundle on plain Linux — pure stdlib, no
// Xcode, no codesign. It stitches the two darwin cross-builds Go already
// produces (make darwin/amd64 + darwin/arm64) into one universal (fat)
// Mach-O, renders the icns icon set from internal/art, writes the
// Info.plist, and packs the bundle into a deterministic zip — same
// contract as tools/mkdeb / tools/mkipa:
//
//	CGO_ENABLED=0 go run ./tools/mkapp -version v0.3.3 \
//	  -amd64 dist/mario-darwin-amd64 -arm64 dist/mario-darwin-arm64 \
//	  -universal dist/mario-darwin-universal \
//	  -out dist/mario_0.3.3_macos.app.zip
//
// The fat container is assembled by hand per <mach-o/fat.h>: a
// big-endian fat_header (magic 0xcafebabe, nfat_arch) followed by
// big-endian fat_arch records (cputype, cpusubtype, offset, size, align
// = log2 of the slice alignment), x86_64 slice first, each slice padded
// to its 2^align boundary (4 KiB / 16 KiB — dyld maps slices at
// page-aligned file offsets). The bundle is unsigned (no Developer ID):
// Gatekeeper shows the right-click → Open dialog on first launch; the
// arm64 slice is already ad-hoc signed by the Go linker.
package main

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/Daviey/mario/internal/art"
	"github.com/Daviey/mario/tools/internal/pack"
)

// One fat-binary slice. Constants from <mach/machine.h>: CPU_ARCH_ABI64
// (0x01000000) | CPU_TYPE_X86 (7) / CPU_TYPE_ARM (12); subtypes
// CPU_SUBTYPE_X86_64_ALL / CPU_SUBTYPE_ARM64_ALL. Align is log2 of the
// page size dyld maps the slice at: 4 KiB on x86_64, 16 KiB on arm64.
type arch struct {
	name       string
	cputype    uint32
	cpusubtype uint32
	align      uint32
}

var (
	archAMD64 = arch{name: "amd64", cputype: 0x01000007, cpusubtype: 3, align: 12}
	archARM64 = arch{name: "arm64", cputype: 0x0100000C, cpusubtype: 0, align: 14}
)

// machoMagic64 is MH_MAGIC_64 (0xfeedfacf) as the little-endian bytes a
// 64-bit Mach-O starts with.
var machoMagic64 = [4]byte{0xcf, 0xfa, 0xed, 0xfe}

// checkArch rejects a slice whose Mach-O header does not match the fat
// record it is about to be filed under — a swapped -amd64/-arm64 pair
// must fail loudly, not produce a fat file macOS refuses to load.
func checkArch(path string, b []byte, a arch) error {
	if len(b) < 8 || !bytes.Equal(b[:4], machoMagic64[:]) {
		return fmt.Errorf("%s: not a little-endian 64-bit Mach-O", path)
	}
	if ct := binary.LittleEndian.Uint32(b[4:8]); ct != a.cputype {
		return fmt.Errorf("%s: Mach-O cputype %#x, want %#x (%s) — swapped -amd64/-arm64?", path, ct, a.cputype, a.name)
	}
	return nil
}

// writeFat glues the two thin Mach-Os into a universal binary. Layout:
// 8-byte fat_header + 2×20-byte big-endian fat_arch records, then the
// slices in record order, zero-padded so each starts at a multiple of
// its 2^align.
func writeFat(w io.Writer, amd64, arm64 []byte) error {
	slices := []struct {
		a arch
		b []byte
	}{{archAMD64, amd64}, {archARM64, arm64}}

	placed := make([]uint32, len(slices)) // file offset of each slice
	off := 8 + 20*len(slices)             // header + fat_arch records
	for i, s := range slices {
		off = alignUp(off, 1<<s.a.align)
		placed[i] = uint32(off)
		off += len(s.b)
	}

	var buf bytes.Buffer
	var be [4]byte
	binary.BigEndian.PutUint32(be[:], 0xcafebabe)
	buf.Write(be[:])
	binary.BigEndian.PutUint32(be[:], uint32(len(slices)))
	buf.Write(be[:])
	for i, s := range slices {
		var rec [20]byte
		binary.BigEndian.PutUint32(rec[0:4], s.a.cputype)
		binary.BigEndian.PutUint32(rec[4:8], s.a.cpusubtype)
		binary.BigEndian.PutUint32(rec[8:12], placed[i])
		binary.BigEndian.PutUint32(rec[12:16], uint32(len(s.b)))
		binary.BigEndian.PutUint32(rec[16:20], s.a.align)
		buf.Write(rec[:])
	}
	end := 8 + 20*len(slices)
	for i, s := range slices {
		buf.Write(make([]byte, int(placed[i])-end)) // alignment padding
		buf.Write(s.b)
		end = int(placed[i]) + len(s.b)
	}
	_, err := w.Write(buf.Bytes())
	return err
}

// icnsTypes are the PNG-encoded OSTypes a macOS app icon wants. The
// @2x types carry the doubled pixel size (ic10 = 512@2x, ic11 = 16@2x,
// ic12 = 32@2x); art covers ~83% of the canvas (cell = size/6), the
// same framing tools/genicon renders the web icons with.
var icnsTypes = []struct {
	typ string
	px  int
}{
	{"ic07", 128},
	{"ic08", 256},
	{"ic09", 512},
	{"ic10", 1024},
	{"ic11", 32},
	{"ic12", 64},
}

// buildIcns renders the icon set from internal/art and packs it as an
// icns container: 'icns' + big-endian total size, then per icon the
// 4-char OSType + big-endian length (chunk header included) + raw PNG.
func buildIcns() []byte {
	var body bytes.Buffer
	for _, ic := range icnsTypes {
		png := art.IconPNG(ic.px, ic.px/6)
		body.WriteString(ic.typ)
		var be [4]byte
		binary.BigEndian.PutUint32(be[:], uint32(len(png)+8))
		body.Write(be[:])
		body.Write(png)
	}
	out := bytes.NewBufferString("icns")
	var be [4]byte
	binary.BigEndian.PutUint32(be[:], uint32(body.Len()+8))
	out.Write(be[:])
	out.Write(body.Bytes())
	return out.Bytes()
}

// infoPlist renders Contents/Info.plist. Keys follow Apple's bundle
// style (see packaging/ios/Info.plist); the category is the App Store
// "Action Games" UTI — the macOS analog of mario.desktop's
// Categories=Game;ActionGame.
func infoPlist(version string) []byte {
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleExecutable</key>
	<string>mario</string>
	<key>CFBundleIconFile</key>
	<string>Mario</string>
	<key>CFBundleIdentifier</key>
	<string>com.daviey.mario</string>
	<key>CFBundleInfoDictionaryVersion</key>
	<string>6.0</string>
	<key>CFBundleName</key>
	<string>Mario</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>CFBundleShortVersionString</key>
	<string>` + pack.ShortVersion(version) + `</string>
	<key>CFBundleVersion</key>
	<string>` + strings.TrimPrefix(version, "v") + `</string>
	<key>LSApplicationCategoryType</key>
	<string>public.app-category.action-games</string>
	<key>LSMinimumSystemVersion</key>
	<string>10.13</string>
</dict>
</plist>
`)
}

// assemble fills the .app bundle: the universal binary as
// Contents/MacOS/mario (0755 — the exec bit must survive the unzip),
// the Info.plist with the version injected, PkgInfo, and
// Contents/Resources/Mario.icns.
func assemble(appDir string, fat []byte, version string) error {
	for _, d := range []string{
		filepath.Join(appDir, "Contents", "MacOS"),
		filepath.Join(appDir, "Contents", "Resources"),
	} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	files := []struct {
		path string
		data []byte
		mode fs.FileMode
	}{
		{filepath.Join(appDir, "Contents", "MacOS", "mario"), fat, 0o755},
		{filepath.Join(appDir, "Contents", "Info.plist"), infoPlist(version), 0o644},
		{filepath.Join(appDir, "Contents", "PkgInfo"), []byte("APPL????"), 0o644},
		{filepath.Join(appDir, "Contents", "Resources", "Mario.icns"), buildIcns(), 0o644},
	}
	for _, f := range files {
		if err := os.WriteFile(f.path, f.data, f.mode); err != nil {
			return err
		}
		// Chmod explicitly: WriteFile's mode argument is masked by
		// the process umask (077 on the CI runner), which would strip
		// the exec bit from Contents/MacOS/mario and leak 0600 files
		// into the archive.
		if err := os.Chmod(f.path, f.mode); err != nil {
			return err
		}
	}
	return nil
}

// normalizeZipMode keeps the .app zip umask-independent: executables
// 0755, everything else 0644 — whatever the staged tree carries (the
// CI runner builds under umask 077).
func normalizeZipMode(perms fs.FileMode) fs.FileMode {
	if perms&0o111 != 0 {
		return 0o755
	}
	return 0o644
}

// alignUp rounds n up to the next multiple of a, which must be a
// power of two (the fat-binary slice alignments always are).
func alignUp(n, a int) int {
	return (n + a - 1) &^ (a - 1)
}

func main() {
	version := flag.String("version", "", "package version (git describe), e.g. v0.3.3")
	amd64Path := flag.String("amd64", "", "darwin/amd64 binary (make darwin/amd64)")
	arm64Path := flag.String("arm64", "", "darwin/arm64 binary (make darwin/arm64)")
	universal := flag.String("universal", "", "also write the bare universal binary here")
	out := flag.String("out", "", "output .app.zip path")
	flag.Parse()

	if *version == "" || *amd64Path == "" || *arm64Path == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "mkapp: -version, -amd64, -arm64 and -out are required")
		os.Exit(2)
	}

	amd64Bin, err := os.ReadFile(*amd64Path)
	if err != nil {
		pack.Fatal("mkapp", err)
	}
	arm64Bin, err := os.ReadFile(*arm64Path)
	if err != nil {
		pack.Fatal("mkapp", err)
	}
	if err := checkArch(*amd64Path, amd64Bin, archAMD64); err != nil {
		pack.Fatal("mkapp", err)
	}
	if err := checkArch(*arm64Path, arm64Bin, archARM64); err != nil {
		pack.Fatal("mkapp", err)
	}

	var fat bytes.Buffer
	if err := writeFat(&fat, amd64Bin, arm64Bin); err != nil {
		pack.Fatal("mkapp", err)
	}
	if *universal != "" {
		if err := os.WriteFile(*universal, fat.Bytes(), 0o755); err != nil {
			pack.Fatal("mkapp", err)
		}
		// Same umask trap as assemble(): WriteFile's mode is masked
		// (077 on the CI runner) and would strip the exec bit.
		if err := os.Chmod(*universal, 0o755); err != nil {
			pack.Fatal("mkapp", err)
		}
		fmt.Println("wrote", *universal)
	}

	staging, err := os.MkdirTemp("", "mkapp-")
	if err != nil {
		pack.Fatal("mkapp", err)
	}
	defer os.RemoveAll(staging)
	appDir := filepath.Join(staging, "Mario.app")
	if err := assemble(appDir, fat.Bytes(), *version); err != nil {
		pack.Fatal("mkapp", err)
	}

	f, err := os.Create(*out)
	if err != nil {
		pack.Fatal("mkapp", err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	if err := pack.ZipDir(staging, zw, normalizeZipMode); err != nil {
		pack.Fatal("mkapp", err)
	}
	if err := zw.Close(); err != nil {
		pack.Fatal("mkapp", err)
	}
	fmt.Println("wrote", *out)
}
