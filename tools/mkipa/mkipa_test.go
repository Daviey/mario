package main

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Daviey/mario/tools/internal/pack"
)

// assemble is testable without clang: the binary is a placeholder file
// that main() would have compiled+signed beforehand.
func TestAssembleBundle(t *testing.T) {
	src := t.TempDir()
	web := t.TempDir()
	staging := t.TempDir()
	appDir := filepath.Join(staging, "Payload", "mario.app")

	plistTmpl := "<plist>{{SHORT_VERSION}}|{{VERSION}}</plist>\n"
	if err := os.WriteFile(filepath.Join(src, "Info.plist"), []byte(plistTmpl), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"index.html", "mario.wasm", "wasm_exec.js"} {
		if err := os.WriteFile(filepath.Join(web, f), []byte("data-"+f), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(web, "icons"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(web, "icons", "icon-192.png"), []byte("pngdata"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(appDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(appDir, "mario"), []byte("\xcf\xfa\xed\xfe fake-macho"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := assemble(appDir, build{Version: "v0.3.3-6-g84d833b", SrcDir: src, WebDir: web}); err != nil {
		t.Fatal(err)
	}

	plist, err := os.ReadFile(filepath.Join(appDir, "Info.plist"))
	if err != nil {
		t.Fatal(err)
	}
	if s := string(plist); s != "<plist>0.3.3|v0.3.3-6-g84d833b</plist>\n" {
		t.Errorf("Info.plist substitution wrong: %q", s)
	}
	if pk, err := os.ReadFile(filepath.Join(appDir, "PkgInfo")); err != nil || string(pk) != "APPL????" {
		t.Errorf("PkgInfo = %q, %v", pk, err)
	}
	for _, f := range []string{"index.html", "mario.wasm", "icons/icon-192.png"} {
		if _, err := os.Stat(filepath.Join(appDir, "www", f)); err != nil {
			t.Errorf("www/%s missing: %v", f, err)
		}
	}
	icon, err := os.ReadFile(filepath.Join(appDir, "AppIcon60x60.png"))
	if err != nil {
		t.Fatalf("app icon missing: %v", err)
	}
	if !bytes.HasPrefix(icon, []byte("\x89PNG\r\n\x1a\n")) {
		t.Error("AppIcon60x60.png is not a PNG")
	}
}

func TestZipDeterministicAndContained(t *testing.T) {
	staging := t.TempDir()
	appDir := filepath.Join(staging, "Payload", "mario.app")
	if err := os.MkdirAll(filepath.Join(appDir, "www"), 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"Payload/mario.app/mario":      "bin",
		"Payload/mario.app/Info.plist": "plist",
		"Payload/mario.app/www/z.html": "z",
		"Payload/mario.app/www/a.js":   "a",
		"Payload/mario.app/PkgInfo":    "APPL????",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(staging, filepath.FromSlash(name)), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	buildZip := func() []byte {
		var buf bytes.Buffer
		zw := zip.NewWriter(&buf)
		if err := pack.ZipDir(staging, zw, uniformZipMode); err != nil {
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
	if len(zr.File) != len(files) {
		t.Fatalf("zip has %d entries, want %d", len(zr.File), len(files))
	}
	want := make(map[string]bool, len(files))
	for name := range files {
		want[name] = true
	}
	for _, f := range zr.File {
		if !want[f.Name] {
			t.Errorf("unexpected zip entry %q", f.Name)
			continue
		}
		delete(want, f.Name)
		if !strings.HasPrefix(f.Name, "Payload/mario.app/") {
			t.Errorf("entry escapes the app bundle: %q", f.Name)
		}
		if strings.Contains(f.Name, "..") || filepath.IsAbs(f.Name) {
			t.Errorf("entry path is not zip-relative: %q", f.Name)
		}
		if !f.Modified.Equal(pack.ZipEpoch) {
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
		if string(content) != files[f.Name] {
			t.Errorf("entry %q content = %q", f.Name, content)
		}
	}
	for name := range want {
		t.Errorf("missing zip entry %q", name)
	}
}
