// Command mkcpio packs the EFI boot's initramfs: a "newc" cpio archive
// with the game's static init binary as /init plus the two device nodes
// PID 1 needs for stdio (/dev/console, /dev/null) — without them the
// kernel gives init closed fds and early userspace panics die invisibly.
// The kernel's EFI stub (embedded initramfs via CONFIG_INITRAMFS_SOURCE)
// and the QEMU dev loop (-initrd) both consume this format. Pure stdlib
// (the newc framing comes from tools/internal/pack, shared with mkrpm)
// — no cpio(1) on the build host (the CI runner has none).
//
// Usage: mkcpio OUT.cpio INITBIN
package main

import (
	"bytes"
	"fmt"
	"os"

	"github.com/Daviey/mario/tools/internal/pack"
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

// archive builds the complete initramfs for one init binary. Inodes are
// each entry's 1-based index — a property of the entry list itself, so
// repeated calls with the same init give identical bytes (the TRAILER!!!
// marker takes the last index, exactly where the old shared counter
// left it).
func archive(init []byte) []byte {
	entries := []pack.NewcEntry{
		{Name: "dev", Mode: 0o040755, Nlink: 1},
		{Name: "dev/console", Mode: 0o020000 | 0o0600, Nlink: 1, RdevMajor: 5, RdevMinor: 1},
		{Name: "dev/null", Mode: 0o020000 | 0o0666, Nlink: 1, RdevMajor: 1, RdevMinor: 3},
		{Name: "init", Mode: 0o100755, Nlink: 1, Size: uint64(len(init)), Data: init},
		{Name: "TRAILER!!!", Nlink: 1}, // mode 0: just the end marker
	}
	var buf bytes.Buffer
	for i := range entries {
		entries[i].Ino = uint64(i + 1)
		pack.WriteNewcEntry(&buf, entries[i])
	}
	// Pad the archive to a 512-byte boundary (cpio(5) convention; the
	// kernel tolerates any tail, external initrd consumers expect this).
	if r := buf.Len() % 512; r != 0 {
		buf.Write(make([]byte, 512-r))
	}
	return buf.Bytes()
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, "mkcpio:", err)
	os.Exit(1)
}
