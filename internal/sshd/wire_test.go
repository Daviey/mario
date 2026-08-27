package sshd

import "testing"

func TestBufReaderRoundtrip(t *testing.T) {
	w := &buf{}
	w.u8(7)
	w.boolean(true)
	w.u32(0xDEADBEEF)
	w.str([]byte{1, 2, 3})
	w.cstr("hello")
	w.mpint([]byte{0x00, 0x01, 0x02})

	r := &reader{b: w.b}
	if got := r.u8(); got != 7 {
		t.Fatalf("u8 = %d", got)
	}
	if !r.boolean() {
		t.Fatal("boolean = false")
	}
	if got := r.u32(); got != 0xDEADBEEF {
		t.Fatalf("u32 = %#x", got)
	}
	if got := r.str(); string(got) != "\x01\x02\x03" {
		t.Fatalf("str = %v", got)
	}
	if got := r.str(); string(got) != "hello" {
		t.Fatalf("cstr = %q", got)
	}
	if got := r.str(); len(got) != 2 || got[0] != 1 || got[1] != 2 {
		t.Fatalf("mpint = %v", got)
	}
	if !r.ok() {
		t.Fatal("reader failed on well-formed input")
	}
}

func TestReaderRejectsTruncated(t *testing.T) {
	w := &buf{}
	w.u32(100) // claims 100 bytes follow
	r := &reader{b: w.b}
	r.str()
	if r.ok() {
		t.Fatal("truncated string should fail the reader")
	}
}

func TestMpintEdges(t *testing.T) {
	enc := func(v []byte) []byte {
		w := &buf{}
		w.mpint(v)
		return w.b
	}
	if got := enc(nil); len(got) != 4 || got[3] != 0 {
		t.Fatalf("mpint(nil) = %x", got)
	}
	if got := enc(make([]byte, 32)); len(got) != 4 {
		t.Fatalf("mpint(zeros) = %x", got)
	}
	// High bit set: a leading zero byte keeps the value positive.
	if got := enc([]byte{0x80}); len(got) != 6 || got[4] != 0 || got[5] != 0x80 {
		t.Fatalf("mpint(0x80) = %x", got)
	}
	// 32-byte shared secrets (curve25519 output) must survive exactly.
	secret := make([]byte, 32)
	for i := range secret {
		secret[i] = byte(i + 1)
	}
	w := &buf{}
	w.mpint(secret)
	r := &reader{b: w.b}
	if got := r.str(); string(got) != string(secret) {
		t.Fatal("32-byte mpint roundtrip mismatch")
	}
}
