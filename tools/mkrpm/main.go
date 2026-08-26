// Command mkrpm builds an RPM v3 package (.rpm) for mario from an
// already-built static binary, using only the Go standard library —
// the CI runner (NixOS) has no rpm toolchain. From the repo root:
//
//	CGO_ENABLED=0 go run ./tools/mkrpm \
//		-version v0.3.0 -arch amd64 \
//		-bin dist/mario-linux-amd64 \
//		-out dist/mario_0.3.0_x86_64.rpm
//
// An RPM v3 package is four sections: a 96-byte lead, a signature
// header, the main header, then a cpio(newc)+gzip payload. It is
// unsigned (no RSA/DSA signature tags — dnf/yum install unsigned
// packages fine) but carries every digest tag `rpm -K` validates:
// SHA1 and SHA256 of the header, MD5 of header+payload, and a SHA256
// payload digest, so checksig reports "digests OK".
//
// Layout mirrors tools/mkdeb minus Debian's /usr/games convention
// (Fedora has none): /usr/bin/mario, man6 page gzipped, desktop entry,
// hicolor icon, copyright as %doc.
//
// Builds are deterministic: fixed epoch mtimes, sorted entries, root/root
// ownership, gzip without name/mtime — identical inputs give identical
// bytes, so release reruns re-upload byte-identical assets.
package main

import (
	"bytes"
	"compress/gzip"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/Daviey/mario/internal/art"
)

const (
	homepage   = "https://daviey.github.io/mario/"
	pkgName    = "mario"
	pkgRelease = "1"

	summary     = "terminal Mario-style platformer"
	description = "A Super Mario Bros.-style platformer rendered as square half-block\n" +
		"terminal pixels, fully deterministic, with a replay-verified online\n" +
		"high-score board. Seven built-in levels, a daily challenge, fire\n" +
		"flower, star power and a stomp combo ladder.\n"
)

// arches maps the Go arch names we ship to the RPM architecture written
// into the header (and the filename — hence the underscore form, which
// the release globs keep apart from dist/mario-* binaries). Anything
// else is a caller mistake, caught early rather than by rpm at install
// time.
var arches = map[string]string{
	"amd64":   "x86_64",
	"arm64":   "aarch64",
	"riscv64": "riscv64",
	"arm":     "armhfp",
	"386":     "i386",
}

// RPM header tag numbers (lib/rpmtag.h). Signature tags live in their
// own namespace inside the signature header — 1000 there is SIZE, not
// NAME — which is why both sets of constants coexist below.
const (
	// data types
	typeChar        = 1 // rpm's tag table registers FILESTATES as char
	typeInt8        = 2
	typeInt16       = 3
	typeInt32       = 4
	typeString      = 6
	typeBin         = 7
	typeStringArray = 8

	// region markers
	tagHeaderSignature = 62
	tagHeaderImmutable = 63

	// main-header namespace
	tagHeaderI18NTable   = 100
	tagName              = 1000
	tagVersion           = 1001
	tagRelease           = 1002
	tagSummary           = 1004
	tagDescription       = 1005
	tagSize              = 1009
	tagLicense           = 1014
	tagURL               = 1020
	tagOS                = 1021
	tagArch              = 1022
	tagFileSizes         = 1028
	tagFileStates        = 1029
	tagFileModes         = 1030
	tagFileRDevs         = 1033
	tagFileMTimes        = 1034
	tagFileDigests       = 1035
	tagFileLinkTos       = 1036
	tagFileFlags         = 1037
	tagFileUserName      = 1039
	tagFileGroupName     = 1040
	tagSourceRPM         = 1044
	tagFileVerifyFlags   = 1045
	tagProvideName       = 1047
	tagFileDevices       = 1095
	tagFileINodes        = 1096
	tagFileLangs         = 1097
	tagProvideFlags      = 1112
	tagProvideVersion    = 1113
	tagDirIndexes        = 1116
	tagBaseNames         = 1117
	tagDirNames          = 1118
	tagPayloadFormat     = 1124
	tagPayloadCompressor = 1125
	tagPayloadFlags      = 1126
	tagPayloadDigest     = 5092 // RPMTAG_PAYLOADSHA256 in modern rpm
	tagPayloadDigestAlgo = 5093

	// signature-header namespace
	sigSHA1        = 269
	sigSHA256      = 273
	sigSize        = 1000 // header+payload bytes on disk
	sigMD5         = 1004 // MD5 of header+payload bytes on disk
	sigPayloadSize = 1007 // uncompressed cpio length
)

// rpmSenseEqual is RPMSENSE_EQUAL ((1 << 3) in rpmds.h): renders the
// self-provide as "mario = <version>-<release>".
const rpmSenseEqual = 8

