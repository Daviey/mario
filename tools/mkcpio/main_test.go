package main

import (
	"bytes"
	"testing"
)

func hexToUint(t *testing.T, s string) int {
	t.Helper()
	v := 0
	for i := range len(s) {
		c := s[i]
		var d int
		switch {
		case c >= '0' && c <= '9':
			d = int(c - '0')
		case c >= 'a' && c <= 'f':
			d = int(c-'a') + 10
		default:
			t.Fatalf("bad hex %q", s)
		}
		v = v*16 + d
	}
	return v
}

// entryAt finds the newc header whose name field is exactly name and
// returns the 13 hex fields with the header start offset.
func entryAt(t *testing.T, b []byte, name string) (off int, fields []string) {
	t.Helper()
	for off = 0; off+110 <= len(b); {
		if string(b[off:off+6]) != "070701" {
			t.Fatalf("entry at %d: bad magic", off)
		}
		f := make([]string, 13)
		for i := range f {
			f[i] = string(b[off+6+i*8 : off+6+(i+1)*8])
		}
		nameSize := hexToUint(t, f[11])
		n := string(b[off+110 : off+110+nameSize-1])
		if n == name {
			return off, f
		}
		size := hexToUint(t, f[6])
		_ = size
		dataStart := off + 110 + nameSize
		if p := (110 + nameSize) % 4; p != 0 {
			dataStart += 4 - p
		}
		dataEnd := dataStart + size
		if p := size % 4; p != 0 {
			dataEnd += 4 - p
		}
		if n == "TRAILER!!!" || dataEnd >= len(b) {
			break
		}
		off = dataEnd
	}
	t.Fatalf("entry %q not found", name)
	return 0, nil
}

func TestArchiveLayout(t *testing.T) {
	b := archive([]byte("HELLO"))
	if len(b)%512 != 0 {
		t.Errorf("len = %d, not padded to 512", len(b))
	}

	// All five entries present and walkable.
	for _, name := range []string{"dev", "dev/console", "dev/null", "init", "TRAILER!!!"} {
		entryAt(t, b, name)
	}

	// dev/console is a char device 5:1.
	_, f := entryAt(t, b, "dev/console")
	if f[9] != "00000005" || f[10] != "00000001" {
		t.Errorf("dev/console rdev = %s:%s, want 5:1", f[9], f[10])
	}
	if f[1] != "00002180" { // S_IFCHR|0600
		t.Errorf("dev/console mode = %s, want 0000a180", f[1])
	}

	// init is a regular executable carrying the payload.
	_, f = entryAt(t, b, "init")
	if f[1] != "0081ed" && f[1] != "000081ed" {
		t.Errorf("init mode = %s, want 000081ed (0o100755)", f[1])
	}
	if hexToUint(t, f[6]) != 5 {
		t.Errorf("init filesize = %s, want 5", f[6])
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
