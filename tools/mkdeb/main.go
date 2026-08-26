// Command mkdeb builds a Debian package (.deb) for mario from an
// already-built static binary, using only the Go standard library —
// the CI runner (NixOS) has no dpkg. From the repo root:
//
//	CGO_ENABLED=0 go run ./tools/mkdeb \
//		-version v0.3.0 -arch amd64 \
//		-bin dist/mario-linux-amd64 \
//		-out dist/mario_0.3.0_amd64.deb
//
// A .deb is an ar archive: debian-binary, control.tar.gz, data.tar.gz.
// Layout follows Debian game conventions: /usr/games/mario, man page in
// man6, hicolor icon, desktop entry with Terminal=true.
//
// Builds are deterministic: fixed epoch mtimes, sorted entries, root/root
// ownership, gzip without name/mtime — identical inputs give identical
// bytes, so release reruns re-upload byte-identical assets.
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/md5"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Daviey/mario/internal/art"
)

const (
	homepage   = "https://daviey.github.io/mario/"
	maintainer = "Dave Walker <dave@daviey.com>"
)

// debs is the set of Debian architectures we ship (amd64, arm64, riscv64
// map 1:1 from Go arch names; GOARCH=arm → armhf, GOARCH=386 → i386 — the
// Makefile deb/<goarch>-shaped targets pass the Debian name). Anything
// else is a caller mistake, caught early rather than by dpkg at install
// time.
var debs = map[string]bool{"amd64": true, "arm64": true, "riscv64": true, "armhf": true, "i386": true}

func main() {
	version := flag.String("version", "", "package version (leading 'v' stripped), e.g. v0.3.0")
	arch := flag.String("arch", "", "Debian architecture: amd64, arm64, riscv64, armhf or i386")
	bin := flag.String("bin", "", "path to the built static binary")
	pkgdir := flag.String("pkgdir", "packaging", "directory holding mario.6, mario.desktop, copyright")
	out := flag.String("out", "", "output .deb path")
	flag.Parse()

	if err := run(*version, *arch, *bin, *pkgdir, *out); err != nil {
		fmt.Fprintln(os.Stderr, "mkdeb:", err)
		os.Exit(1)
	}
}

func run(version, arch, bin, pkgdir, out string) error {
	if version == "" || arch == "" || bin == "" || out == "" {
		return fmt.Errorf("-version, -arch, -bin and -out are all required")
	}
	if !debs[arch] {
		return fmt.Errorf("unsupported Debian architecture %q (want amd64, arm64, riscv64, armhf or i386)", arch)
	}
	ver, err := sanitizeVersion(version)
	if err != nil {
		return err
	}

	game, err := os.ReadFile(bin)
	if err != nil {
		return err
	}
	man, err := os.ReadFile(filepath.Join(pkgdir, "mario.6"))
	if err != nil {
		return err
	}
	desktop, err := os.ReadFile(filepath.Join(pkgdir, "mario.desktop"))
	if err != nil {
		return err
	}
	copyright, err := os.ReadFile(filepath.Join(pkgdir, "copyright"))
	if err != nil {
		return err
	}

	files := []file{
		{"usr/games/mario", 0o755, game},
		{"usr/share/man/man6/mario.6.gz", 0o644, gz(man)},
		{"usr/share/doc/mario/copyright", 0o644, copyright},
		{"usr/share/applications/mario.desktop", 0o644, desktop},
		{"usr/share/icons/hicolor/48x48/apps/mario.png", 0o644, art.IconPNG(48, 8)},
	}
	data, err := tarball(files)
	if err != nil {
		return err
	}
	control, err := tarball([]file{
		{"control", 0o644, controlFile(ver, arch, len(data))},
		{"md5sums", 0o644, md5sums(files)},
	})
	if err != nil {
		return err
	}

	deb := ar([]member{
		{"debian-binary", []byte("2.0\n")},
		{"control.tar.gz", control},
		{"data.tar.gz", data},
	})
	if err := os.WriteFile(out, deb, 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s (%d bytes, version %s, arch %s)\n", out, len(deb), ver, arch)
	return nil
}

type file struct {
	path string // install path, no leading slash
	mode int64
	data []byte
}

type member struct {
	name string
	data []byte
}

// sanitizeVersion maps a git-describe version onto a valid Debian native
// version: strips the leading "v", rewrites the off-tag suffix
// ("-14-g1a2b3c" → "+14.g1a2b3c", "-dirty" → "+dirty" — a native
// version must not contain '-', which dpkg would read as an
// upstream/revision split), then rejects anything still outside the
// Debian charset (must start with a digit; no ':' epoch, no spaces, no
// leftover hyphens).
var (
	verRe  = regexp.MustCompile(`^[0-9][0-9A-Za-z.+~_]*$`)
	offTag = regexp.MustCompile(`-(\d+)-g([0-9A-Fa-f]+)`)
)

func sanitizeVersion(v string) (string, error) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	v = offTag.ReplaceAllString(v, "+$1.g$2")
	v = strings.ReplaceAll(v, "-dirty", "+dirty")
	if !verRe.MatchString(v) {
		return "", fmt.Errorf("invalid Debian version %q (must start with a digit; no '-' after describe mapping)", v)
	}
	return v, nil
}

