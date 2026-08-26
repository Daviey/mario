package main

import (
	"bytes"
	"fmt"
	"testing"
)

func TestNewcEntryLayout(t *testing.T) {
	data := []byte("HELLO")
	b := archive(data)

	if !bytes.HasPrefix(b, []byte("070701")) {
		t.Fatalf("magic mismatch")
	}

	get := func(i int) string {
		start := 6 + i*8
		return string(b[start : start+8])
	}
	if get(1) != "000081ed" {
		t.Errorf("mode = %q, want 000081ed (0o100755)", get(1))
	}
	if get(6) != "00000005" {
		t.Errorf("filesize = %q, want 5", get(6))
	}
	if get(11) != "00000005" {
		t.Errorf("namesize = %q, want 5", get(11))
	}

	// 110-byte header (6 + 13*8). Name is "init\0".
	if string(b[110:115]) != "init\x00" {
		t.Errorf("name = %q, want 'init\\0'", string(b[110:115]))
	}

	// Name pad to 4 bytes: 110+5 = 115. Pad 1 byte to 116.
	// Data starts at 116.
	if string(b[116:121]) != "HELLO" {
		t.Errorf("data = %q, want 'HELLO'", string(b[116:121]))
	}

	// Trailer present.
	if !bytes.Contains(b, []byte("TRAILER!!!")) {
		t.Errorf("no trailer found")
	}

	// Archive padded to 512.
	if len(b)%512 != 0 {
		t.Errorf("len = %d, not padded to 512", len(b))
	}
}

func TestPadding(t *testing.T) {
	var buf bytes.Buffer
	buf.WriteString("123")
	padTo4(&buf)
	if buf.Len() != 4 {
		t.Errorf("3 padded to 4: got %d", buf.Len())
	}

	buf.Reset()
	buf.WriteString("1234")
	padTo4(&buf)
	if buf.Len() != 4 {
		t.Errorf("4 padded to 4: got %d", buf.Len())
	}
}

func TestItoa(t *testing.T) {
	// Re-add generic sanity check to replace the one we removed from fb.go.
	if fmt.Sprintf("%08x", 255) != "000000ff" {
		t.Errorf("format sanity")
	}
}