// pgpHashSHA256 is the PGP hash algorithm id rpm uses in
// PAYLOADDIGESTALGO: 8 = SHA256.
const pgpHashSHA256 = 8

func main() {
	version := flag.String("version", "", "package version (leading 'v' stripped), e.g. v0.3.0")
	arch := flag.String("arch", "", "Go architecture: amd64, arm64, riscv64, arm or 386")
	bin := flag.String("bin", "", "path to the built static binary")
	pkgdir := flag.String("pkgdir", "packaging", "directory holding mario.6, mario.desktop, copyright")
	out := flag.String("out", "", "output .rpm path")
	flag.Parse()

	if err := run(*version, *arch, *bin, *pkgdir, *out); err != nil {
		fmt.Fprintln(os.Stderr, "mkrpm:", err)
		os.Exit(1)
	}
}

func run(version, arch, bin, pkgdir, out string) error {
	if version == "" || arch == "" || bin == "" || out == "" {
		return fmt.Errorf("-version, -arch, -bin and -out are all required")
	}
	rpmArch, ok := arches[arch]
	if !ok {
		return fmt.Errorf("unsupported architecture %q (want amd64, arm64, riscv64, arm or 386)", arch)
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

	// Fedora has no /usr/games convention: the binary goes in /usr/bin.
	// Everything else mirrors the .deb payload exactly.
	entries := manifest([]file{
		{"usr/bin/mario", 0o755, false, game},
		{"usr/share/man/man6/mario.6.gz", 0o644, true, gz(man)},
		{"usr/share/doc/mario/copyright", 0o644, true, copyright},
		{"usr/share/applications/mario.desktop", 0o644, false, desktop},
		{"usr/share/icons/hicolor/48x48/apps/mario.png", 0o644, false, art.IconPNG(48, 8)},
	})

	cpio := cpioArchive(entries)
	payload := gzip9(cpio)
	digest := sha256.Sum256(payload)
	hdr := mainHeader(ver, rpmArch, entries, digest)
	sig := signatureHeader(hdr, payload, len(cpio))

	rpm := lead(pkgName, ver+"-"+pkgRelease)
	rpm = append(rpm, sig...)
	if r := len(rpm) % 8; r != 0 { // signature header pads to 8
		rpm = append(rpm, make([]byte, 8-r)...)
	}
	rpm = append(rpm, hdr...)
	rpm = append(rpm, payload...)
	if err := os.WriteFile(out, rpm, 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s (%d bytes, version %s, arch %s)\n", out, len(rpm), ver, rpmArch)
	return nil
}

type file struct {
	path string // install path, no leading slash
	mode int64
	doc  bool // %doc: man page + copyright
	data []byte
}

// payloadEntry is one install-path entry (directory or regular file) as
// it appears in both the cpio archive and the header file lists, which
// must agree path-for-path.
type payloadEntry struct {
	path string
	dir  bool
	mode int64 // permission bits only; S_IFMT bits added per context
	doc  bool
	data []byte
}

// manifest expands files with their parent directories (deduped) and
// sorts everything lexicographically — a dir path is always a strict
// prefix of its children, so parents precede children and the order is
// fixed, for determinism and for cpio extraction.
func manifest(files []file) []payloadEntry {
	seen := map[string]bool{}
	var all []payloadEntry
	addDir := func(p string) {
		if !seen[p] {
			seen[p] = true
			all = append(all, payloadEntry{path: p, dir: true, mode: 0o755})
		}
	}
	for _, f := range files {
		for d := path.Dir(f.path); d != "."; d = path.Dir(d) {
			addDir(d)
		}
	}
	for _, f := range files {
		all = append(all, payloadEntry{path: f.path, mode: f.mode, doc: f.doc, data: f.data})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].path < all[j].path })
	return all
}

// sanitizeVersion maps a git-describe version onto a valid RPM version:
// strips the leading "v", rewrites the off-tag suffix ("-14-g1a2b3c" →
// "+14.g1a2b3c", "-dirty" → "+dirty" — an RPM version must not contain
// '-', which would read as the version/release separator), then rejects
// anything still outside the charset (must start with a digit).
var (
	verRe  = regexp.MustCompile(`^[0-9][0-9A-Za-z.+~_]*$`)
	offTag = regexp.MustCompile(`-(\d+)-g([0-9A-Fa-f]+)`)
)

func sanitizeVersion(v string) (string, error) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	v = offTag.ReplaceAllString(v, "+$1.g$2")
	v = strings.ReplaceAll(v, "-dirty", "+dirty")
	if !verRe.MatchString(v) {
		return "", fmt.Errorf("invalid RPM version %q (must start with a digit; no '-' after describe mapping)", v)
	}
	return v, nil
}

