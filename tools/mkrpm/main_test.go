package main

import (
	"bytes"
	"compress/gzip"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Daviey/mario/internal/art"
	"github.com/Daviey/mario/tools/internal/pack"
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

func buildRpm(t *testing.T, version, arch string) []byte {
	t.Helper()
	bin, pkg := fixture(t)
	out := filepath.Join(t.TempDir(), "mario.rpm")
	if err := run(version, arch, bin, pkg, out); err != nil {
		t.Fatalf("run: %v", err)
	}
	rpm, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return rpm
}

// --- parse-back machinery (a miniature rpm reader) ---

type tagData struct {
	typ, cnt uint32
	raw      []byte
}

// header is a parsed RPM header: the 16-byte intro, the index and the
// data segment, with every tag's data sliced out.
type header struct {
	tags map[uint32]tagData
	end  int // offset just past the data segment (padding excluded)
}

func (h header) tag(t uint32) tagData {
	return h.tags[t]
}

func (h header) str(t uint32) string {
	raw := h.tag(t).raw
	if i := bytes.IndexByte(raw, 0); i >= 0 {
		return string(raw[:i])
	}
	return string(raw)
}

// strs returns exactly cnt strings — string arrays legitimately contain
// empty entries ("" digests for directories).
func (h header) strs(t uint32) []string {
	td := h.tag(t)
	parts := bytes.SplitN(td.raw, []byte{0}, int(td.cnt)+1)
	out := make([]string, 0, td.cnt)
	for i := 0; i < int(td.cnt) && i < len(parts); i++ {
		out = append(out, string(parts[i]))
	}
	return out
}

func (h header) i32s(t uint32) []int32 {
	raw := h.tag(t).raw
	out := make([]int32, 0, len(raw)/4)
	for i := 0; i+4 <= len(raw); i += 4 {
		out = append(out, int32(binary.BigEndian.Uint32(raw[i:])))
	}
	return out
}

func (h header) i16s(t uint32) []int16 {
	raw := h.tag(t).raw
	out := make([]int16, 0, len(raw)/2)
	for i := 0; i+2 <= len(raw); i += 2 {
		out = append(out, int16(binary.BigEndian.Uint16(raw[i:])))
	}
	return out
}

func (h header) i8s(t uint32) []int8 {
	raw := h.tag(t).raw
	out := make([]int8, 0, len(raw))
	for _, b := range raw {
		out = append(out, int8(b))
	}
	return out
}

// rpm is a whole parsed package.
type rpm struct {
	raw      []byte
	lead     []byte
	sig      header
	main     header
	payload  []byte // gzipped cpio
	mainFrom int    // main header start in raw
}

// parsePackage splits lead + signature header (8-padded) + main header
// + payload and parses both headers.
func parsePackage(t *testing.T, b []byte) rpm {
	t.Helper()
	var p rpm
	p.raw = b
	p.lead = b[:96]
	p.sig = parseHeader(t, b, 96, "signature")
	off := p.sig.end
	if r := off % 8; r != 0 {
		off += 8 - r
	}
	p.mainFrom = off
	p.main = parseHeader(t, b, off, "main")
	p.payload = b[p.main.end:]
	if len(p.payload) == 0 {
		t.Fatal("empty payload")
	}
	return p
}

func parseHeader(t *testing.T, b []byte, off int, what string) header {
	t.Helper()
	if !bytes.Equal(b[off:off+8], headerMagic) {
		t.Fatalf("%s header: bad magic % x", what, b[off:off+8])
	}
	il := int(binary.BigEndian.Uint32(b[off+8:]))
	dl := int(binary.BigEndian.Uint32(b[off+12:]))
	h := header{tags: map[uint32]tagData{}, end: off + 16 + 16*il + dl}
	if h.end > len(b) {
		t.Fatalf("%s header: %d bytes overruns package (%d)", what, h.end-off, len(b))
	}
	dataStart := off + 16 + 16*il
	for i := 0; i < il; i++ {
		e := off + 16 + 16*i
		tag := binary.BigEndian.Uint32(b[e:])
		typ := binary.BigEndian.Uint32(b[e+4:])
		foff := int(binary.BigEndian.Uint32(b[e+8:]))
		cnt := int(binary.BigEndian.Uint32(b[e+12:]))
		start := dataStart + foff
		var end int
		switch typ {
		case typeString: // one NUL-terminated string
			nul := bytes.IndexByte(b[start:], 0)
			if nul < 0 {
				t.Fatalf("%s header: tag %d not NUL-terminated", what, tag)
			}
			end = start + nul + 1
		case typeStringArray: // cnt NUL-terminated strings
			end = start
			for j := 0; j < cnt; j++ {
				nul := bytes.IndexByte(b[end:], 0)
				if nul < 0 {
					t.Fatalf("%s header: tag %d string %d not NUL-terminated", what, tag, j)
				}
				end += nul + 1
			}
		case typeChar, typeBin, typeInt8:
			end = start + cnt
		case typeInt16:
			end = start + 2*cnt
		case typeInt32:
			end = start + 4*cnt
		default:
			t.Fatalf("%s header: tag %d has unknown type %d", what, tag, typ)
		}
		if end > dataStart+dl {
			t.Fatalf("%s header: tag %d data overruns segment", what, tag)
		}
		h.tags[tag] = tagData{typ: typ, cnt: uint32(cnt), raw: b[start:end]}
	}
	return h
}

// cpioEntry is one parsed newc member.
type cpioEntry struct {
	name                string
	ino, mode, uid, gid int
	nlink, mtime, size  int
	data                []byte
}

// parseCpio walks a newc archive (what rpm2cpio emits).
func parseCpio(t *testing.T, b []byte) []cpioEntry {
	t.Helper()
	var out []cpioEntry
	for off := 0; off+110 <= len(b); {
		if string(b[off:off+6]) != "070701" {
			t.Fatalf("cpio: bad magic at %d: %q", off, b[off:off+6])
		}
		var f [13]int
		for i := range f {
			var v int
			if _, err := fmt.Sscanf(string(b[off+6+8*i:off+14+8*i]), "%x", &v); err != nil {
				t.Fatalf("cpio: field %d: %v", i, err)
			}
			f[i] = v
		}
		nameLen := f[11]
		name := string(b[off+110 : off+110+nameLen-1])
		dataStart := (off + 110 + nameLen + 3) &^ 3
		size := f[6]
		out = append(out, cpioEntry{
			name:  name,
			ino:   f[0],
			mode:  f[1],
			uid:   f[2],
			gid:   f[3],
			nlink: f[4],
			mtime: f[5],
			size:  size,
			data:  b[dataStart : dataStart+size],
		})
		if name == "TRAILER!!!" {
			return out
		}
		off = (dataStart + size + 3) &^ 3
	}
	t.Fatal("cpio: TRAILER!!! not found")
	return nil
}

func gunzip(t *testing.T, b []byte) []byte {
	t.Helper()
	r, err := gzip.NewReader(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	return out
}

// wantManifest is the expected install layout, sorted — including the
// real icon bytes, so digest round-trips check actual content.
func wantManifest() []payloadEntry {
	return manifest([]file{
		{"usr/bin/mario", 0o755, false, []byte("FAKE-ELF-BODY")},
		{"usr/share/man/man6/mario.6.gz", 0o644, true, pack.Gzip([]byte(".TH MARIO 6 fixture\n"), gzip.DefaultCompression)},
		{"usr/share/doc/mario/copyright", 0o644, true, []byte("Copyright: 2026 fixture\n")},
		{"usr/share/applications/mario.desktop", 0o644, false, []byte("[Desktop Entry]\nType=Application\nTerminal=true\n")},
		{"usr/share/icons/hicolor/48x48/apps/mario.png", 0o644, false, art.IconPNG(48, 8)},
	})
}

// mainBytes returns the main header's exact on-disk bytes.
func (p rpm) mainBytes() []byte {
	return p.raw[p.mainFrom:p.main.end]
}

func TestLead(t *testing.T) {
	p := parsePackage(t, buildRpm(t, "v1.2.3", "amd64"))
	l := p.lead
	if !bytes.Equal(l[0:4], []byte{0xed, 0xab, 0xee, 0xdb}) {
		t.Errorf("lead magic = % x", l[0:4])
	}
	if l[4] != 3 || l[5] != 0 {
		t.Errorf("lead version = %d.%d, want 3.0", l[4], l[5])
	}
	if got := binary.BigEndian.Uint16(l[6:]); got != 0 {
		t.Errorf("lead type = %d, want 0 (binary)", got)
	}
	if got := binary.BigEndian.Uint16(l[8:]); got != 1 {
		t.Errorf("lead archnum = %d, want 1", got)
	}
	if name := string(bytes.TrimRight(l[10:76], "\x00")); name != "mario-1.2.3-1" {
		t.Errorf("lead name = %q, want mario-1.2.3-1", name)
	}
	if got := binary.BigEndian.Uint16(l[76:]); got != 1 {
		t.Errorf("lead osnum = %d, want 1 (linux)", got)
	}
	if got := binary.BigEndian.Uint16(l[78:]); got != 5 {
		t.Errorf("lead sigtype = %d, want 5 (header-style)", got)
	}
	if reserved := l[80:96]; !bytes.Equal(reserved, make([]byte, 16)) {
		t.Errorf("lead reserved = % x, want zeros", reserved)
	}
}

func TestSignatureDigests(t *testing.T) {
	p := parsePackage(t, buildRpm(t, "v1.2.3", "amd64"))
	main := p.mainBytes()

	// The signature tags cover exactly the main header's on-disk bytes
	// (SHA1/SHA256) and those bytes plus the compressed payload (MD5).
	sha1sum := sha1.Sum(main)
	if got := p.sig.str(sigSHA1); got != hex.EncodeToString(sha1sum[:]) {
		t.Errorf("sig SHA1 = %s, want %s", got, hex.EncodeToString(sha1sum[:]))
	}
	sha256sum := sha256.Sum256(main)
	if got := p.sig.str(sigSHA256); got != hex.EncodeToString(sha256sum[:]) {
		t.Errorf("sig SHA256 = %s, want %s", got, hex.EncodeToString(sha256sum[:]))
	}

	h := md5.New()
	_, _ = h.Write(main)
	_, _ = h.Write(p.payload)
	if got, want := p.sig.tag(sigMD5).raw, h.Sum(nil); !bytes.Equal(got, want) {
		t.Errorf("sig MD5 = %x, want %x", got, want)
	}

	cpio := gunzip(t, p.payload)
	if got := p.sig.i32s(sigSize); len(got) != 1 || got[0] != int32(len(main)+len(p.payload)) {
		t.Errorf("sig SIZE = %v, want %d", got, len(main)+len(p.payload))
	}
	if got := p.sig.i32s(sigPayloadSize); len(got) != 1 || got[0] != int32(len(cpio)) {
		t.Errorf("sig PAYLOADSIZE = %v, want %d (uncompressed cpio)", got, len(cpio))
	}
}

func TestHeaderMetadata(t *testing.T) {
	p := parsePackage(t, buildRpm(t, "v1.2.3", "amd64"))
	h := p.main

	for tag, want := range map[uint32]string{
		tagName:              "mario",
		tagVersion:           "1.2.3",
		tagRelease:           "1",
		tagSummary:           summary,
		tagLicense:           "MIT",
		tagURL:               homepage,
		tagOS:                "linux",
		tagArch:              "x86_64",
		tagSourceRPM:         "mario-1.2.3-1.src.rpm",
		tagPayloadFormat:     "cpio",
		tagPayloadCompressor: "gzip",
		tagPayloadFlags:      "9",
	} {
		if got := h.str(tag); got != want {
			t.Errorf("tag %d = %q, want %q", tag, got, want)
		}
	}
	if got := h.str(tagDescription); !strings.Contains(got, "platformer") || !strings.Contains(got, "high-score board") {
		t.Errorf("description = %q", got)
	}
	if got := h.strs(tagHeaderI18NTable); len(got) != 1 || got[0] != "C" {
		t.Errorf("i18n table = %v", got)
	}

	// self-provide mario = 1.2.3-1
	if got := h.strs(tagProvideName); len(got) != 1 || got[0] != "mario" {
		t.Errorf("provides = %v", got)
	}
	if got := h.strs(tagProvideVersion); len(got) != 1 || got[0] != "1.2.3-1" {
		t.Errorf("provide versions = %v", got)
	}
	if got := h.i32s(tagProvideFlags); len(got) != 1 || got[0] != rpmSenseEqual {
		t.Errorf("provide flags = %v, want EQUAL (%d)", got, rpmSenseEqual)
	}

	// payload digest over the compressed payload as stored
	sum := sha256.Sum256(p.payload)
	if got := h.strs(tagPayloadDigest); len(got) != 1 || got[0] != hex.EncodeToString(sum[:]) {
		t.Errorf("payload digest = %v, want %s", got, hex.EncodeToString(sum[:]))
	}
	if got := h.i32s(tagPayloadDigestAlgo); len(got) != 1 || got[0] != pgpHashSHA256 {
		t.Errorf("payload digest algo = %v, want %d", got, pgpHashSHA256)
	}
}

func TestArchMapping(t *testing.T) {
	for goarch, rpmarch := range arches {
		p := parsePackage(t, buildRpm(t, "1.0.0", goarch))
		if got := p.main.str(tagArch); got != rpmarch {
			t.Errorf("arch %s: header ARCH = %q, want %q", goarch, got, rpmarch)
		}
	}
}

func TestFileEntries(t *testing.T) {
	p := parsePackage(t, buildRpm(t, "v1.2.3", "amd64"))
	h := p.main
	want := wantManifest()

	bases := h.strs(tagBaseNames)
	dirs := h.strs(tagDirNames)
	idxs := h.i32s(tagDirIndexes)
	if len(bases) != len(want) {
		t.Fatalf("basename count = %d, want %d", len(bases), len(want))
	}
	for i, b := range bases {
		if int(idxs[i]) >= len(dirs) {
			t.Fatalf("dirindex %d out of range (%d dirs)", idxs[i], len(dirs))
		}
		// DIRNAMES carry the leading slash (rpmbuild convention)
		if got, want := dirs[idxs[i]]+b, "/"+want[i].path; got != want {
			t.Errorf("file[%d] = %q, want %q", i, got, want)
		}
	}
	for _, d := range dirs {
		if !strings.HasSuffix(d, "/") {
			t.Errorf("dirname %q lacks trailing slash", d)
		}
	}

	sizes := h.i32s(tagFileSizes)
	modes := h.i16s(tagFileModes)
	mtimes := h.i32s(tagFileMTimes)
	digests := h.strs(tagFileDigests)
	states := h.i8s(tagFileStates)
	users := h.strs(tagFileUserName)
	inodes := h.i32s(tagFileINodes)
	seen := map[int32]bool{}
	for i, e := range want {
		wantMode, wantSize, wantDigest := e.mode, int32(len(e.data)), ""
		if e.dir {
			wantMode |= 0o040000
			wantSize = 4096
		} else {
			wantMode |= 0o100000
			sum := md5.Sum(e.data)
			wantDigest = hex.EncodeToString(sum[:])
		}
		if modes[i] != int16(wantMode) {
			t.Errorf("%s: mode = %o, want %o", e.path, modes[i], wantMode)
		}
		if sizes[i] != wantSize {
			t.Errorf("%s: size = %d, want %d", e.path, sizes[i], wantSize)
		}
		if digests[i] != wantDigest {
			t.Errorf("%s: digest = %q, want %q", e.path, digests[i], wantDigest)
		}
		if mtimes[i] != 0 {
			t.Errorf("%s: mtime = %d, want 0", e.path, mtimes[i])
		}
		if states[i] != 0 {
			t.Errorf("%s: state = %d, want 0 (normal)", e.path, states[i])
		}
		if users[i] != "root" {
			t.Errorf("%s: owner = %q, want root", e.path, users[i])
		}
		if seen[inodes[i]] {
			t.Errorf("%s: duplicate inode %d (hardlink risk)", e.path, inodes[i])
		}
		seen[inodes[i]] = true
	}

	// man page + copyright are %doc, nothing else is
	flags := h.i32s(tagFileFlags)
	for i, e := range want {
		if docFlag := flags[i] == 2; docFlag != e.doc {
			t.Errorf("%s: doc flag = %d, want %v", e.path, flags[i], e.doc)
		}
	}
}

func TestPayloadListing(t *testing.T) {
	p := parsePackage(t, buildRpm(t, "v1.2.3", "amd64"))
	cpio := gunzip(t, p.payload)
	entries := parseCpio(t, cpio)
	want := wantManifest()

	if r := len(cpio) % 512; r != 0 {
		t.Errorf("cpio length %d not padded to 512", len(cpio))
	}
	if len(entries) != len(want)+1 {
		t.Fatalf("cpio entries = %d, want %d (+trailer)", len(entries), len(want)+1)
	}
	if entries[len(entries)-1].name != "TRAILER!!!" {
		t.Errorf("last cpio entry = %q, want TRAILER!!!", entries[len(entries)-1].name)
	}

	digests := p.main.strs(tagFileDigests)
	for i, e := range want {
		c := entries[i]
		if c.name != "./"+e.path {
			t.Errorf("cpio[%d] = %q, want %q", i, c.name, "./"+e.path)
		}
		wantMode := e.mode
		if e.dir {
			wantMode |= 0o040000
		} else {
			wantMode |= 0o100000
		}
		if c.mode != int(wantMode) {
			t.Errorf("%s: cpio mode = %o, want %o", c.name, c.mode, wantMode)
		}
		if c.size != len(e.data) {
			t.Errorf("%s: cpio size = %d, want %d", c.name, c.size, len(e.data))
		}
		if c.uid != 0 || c.gid != 0 || c.mtime != 0 {
			t.Errorf("%s: cpio uid/gid/mtime = %d/%d/%d, want 0/0/0", c.name, c.uid, c.gid, c.mtime)
		}
		if !e.dir {
			sum := md5.Sum(c.data)
			if hex.EncodeToString(sum[:]) != digests[i] {
				t.Errorf("%s: cpio data digest does not match header FILEDIGESTS", c.name)
			}
			if !bytes.Equal(c.data, e.data) {
				t.Errorf("%s: cpio data does not match manifest", c.name)
			}
		}
	}

	// inodes unique across the archive
	seen := map[int]bool{}
	for _, c := range entries {
		if c.name != "TRAILER!!!" && seen[c.ino] {
			t.Errorf("cpio: duplicate inode %d", c.ino)
		}
		seen[c.ino] = true
	}
}

func TestDeterministic(t *testing.T) {
	a := buildRpm(t, "v1.2.3", "amd64")
	b := buildRpm(t, "1.2.3", "amd64") // leading v must not change bytes
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
		got, err := pack.SanitizeVersion(in, "RPM")
		if err != nil || got != want {
			t.Errorf("sanitizeVersion(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"", "abc", "v", "1 2", "1:2.0", "1.2-3"} {
		if _, err := pack.SanitizeVersion(bad, "RPM"); err == nil {
			t.Errorf("sanitizeVersion(%q) accepted", bad)
		}
	}
}

func TestArchRejected(t *testing.T) {
	bin, pkg := fixture(t)
	out := filepath.Join(t.TempDir(), "x.rpm")
	if err := run("1.0.0", "mips", bin, pkg, out); err == nil {
		t.Error("unsupported arch accepted")
	}
	if err := run("1.0.0", "amd64", bin, pkg, ""); err == nil {
		t.Error("empty -out accepted")
	}
}
