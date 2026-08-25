package input

import "testing"

func TestPlainDecoderDecodesKittyTextKeys(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"letter press", "\x1b[100;1:1u", "d"},
		{"letter repeat", "\x1b[100;1:2u", "d"},
		{"letter release dropped", "\x1b[100;1:3u", ""},
		{"shifted letter", "\x1b[68;2:1u", "D"},
		{"enter", "\x1b[13;1:1u", "\r"},
		{"backspace", "\x1b[127;1:1u", "\x7f"},
		{"escape key", "\x1b[27;1:1u", "\x1b"},
		{"arrow dropped", "\x1b[1;1:1C", ""},
		{"ss3 dropped", "\x1bOC", ""},
		{"plain passthrough", "dq", "dq"},
		{"alt letter", "\x1bd", "d"},
		{"no params", "\x1b[u", ""},
	}
	for _, c := range cases {
		d := NewPlainDecoder()
		if got := string(d.Feed([]byte(c.in))); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestPlainDecoderHoldsSplitSequences(t *testing.T) {
	d := NewPlainDecoder()
	seq := "\x1b[100;1:1u"
	if got := d.Feed([]byte(seq[:4])); got != nil {
		t.Fatalf("partial feed emitted %q", got)
	}
	if got := string(d.Feed([]byte(seq[4:]))); got != "d" {
		t.Fatalf("completion got %q, want d", got)
	}
}

func TestPlainDecoderNeverWedgesOnGarbage(t *testing.T) {
	d := NewPlainDecoder()
	if got := d.Feed([]byte("\x1b[01234567890123456789")); got != nil {
		t.Fatalf("garbage emitted %q", got)
	}
	// Past the guard bound the buffer is flushed and plain keys flow again.
	if got := string(d.Feed([]byte{'x'})); got != "x" {
		t.Fatalf("after flush got %q, want x", got)
	}
}

// The regression this package exists for: on a kitty terminal with flags
// 1|2|8 pushed, a held key must stay held through the OS repeat delay with
// NO repeat bytes at all — press events are sticky until the release.
func TestKittyHoldOutlivesRepeatDelayWithoutRepeats(t *testing.T) {
	m := NewMapper()
	m.Feed([]byte("\x1b[100;1:1u")) // press 'd'
	for i := range maxHoldWindow {  // longer than any OS repeat delay
		if in := m.Poll(); !in.Right {
			t.Fatalf("held key dropped at poll %d with no repeat bytes", i)
		}
	}
	m.Feed([]byte("\x1b[100;1:3u")) // release
	if in := m.Poll(); in.Right {
		t.Fatal("release did not clear the key")
	}
}