// --- header (index + data store) ---

var headerMagic = []byte{0x8e, 0xad, 0xe8, 0x01, 0x00, 0x00, 0x00, 0x00}

type entry struct {
	tag, typ uint32
	cnt      int
	val      []byte
}

// store builds one RPM header: an index of tags over a packed data
// segment. bytes() lays the data out in tag order — rpm's verifier
// requires each tag's data to start where the previous (in index
// order) ended — with integers naturally aligned (misaligned ints are
// rejected) and strings/blobs packed unaligned (aligning those breaks
// other rpm versions). The region trailer is appended at the very end
// of the segment.
type store struct {
	entries []entry
}

func (s *store) add(tag, typ uint32, cnt int, val []byte) {
	s.entries = append(s.entries, entry{tag: tag, typ: typ, cnt: cnt, val: val})
}

func (s *store) addString(tag uint32, v string) {
	s.add(tag, typeString, 1, append([]byte(v), 0))
}

func (s *store) addStrings(tag uint32, vs []string) {
	var b []byte
	for _, v := range vs {
		b = append(b, v...)
		b = append(b, 0)
	}
	s.add(tag, typeStringArray, len(vs), b)
}

func (s *store) addChars(tag uint32, vs ...int8) {
	b := make([]byte, len(vs))
	for i, v := range vs {
		b[i] = byte(v)
	}
	s.add(tag, typeChar, len(vs), b)
}

func (s *store) addInt16s(tag uint32, vs ...int16) {
	b := make([]byte, 2*len(vs))
	for i, v := range vs {
		binary.BigEndian.PutUint16(b[2*i:], uint16(v))
	}
	s.add(tag, typeInt16, len(vs), b)
}

func (s *store) addInt32s(tag uint32, vs ...int32) {
	b := make([]byte, 4*len(vs))
	for i, v := range vs {
		binary.BigEndian.PutUint32(b[4*i:], uint32(v))
	}
	s.add(tag, typeInt32, len(vs), b)
}

func (s *store) addBin(tag uint32, v []byte) {
	s.add(tag, typeBin, len(v), v)
}

// typeAlign mirrors rpm's alignment table: only integer types align.
func typeAlign(typ uint32) int {
	switch typ {
	case typeInt16:
		return 2
	case typeInt32:
		return 4
	default:
		return 1
	}
}

// bytes renders the header. regionTag is 62 (HEADERSIGNATURES) for the
// signature header, 63 (HEADERIMMUTABLE) for the main one. The region
// is tracked by an index entry — first in the index — pointing at a
// 16-byte trailer at the very end of the data segment; the trailer
// repeats the entry with a negative offset spanning the whole index.
// Odd, but rpm's digest ranges are defined over exactly this shape
// (hdrblobDigestUpdate hashes magic + lengths + region + region data).
func (s *store) bytes(regionTag uint32) []byte {
	sort.Slice(s.entries, func(i, j int) bool { return s.entries[i].tag < s.entries[j].tag })

	var data []byte
	offs := make([]uint32, len(s.entries))
	for i, e := range s.entries {
		if a := typeAlign(e.typ); a > 1 {
			if r := len(data) % a; r != 0 {
				data = append(data, make([]byte, a-r)...)
			}
		}
		offs[i] = uint32(len(data))
		data = append(data, e.val...)
	}

	il := len(s.entries) + 1
	trailer := make([]byte, 16)
	binary.BigEndian.PutUint32(trailer[0:], regionTag)
	binary.BigEndian.PutUint32(trailer[4:], typeBin)
	binary.BigEndian.PutUint32(trailer[8:], ^uint32(16*il-1)) // -(16 * il)
	binary.BigEndian.PutUint32(trailer[12:], 16)
	data = append(data, trailer...)

	var b bytes.Buffer
	b.Write(headerMagic)
	w32(&b, uint32(il))
	w32(&b, uint32(len(data)))
	w32(&b, regionTag)
	w32(&b, typeBin)
	w32(&b, uint32(len(data))-16) // trailer offset
	w32(&b, 16)
	for i, e := range s.entries {
		w32(&b, e.tag)
		w32(&b, e.typ)
		w32(&b, offs[i])
		w32(&b, uint32(e.cnt))
	}
	b.Write(data)
	return b.Bytes()
}

func w32(b *bytes.Buffer, v uint32) {
	var buf [4]byte
	binary.BigEndian.PutUint32(buf[:], v)
	b.Write(buf[:])
}

