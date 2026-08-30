//go:build linux

package main

import (
	"os"
	"testing"
	"unsafe"
)

// evEvent renders one EV_KEY input_event in the kernel's 64-bit layout,
// ready to feed a keyboard's drain through a pipe.
func evEvent(code uint16, value int32) []byte {
	e := inputEvent{Type: evKey, Code: code, Value: value}
	b := make([]byte, unsafe.Sizeof(e))
	*(*inputEvent)(unsafe.Pointer(&b[0])) = e
	return b
}

// A release must echo its press's codepoint even when shift was let go
// while the key stayed down: the mapper keys kitty holds by source, so
// releasing 'd' (100) after a 'D' press (68) would strand the hold —
// and the stale source would later swallow a sibling release.
func TestDrainReleaseEchoesPressTimeShift(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()

	var script []byte
	for _, e := range [][]byte{
		evEvent(evLeftShift, 1), // shift down
		evEvent(evD, 1),         // 'D' press (codepoint 68)
		evEvent(evLeftShift, 0), // shift up while 'd' still held
		evEvent(evD, 0),         // release must still say 68, not 100
	} {
		script = append(script, e...)
	}
	if _, err := w.Write(script); err != nil {
		t.Fatal(err)
	}

	var seqs []string
	k := &keyboard{fd: int(r.Fd())}
	k.drain(func(b []byte) { seqs = append(seqs, string(b)) })

	want := []string{"\x1b[68;1:1u", "\x1b[68;1:3u"} // 'D' press, 'D' release
	if len(seqs) != len(want) {
		t.Fatalf("drain emitted %d sequences (%q), want %d", len(seqs), seqs, len(want))
	}
	for i, s := range seqs {
		if s != want[i] {
			t.Errorf("sequence %d = %q, want %q", i, s, want[i])
		}
	}
}
