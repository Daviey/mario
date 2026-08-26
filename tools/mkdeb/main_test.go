package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixture builds a fake payload directory + binary and returns their paths.
func fixture(t *testing.T) (binPath, pkgDir string) {
	t.Helper()
	dir := t.TempDir()
	pkg := filepath.Join(dir, "pkg")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"mario.6":       ".TH MARIO 6 fixture\n",
		"mario.desktop": "[Desktop Entry]\nType=Application\nTerminal=true\n",
		"copyright":     "Copyright: 2026 fixture\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(pkg, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(dir, "mario-bin")
	if err := os.WriteFile(bin, []byte("FAKE-ELF-BODY"), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, pkg
}

func buildDeb(t *testing.T, version, arch string) []byte {
	t.Helper()
	bin, pkg := fixture(t)
	out := filepath.Join(t.TempDir(), "mario.deb")
	if err := run(version, arch, bin, pkg, out); err != nil {
		t.Fatalf("run: %v", err)
	}
	deb, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return deb
}

type arMember struct {
	name string
	data []byte
}

// parseAr reads a classic ar archive back.
func parseAr(t *testing.T, data []byte) []arMember {
	t.Helper()
	if !bytes.HasPrefix(data, []byte("!<arch>\n")) {
		t.Fatal("missing ar magic")
	}
	var out []arMember
	for pos := 8; pos < len(data); {
		if pos+60 > len(data) {
			t.Fatalf("truncated ar header at %d", pos)
		}
		hdr := string(data[pos : pos+60])
		if hdr[58] != '`' || hdr[59] != '\n' {
			t.Fatalf("bad ar header terminator %q", hdr[58:60])
		}
		name := strings.TrimSpace(hdr[0:16])
		var size int
		if _, err := fmt.Sscanf(strings.TrimSpace(hdr[48:58]), "%d", &size); err != nil {
			t.Fatalf("bad size in ar header: %v", err)
		}
		pos += 60
		if pos+size > len(data) {
			t.Fatalf("member %s overruns archive", name)
		}
		out = append(out, arMember{name, data[pos : pos+size]})
		pos += size
		if size%2 == 1 {
			pos++ // `\n` padding
		}
	}
	return out
}

// readTarGz unpacks a gzipped tar into name → (header, content).
func readTarGz(t *testing.T, data []byte) map[string]*tarEntry {
	t.Helper()
	zr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	defer zr.Close()
	tr := tar.NewReader(zr)
	out := map[string]*tarEntry{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar: %v", err)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("tar read %s: %v", hdr.Name, err)
		}
		out[hdr.Name] = &tarEntry{hdr, body}
	}
	return out
}

type tarEntry struct {
	hdr  *tar.Header
	body []byte
}

func TestArStructure(t *testing.T) {
	deb := buildDeb(t, "v1.2.3", "amd64")
	ms := parseAr(t, deb)
	if len(ms) != 3 {
		t.Fatalf("got %d members, want 3", len(ms))
	}
	for i, want := range []string{"debian-binary", "control.tar.gz", "data.tar.gz"} {
		if ms[i].name != want {
			t.Errorf("member %d = %q, want %q", i, ms[i].name, want)
		}
	}
	if string(ms[0].data) != "2.0\n" {
		t.Errorf("debian-binary = %q, want \"2.0\\n\"", ms[0].data)
	}
}

func TestControlFile(t *testing.T) {
	ms := parseAr(t, buildDeb(t, "v1.2.3", "arm64"))
	ctrl := readTarGz(t, ms[1].data)
	c, ok := ctrl["control"]
	if !ok {
		t.Fatal("no control entry in control.tar.gz")
	}
	for _, want := range []string{
		"Package: mario\n",
		"Version: 1.2.3\n",
		"Architecture: arm64\n",
		"Maintainer: Dave Walker <dave@daviey.com>\n",
		"Section: games\n",
		"Priority: optional\n",
		"Homepage: https://daviey.github.io/mario/\n",
		"Description: terminal Mario-style platformer\n",
	} {
		if !bytes.Contains(c.body, []byte(want)) {
			t.Errorf("control missing %q", want)
		}
	}
	if !bytes.Contains(c.body, []byte("Installed-Size: ")) {
		t.Error("control missing Installed-Size")
	}
}

func TestMd5sumsMatchData(t *testing.T) {
	ms := parseAr(t, buildDeb(t, "1.0.0", "amd64"))
	ctrl := readTarGz(t, ms[1].data)
	data := readTarGz(t, ms[2].data)

	md5s, ok := ctrl["md5sums"]
	if !ok {
		t.Fatal("no md5sums entry")
	}
	for _, line := range strings.Split(strings.TrimSpace(string(md5s.body)), "\n") {
		parts := strings.Split(line, "  ")
		if len(parts) != 2 {
			t.Fatalf("md5sums line %q malformed", line)
		}
		e, ok := data[parts[1]]
		if !ok {
			t.Fatalf("md5sums references missing data file %q", parts[1])
		}
		sum := md5.Sum(e.body)
		if got := hex.EncodeToString(sum[:]); got != parts[0] {
			t.Errorf("%s: md5 %s, recorded %s", parts[1], got, parts[0])
		}
	}
}

