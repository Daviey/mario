// mkipa builds the unsigned iOS .ipa on plain Linux — no Mac, no Xcode.
//
// It compiles packaging/ios/main.m (a thin WKWebView shell) with clang
// -target arm64-apple-ios against an iPhoneOS SDK, links with lld (the
// SDK ships .tbd stubs, so no Apple binaries are needed), ad-hoc signs
// with ldid, and packs Payload/mario.app (binary, Info.plist, generated
// app icons, www/ = the WASM web build) into a deterministic zip.
//
// CGO_ENABLED=0 go run ./tools/mkipa -version v0.3.3 -out dist/mario_0.3.3_ios_unsigned.ipa
//
// The SDK is NOT in this repo (license: Apple allows Xcode SDK use on
// Apple hardware only) — point -sdk or $IOS_SDK at an extracted
// iPhoneOS.sdk (see packaging/ios/README.md). The resulting .ipa is
// unsigned; sideload it with Sideloadly/AltStore, which re-sign it with
// your own Apple ID. Tools needed on PATH: clang (multi-target), ld64.lld,
// ldid — on NixOS: nix develop .#ios.
package main

import (
	"archive/zip"
	"bytes"
	"flag"
	"fmt"
	"image"
	"image/png"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/Daviey/mario/internal/art"
	"github.com/Daviey/mario/tools/internal/pack"
)

// appIcons are the home-screen sizes iOS looks up via CFBundleIconFiles.
// Name → px; art pixels are ~83% of the canvas, matching tools/genicon.
var appIcons = map[string]int{
	"AppIcon20x20":        20,
	"AppIcon29x29":        29,
	"AppIcon40x40":        40,
	"AppIcon60x60":        60,
	"AppIcon76x76":        76,
	"AppIcon83.5x83.5@2x": 167,
}

type build struct {
	Version string // git describe (v0.3.3-6-g84d833b)
	SrcDir  string // packaging/ios
	WebDir  string // dist/web → bundled as www/
}

func main() {
	version := flag.String("version", "", "package version (git describe), e.g. v0.3.3")
	src := flag.String("src", "packaging/ios", "directory holding main.m + Info.plist")
	web := flag.String("web", "dist/web", "the WASM web build to bundle as www/")
	sdk := flag.String("sdk", os.Getenv("IOS_SDK"), "iPhoneOS SDK path (or $IOS_SDK)")
	clang := flag.String("clang", "clang", "clang binary (must be multi-target, unwrapped on NixOS)")
	ldid := flag.String("ldid", "ldid", "ldid binary for the ad-hoc signature")
	out := flag.String("out", "", "output .ipa path")
	flag.Parse()

	if *version == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "mkipa: -version and -out are required")
		os.Exit(2)
	}
	if *sdk == "" {
		fmt.Fprintln(os.Stderr, "mkipa: no iPhoneOS SDK — pass -sdk or set IOS_SDK (see packaging/ios/README.md)")
		os.Exit(2)
	}
	if _, err := os.Stat(filepath.Join(*sdk, "SDKSettings.plist")); err != nil {
		fmt.Fprintf(os.Stderr, "mkipa: %s does not look like an iPhoneOS SDK (no SDKSettings.plist): %v\n", *sdk, err)
		os.Exit(2)
	}

	b := build{Version: *version, SrcDir: *src, WebDir: *web}
	staging, err := os.MkdirTemp("", "mkipa-")
	if err != nil {
		fatal(err)
	}
	defer os.RemoveAll(staging)
	appDir := filepath.Join(staging, "Payload", "mario.app")
	bin := filepath.Join(appDir, "mario")
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		fatal(err)
	}
	args := []string{
		"-target", "arm64-apple-ios15.0",
		"-isysroot", *sdk,
		"-fobjc-arc", "-miphoneos-version-min=15.0",
		"-O2", "-Wall",
		filepath.Join(*src, "main.m"),
		"-framework", "Foundation", "-framework", "UIKit", "-framework", "WebKit",
		"-fuse-ld=lld",
		"-o", bin,
	}
	if out, err := runCmd(*clang, args...); err != nil {
		fmt.Fprintln(os.Stderr, out)
		fatal(fmt.Errorf("clang: %w", err))
	}
	if out, err := runCmd(*ldid, "-S", bin); err != nil {
		fmt.Fprintln(os.Stderr, out)
		fatal(fmt.Errorf("ldid: %w", err))
	}

	if err := assemble(appDir, b); err != nil {
		fatal(err)
	}

	f, err := os.Create(*out)
	if err != nil {
		fatal(err)
	}
	defer f.Close()
	zw := zip.NewWriter(f)
	if err := pack.ZipDir(staging, zw, uniformZipMode); err != nil {
		fatal(err)
	}
	if err := zw.Close(); err != nil {
		fatal(err)
	}
	fmt.Println("wrote", *out)
}

// assemble fills the .app bundle: Info.plist (version-substituted), the
// app icons rendered from internal/art, PkgInfo, and www/ = web build.
func assemble(appDir string, b build) error {
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		return err
	}
	plist, err := os.ReadFile(filepath.Join(b.SrcDir, "Info.plist"))
	if err != nil {
		return err
	}
	plist = bytes.ReplaceAll(plist, []byte("{{SHORT_VERSION}}"), []byte(pack.ShortVersion(b.Version)))
	plist = bytes.ReplaceAll(plist, []byte("{{VERSION}}"), []byte(b.Version))
	if err := os.WriteFile(filepath.Join(appDir, "Info.plist"), plist, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(appDir, "PkgInfo"), []byte("APPL????"), 0o644); err != nil {
		return err
	}
	for name, px := range appIcons {
		if err := writePNG(filepath.Join(appDir, name+".png"), art.Icon(px, px*5/6)); err != nil {
			return err
		}
	}
	return copyTree(filepath.Join(appDir, "www"), b.WebDir)
}

func writePNG(path string, img *image.RGBA) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

// runCmd runs an external command and returns its combined output.
// Named runCmd, not run: mkdeb/mkrpm reserve `run` for the whole tool
// pipeline returning error.
func runCmd(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	return buf.String(), err
}

// uniformZipMode gives every .ipa entry 0755: iOS ignores unix modes in
// the zip, so one uniform mode keeps the archive umask-independent.
func uniformZipMode(fs.FileMode) fs.FileMode {
	return 0o755
}

func copyTree(dst, src string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		// WriteFile's mode is umask-masked (077 on the CI runner);
		// harmless here — the modes are discarded when ZipDir
		// re-normalizes them via modeFor.
		return os.WriteFile(target, data, info.Mode().Perm())
	})
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "mkipa:", err)
	os.Exit(1)
}