// mainHeader builds the package metadata header: identity, payload
// description, the provides entry and the file lists.
func mainHeader(ver, rpmArch string, entries []payloadEntry, payloadDigest [sha256.Size]byte) []byte {
	s := &store{}
	s.addStrings(tagHeaderI18NTable, []string{"C"})
	s.addString(tagName, pkgName)
	s.addString(tagVersion, ver)
	s.addString(tagRelease, pkgRelease)
	s.addString(tagSummary, summary)
	s.addString(tagDescription, description)

	var installed int64
	for _, e := range entries {
		if !e.dir {
			installed += int64(len(e.data))
		}
	}
	s.addInt32s(tagSize, int32(installed))
	s.addString(tagLicense, "MIT")
	s.addString(tagURL, homepage)
	s.addString(tagOS, "linux")
	s.addString(tagArch, rpmArch)
	// rpm deduces "not a source rpm" from the presence of this tag.
	s.addString(tagSourceRPM, fmt.Sprintf("%s-%s-%s.src.rpm", pkgName, ver, pkgRelease))

	// self-provide: mario = <version>-<release>
	s.addStrings(tagProvideName, []string{pkgName})
	s.addInt32s(tagProvideFlags, rpmSenseEqual)
	s.addStrings(tagProvideVersion, []string{ver + "-" + pkgRelease})

	var (
		sizes, mtimes, flags, verify, devices, inodes []int32
		states                                        []int8
		modes, rdevs                                  []int16
		digests, linktos, langs, users, groups        []string
		dirindexes                                    []int32
		basenames                                     []string
	)
	dirnames := []string{}
	dirIdx := map[string]int32{}
	for i, e := range entries {
		// rpmbuild convention: DIRNAMES carry the leading slash
		// ("/usr/bin/") while the cpio payload names files
		// "./usr/bin/mario" — rpm's matcher strips both prefixes.
		dir, base := "/", e.path
		if j := strings.LastIndex(e.path, "/"); j >= 0 {
			dir, base = "/"+e.path[:j+1], e.path[j+1:]
		}
		idx, ok := dirIdx[dir]
		if !ok {
			idx = int32(len(dirnames))
			dirIdx[dir] = idx
			dirnames = append(dirnames, dir)
		}
		dirindexes = append(dirindexes, idx)
		basenames = append(basenames, base)

		mode, size, digest := e.mode, int32(0), ""
		if e.dir {
			mode |= 0o040000
			size = 4096
		} else {
			mode |= 0o100000
			size = int32(len(e.data))
			sum := md5.Sum(e.data)
			digest = hex.EncodeToString(sum[:])
		}
		modes = append(modes, int16(mode))
		sizes = append(sizes, size)
		digests = append(digests, digest)
		states = append(states, 0) // RPMFILE_STATE_NORMAL
		rdevs = append(rdevs, 0)
		mtimes = append(mtimes, 0)
		linktos = append(linktos, "")
		var fl int32
		if e.doc {
			fl = 2 // RPMFILE_DOC
		}
		flags = append(flags, fl)
		users = append(users, "root")
		groups = append(groups, "root")
		verify = append(verify, -1)
		devices = append(devices, 1)
		inodes = append(inodes, int32(i+1)) // unique: no accidental hardlink groups
		langs = append(langs, "")
	}
	s.addInt32s(tagFileSizes, sizes...)
	s.addChars(tagFileStates, states...)
	s.addInt16s(tagFileModes, modes...)
	s.addInt16s(tagFileRDevs, rdevs...)
	s.addInt32s(tagFileMTimes, mtimes...)
	s.addStrings(tagFileDigests, digests)
	s.addStrings(tagFileLinkTos, linktos)
	s.addInt32s(tagFileFlags, flags...)
	s.addStrings(tagFileUserName, users)
	s.addStrings(tagFileGroupName, groups)
	s.addInt32s(tagFileVerifyFlags, verify...)
	s.addInt32s(tagFileDevices, devices...)
	s.addInt32s(tagFileINodes, inodes...)
	s.addStrings(tagFileLangs, langs)
	s.addInt32s(tagDirIndexes, dirindexes...)
	s.addStrings(tagBaseNames, basenames)
	s.addStrings(tagDirNames, dirnames)

	s.addString(tagPayloadFormat, "cpio")
	s.addString(tagPayloadCompressor, "gzip")
	s.addString(tagPayloadFlags, "9") // matches gzip.BestCompression below
	s.addStrings(tagPayloadDigest, []string{hex.EncodeToString(payloadDigest[:])})
	s.addInt32s(tagPayloadDigestAlgo, pgpHashSHA256)
	return s.bytes(tagHeaderImmutable)
}