func TestDataLayout(t *testing.T) {
	ms := parseAr(t, buildDeb(t, "v1.2.3", "amd64"))
	data := readTarGz(t, ms[2].data)

	// Directories present with 0755 and trailing slash.
	for _, d := range []string{"usr/", "usr/games/", "usr/share/man/man6/", "usr/share/doc/mario/", "usr/share/icons/hicolor/48x48/apps/"} {
		e, ok := data[d]
		if !ok {
			t.Errorf("missing dir entry %s", d)
			continue
		}
		if e.hdr.Typeflag != tar.TypeDir {
			t.Errorf("%s not a dir entry", d)
		}
		if e.hdr.Mode != 0o755 {
			t.Errorf("%s mode %o, want 755", d, e.hdr.Mode)
		}
	}

	// Game binary: the actual input bytes, executable.
	g, ok := data["usr/games/mario"]
	if !ok {
		t.Fatal("missing usr/games/mario")
	}
	if string(g.body) != "FAKE-ELF-BODY" {
		t.Error("game binary content is not the -bin input verbatim")
	}
	if g.hdr.Mode != 0o755 {
		t.Errorf("game mode %o, want 755", g.hdr.Mode)
	}

	// Manpage gzips back to the payload verbatim.
	m, ok := data["usr/share/man/man6/mario.6.gz"]
	if !ok {
		t.Fatal("missing usr/share/man/man6/mario.6.gz")
	}
	zr, err := gzip.NewReader(bytes.NewReader(m.body))
	if err != nil {
		t.Fatalf("manpage not gzip: %v", err)
	}
	body, _ := io.ReadAll(zr)
	zr.Close()
	if string(body) != ".TH MARIO 6 fixture\n" {
		t.Errorf("manpage content %q", body)
	}

	// Desktop entry keeps Terminal=true — a desktop file without it
	// launches a terminal game in a GUI void.
	d, ok := data["usr/share/applications/mario.desktop"]
	if !ok {
		t.Fatal("missing usr/share/applications/mario.desktop")
	}
	if !bytes.Contains(d.body, []byte("Terminal=true")) {
		t.Error("desktop entry lost Terminal=true")
	}

	// Icon is a real 48×48 PNG.
	ic, ok := data["usr/share/icons/hicolor/48x48/apps/mario.png"]
	if !ok {
		t.Fatal("missing hicolor icon")
	}
	img, err := png.Decode(bytes.NewReader(ic.body))
	if err != nil {
		t.Fatalf("icon not a PNG: %v", err)
	}
	if img.Bounds().Dx() != 48 || img.Bounds().Dy() != 48 {
		t.Errorf("icon bounds %v, want 48×48", img.Bounds())
	}

	// Ownership: everything root/root at epoch.
	for name, e := range data {
		if e.hdr.Uname != "root" || e.hdr.Gname != "root" {
			t.Errorf("%s owned by %s/%s", name, e.hdr.Uname, e.hdr.Gname)
		}
		if !e.hdr.ModTime.IsZero() && e.hdr.ModTime.Unix() != 0 {
			t.Errorf("%s mtime %v, want epoch", name, e.hdr.ModTime)
		}
	}
}

func TestDeterministic(t *testing.T) {
	a := buildDeb(t, "v1.2.3", "amd64")
	b := buildDeb(t, "1.2.3", "amd64") // leading v must not change bytes
	if !bytes.Equal(a, b) {
		t.Error("two builds of the same input differ (or 'v' prefix leaked)")
	}
}

func TestVersionSanitize(t *testing.T) {
	ok := map[string]string{
		"v0.3.0":                  "0.3.0",
		"0.3.0":                   "0.3.0",
		"0.3.0-dirty":             "0.3.0+dirty",
		"0.3.0-14-g1a2b3c4":       "0.3.0+14.g1a2b3c4",
		"0.3.0-14-g1a2b3c4-dirty": "0.3.0+14.g1a2b3c4+dirty",
	}
	for in, want := range ok {
		got, err := sanitizeVersion(in)
		if err != nil || got != want {
			t.Errorf("sanitizeVersion(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "abc", "v", "1 2", "1:2.0", "1.2-3"} {
		if _, err := sanitizeVersion(bad); err == nil {
			t.Errorf("sanitizeVersion(%q) accepted", bad)
		}
	}
}

func TestArchRejected(t *testing.T) {
	bin, pkg := fixture(t)
	out := filepath.Join(t.TempDir(), "x.deb")
	if err := run("1.0.0", "386", bin, pkg, out); err == nil {
		t.Error("unsupported arch accepted")
	}
	if err := run("1.0.0", "amd64", bin, pkg, ""); err == nil {
		t.Error("empty -out accepted")
	}
}
