// Command mkcpio packs the EFI boot's initramfs: a "newc" cpio archive
// with the game's static init binary as /init plus the two device nodes
// PID 1 needs for stdio (/dev/console, /dev/null) — without them the
// kernel gives init closed fds and early userspace panics die invisibly.
// The kernel's EFI stub (embedded initramfs via CONFIG_INITRAMFS_SOURCE)
// and the QEMU dev loop (-initrd) both consume this format. Pure stdlib,
// like tools/mkdeb — no cpio(1) on the build host (the CI runner has none).
//
// Usage: mkcpio OUT.cpio INITBIN
package main

import (
	"bytes"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) != 3 {
		fmt.Fprintln(os.Stderr, "usage: mkcpio OUT.cpio INITBIN")
		os.Exit(2)
	}
	out, init := os.Args[1], os.Args[2]
	data, err := os.ReadFile(init)
	if err != nil {
		fatal(err)
	}
	if err := os.WriteFile(out, archive(data), 0o644); err != nil {
		fatal(err)
	}
}

// archive builds the complete initramfs for one init binary.
func archive(init []byte) []byte {
	var buf bytes.Buffer
	writeDir(&buf, "dev")
	writeCharDev(&buf, "dev/console", 0o0600, 5, 1)
	writeCharDev(&buf, "dev/null", 0o0666, 1, 3)
	writeFile(&buf, "init", 0o755, init)
	writeEntry(&buf, "TRAILER!!!", 0, 0, nil, 0, 0)
	// Pad the archive to a 512-byte boundary (cpio(5) convention; the
	// kernel tolerates any tail, external initrd consumers expect this).
	if r := buf.Len() % 512; r != 0 {
		buf.Write(make([]byte, 512-r))
	}
	return buf.Bytes()
}

func writeDir(buf *bytes.Buffer, name string) {
	writeEntry(buf, name, 0o040755, 0, nil, 0, 0)
}

func writeCharDev(buf *bytes.Buffer, name string, perm uint64, major, minor uint64) {
	writeEntry(buf, name, 0o020000|perm, 0, nil, major, minor)
}

func writeFile(buf *bytes.Buffer, name string, perm uint64, data []byte) {
	writeEntry(buf, name, 0o100000|perm, uint64(len(data)), data, 0, 0)
}

// writeEntry appends one newc-format entry. Header: "070701" + 13
// little-endian hex fields, then NUL-terminated name and file data, each
// padded to a 4-byte boundary.
func writeEntry(buf *bytes.Buffer, name string, mode, filesize uint64, data []byte, devMajor, devMinor uint64) {
	fields := []uint64{
		nextInode(), // c_ino
		mode,        // c_mode (S_IFREG | perms)
		0,           // c_uid
		0,           // c_gid
		1,           // c_nlink
		0,           // c_mtime
		filesize,
		0,                     // c_devmajor
		0,                     // c_devminor
		devMajor,              // c_rdevmajor (device nodes)
		devMinor,              // c_rdevminor
		uint64(len(name) + 1), // c_namesize (NUL included)
		0,                     // c_check (always 0 for newc)
	}
	buf.WriteString("070701")
	for _, f := range fields {
		fmt.Fprintf(buf, "%08x", f)
	}
	buf.WriteString(name)
	buf.WriteByte(0)
	padTo4(buf)
	buf.Write(data)
	padTo4(buf)
}

var ino uint64

func nextInode() uint64 {
	ino++
	return ino
}

func padTo4(buf *bytes.Buffer) {
	if r := buf.Len() % 4; r != 0 {
		buf.Write(make([]byte, 4-r))
	}
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "mkcpio:", err)
	os.Exit(1)
}