// signatureHeader builds the digest-only signature header. The SHA1 and
// SHA256 tags cover exactly the main header bytes as stored; MD5 covers
// main header + compressed payload; SIZE/PAYLOADSIZE are informational
// (progress bars) — on-disk header+payload and uncompressed cpio sizes.
func signatureHeader(hdr, payload []byte, cpioLen int) []byte {
	s := &store{}
	sha1sum := sha1.Sum(hdr)
	sha256sum := sha256.Sum256(hdr)
	s.addString(sigSHA1, hex.EncodeToString(sha1sum[:]))
	s.addString(sigSHA256, hex.EncodeToString(sha256sum[:]))
	s.addInt32s(sigSize, int32(len(hdr)+len(payload)))
	h := md5.New()
	_, _ = h.Write(hdr)
	_, _ = h.Write(payload)
	s.addBin(sigMD5, h.Sum(nil))
	s.addInt32s(sigPayloadSize, int32(cpioLen))
	return s.bytes(tagHeaderSignature)
}

// lead writes the 96-byte legacy lead: magic, format version 3.0,
// binary type, archnum 1, name-version-release, os 1 (linux),
// signature type 5 (header-style), 16 reserved bytes. Readers only
// check the magic and version, but rpm writes all of it.
func lead(name, fullVersion string) []byte {
	n := []byte(fmt.Sprintf("%s-%s", name, fullVersion))
	if len(n) > 65 {
		n = n[:65]
	}
	n = append(n, make([]byte, 66-len(n))...)
	b := []byte{0xed, 0xab, 0xee, 0xdb, 0x03, 0x00, 0x00, 0x00, 0x00, 0x01}
	b = append(b, n...)
	b = append(b, 0x00, 0x01, 0x00, 0x05)
	return append(b, make([]byte, 16)...)
}

// --- cpio(newc) payload ---

// cpioArchive packs the manifest as an SVR4 "newc" cpio archive, the
// way rpm itself does: every path "./"-prefixed (no absolute names),
// directories included, uid/gid/mtime zero, unique inodes, and the
// mandatory TRAILER!!! entry. The archive is padded to 512 bytes.
func cpioArchive(entries []payloadEntry) []byte {
	var buf bytes.Buffer
	for i, e := range entries {
		mode := uint64(e.mode)
		nlink := uint64(1)
		if e.dir {
			mode |= 0o040000
			nlink = 2
		} else {
			mode |= 0o100000
		}
		writeNewc(&buf, "./"+e.path, mode, uint64(len(e.data)), e.data, uint64(i+1), nlink)
	}
	writeNewc(&buf, "TRAILER!!!", 0, 0, nil, 0, 1)
	if r := buf.Len() % 512; r != 0 {
		buf.Write(make([]byte, 512-r))
	}
	return buf.Bytes()
}

// writeNewc appends one newc entry: "070701" magic + 13 hex fields,
// then NUL-terminated name and file data, each padded to 4 bytes.
func writeNewc(buf *bytes.Buffer, name string, mode, size uint64, data []byte, ino, nlink uint64) {
	fields := []uint64{
		ino,                   // c_ino
		mode,                  // c_mode (S_IFMT bits | permissions)
		0,                     // c_uid
		0,                     // c_gid
		nlink,                 // c_nlink
		0,                     // c_mtime (epoch: deterministic)
		size,                  // c_filesize
		0,                     // c_devmajor
		0,                     // c_devminor
		0,                     // c_rdevmajor
		0,                     // c_rdevminor
		uint64(len(name)) + 1, // c_namesize (NUL included)
		0,                     // c_check (always 0 for newc)
	}
	buf.WriteString("070701")
	for _, f := range fields {
		fmt.Fprintf(buf, "%08x", f)
	}
	buf.WriteString(name)
	buf.WriteByte(0)
	pad4(buf)
	buf.Write(data)
	pad4(buf)
}

func pad4(buf *bytes.Buffer) {
	if r := buf.Len() % 4; r != 0 {
		buf.Write(make([]byte, 4-r))
	}
}

// --- compression ---

// gzip9 compresses the payload at the level PAYLOADFLAGS ("9")
// advertises. Go's flate is deterministic for a fixed level.
func gzip9(b []byte) []byte {
	var buf bytes.Buffer
	w, _ := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	_, _ = w.Write(b)
	_ = w.Close()
	return buf.Bytes()
}

// gz mirrors tools/mkdeb's helper so the man page carries identical
// bytes in both package formats.
func gz(b []byte) []byte {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	_, _ = w.Write(b)
	_ = w.Close()
	return buf.Bytes()
}
