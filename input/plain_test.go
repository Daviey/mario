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

// The regression: a truncated escape (rest lost on a quiet link) used to
// wedge the decoder — every later trigger byte was eaten as the missing
// CSI final until >16 bytes accumulated, so 'l'/'d'/'q'/Enter went dead.
// Per-tick FlushStale (the router hook) must free the buffer silently.
func TestPlainDecoderFlushStaleFreesKeys(t *testing.T) {
	for _, c := range []struct {
		name string
		key  string
	}{
		{"plain l", "l"},
		{"kitty-encoded l", "\x1b[108;1:1u"},
	} {
		d := NewPlainDecoder()
		if got := d.Feed([]byte("\x1b[")); got != nil {
			t.Fatalf("%s: truncated escape emitted %q", c.name, got)
		}
		for range staleSeqPolls {
			d.FlushStale()
		}
		if got := string(d.Feed([]byte(c.key))); got != "l" {
			t.Fatalf("%s: after stale flush got %q, want l", c.name, got)
		}
	}
}

// FlushStale must not eat a sequence that is merely split across reads:
// within the grace window the completing bytes still land.
func TestPlainDecoderFlushStaleKeepsSplitSequences(t *testing.T) {
	d := NewPlainDecoder()
	d.Feed([]byte("\x1b[10"))
	d.FlushStale()
	d.FlushStale() // staleSeqPolls-1 ticks: still waiting
	if got := string(d.Feed([]byte("0;1:1u"))); got != "d" {
		t.Fatalf("split sequence dropped by FlushStale: got %q, want d", got)
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
