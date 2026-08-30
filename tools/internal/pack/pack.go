// Package pack holds the packaging helpers shared by the tools/mk*
// commands (mkdeb, mkrpm, mkapp, mkipa): git-describe version mapping
// and the deterministic gzip/zip writers. The package formats are
// byte-deterministic by contract, so these must exist as exactly one
// definition — a drift between tools would ship formats that quietly
// stop matching each other. It lives under tools/internal so only the
// packaging tools can import it.
package pack

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// ZipEpoch is the fixed zip timestamp (1980 = the zip epoch) so
// identical inputs give byte-identical archives in every tool.
var ZipEpoch = time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC)

var semverRe = regexp.MustCompile(`^v?(\d+\.\d+\.\d+)`)

// ShortVersion maps git describe output (v0.3.3-6-g84d833b-dirty) to the
// X.Y.Z CFBundleShortVersionString wants; anything unparsable is 0.0.0.
func ShortVersion(v string) string {
	if m := semverRe.FindStringSubmatch(strings.TrimSpace(v)); m != nil {
		return m[1]
	}
	return "0.0.0"
}

var (
	verRe  = regexp.MustCompile(`^[0-9][0-9A-Za-z.+~_]*$`)
	offTag = regexp.MustCompile(`-(\d+)-g([0-9A-Fa-f]+)`)
)

// SanitizeVersion maps a git-describe version onto a valid Debian or RPM
// native version — kind names the format in the error message ("Debian",
// "RPM"): strips the leading "v", rewrites the off-tag suffix
// ("-14-g1a2b3c" → "+14.g1a2b3c", "-dirty" → "+dirty" — a native
// version must not contain '-', which dpkg/rpm would read as an
// upstream/revision split), then rejects anything still outside the
// charset (must start with a digit; no ':' epoch, no spaces, no leftover
// hyphens).
func SanitizeVersion(v, kind string) (string, error) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	v = offTag.ReplaceAllString(v, "+$1.g$2")
	v = strings.ReplaceAll(v, "-dirty", "+dirty")
	if !verRe.MatchString(v) {
		return "", fmt.Errorf("invalid %s version %q (must start with a digit; no '-' after describe mapping)", kind, v)
	}
	return v, nil
}

// Gzip deterministically compresses b at the given level, without name
// or mtime — Go's flate output is deterministic for a fixed level, so
// package rebuilds stay byte-identical. The man pages use
// DefaultCompression (so .deb and .rpm carry identical bytes), the RPM
// payload BestCompression, the level PAYLOADFLAGS advertises.
func Gzip(b []byte, level int) []byte {
	var buf bytes.Buffer
	w, _ := gzip.NewWriterLevel(&buf, level)
	_, _ = w.Write(b)
	_ = w.Close()
	return buf.Bytes()
}

// ZipDir walks dir deterministically (sorted, filepath separators → /)
// and writes every regular file with the fixed ZipEpoch timestamp and
// the mode returned by modeFor — normalizing there keeps the archive
// from mirroring the build host's umask (the CI runner builds with
// umask 077).
func ZipDir(dir string, zw *zip.Writer, modeFor func(perms fs.FileMode) fs.FileMode) error {
	var files []struct {
		path string
		mode fs.FileMode
	}
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		files = append(files, struct {
			path string
			mode fs.FileMode
		}{path, info.Mode().Perm()})
		return nil
	})
	if err != nil {
		return err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].path < files[j].path })
	for _, f := range files {
		rel, err := filepath.Rel(dir, f.path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(f.path)
		if err != nil {
			return err
		}
		hdr := &zip.FileHeader{Name: filepath.ToSlash(rel), Modified: ZipEpoch}
		hdr.SetMode(modeFor(f.mode))
		w, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
	}
	return nil
}