func controlFile(ver, arch string, dataLen int) []byte {
	installedSize := (dataLen + 1023) / 1024 // KiB, rounded up
	var b strings.Builder
	fmt.Fprintf(&b, "Package: mario\n")
	fmt.Fprintf(&b, "Version: %s\n", ver)
	fmt.Fprintf(&b, "Architecture: %s\n", arch)
	fmt.Fprintf(&b, "Maintainer: %s\n", maintainer)
	fmt.Fprintf(&b, "Installed-Size: %d\n", installedSize)
	fmt.Fprintf(&b, "Section: games\n")
	fmt.Fprintf(&b, "Priority: optional\n")
	fmt.Fprintf(&b, "Homepage: %s\n", homepage)
	b.WriteString("Description: terminal Mario-style platformer\n")
	b.WriteString(" A Super Mario Bros.-style platformer rendered as square half-block\n")
	b.WriteString(" terminal pixels, fully deterministic, with a replay-verified online\n")
	b.WriteString(" high-score board. Seven built-in levels, a daily challenge, fire\n")
	b.WriteString(" flower, star power and a stomp combo ladder.\n")
	return []byte(b.String())
}

func md5sums(files []file) []byte {
	var b strings.Builder
	for _, f := range files {
		sum := md5.Sum(f.data)
		fmt.Fprintf(&b, "%s  %s\n", hex.EncodeToString(sum[:]), f.path)
	}
	return []byte(b.String())
}

// tarball packs files (plus their parent directories, deduped, dirs first
// by lexicographic order — a dir path is always a strict prefix of its
// children) into a deterministic gzipped tar.
func tarball(files []file) ([]byte, error) {
	seen := map[string]bool{}
	var all []file
	for _, f := range files {
		for d := path.Dir(f.path); d != "."; d = path.Dir(d) {
			if !seen[d] {
				seen[d] = true
				all = append(all, file{d + "/", 0o755, nil})
			}
		}
	}
	all = append(all, files...)
	sort.Slice(all, func(i, j int) bool { return all[i].path < all[j].path })

	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for _, f := range all {
		hdr := &tar.Header{
			Name:    f.path,
			Mode:    f.mode,
			Size:    int64(len(f.data)),
			ModTime: time.Unix(0, 0).UTC(),
			Format:  tar.FormatUSTAR,
			Uid:     0, Gid: 0,
			Uname: "root", Gname: "root",
		}
		if strings.HasSuffix(f.path, "/") {
			hdr.Typeflag = tar.TypeDir
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, err
		}
		if _, err := tw.Write(f.data); err != nil {
			return nil, err
		}
	}
	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func gz(b []byte) []byte {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	_, _ = w.Write(b)
	_ = w.Close()
	return buf.Bytes()
}

// ar writes a classic (GNU-style) ar archive: 8-byte magic, then 60-byte
// ASCII headers with `\n` padding to even offsets. dpkg requires exactly
// this shape for debian-binary / control.tar.* / data.tar.*.
func ar(members []member) []byte {
	var buf bytes.Buffer
	buf.WriteString("!<arch>\n")
	for _, m := range members {
		fmt.Fprintf(&buf, "%-16s%-12d%-6d%-6d%-8s%-10d`\n",
			m.name, 0, 0, 0, "100644", len(m.data))
		buf.Write(m.data)
		if len(m.data)%2 == 1 {
			buf.WriteByte('\n')
		}
	}
	return buf.Bytes()
}
