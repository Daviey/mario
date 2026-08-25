package input

import (
	"testing"

	"mario/engine"
)

func press(m *Mapper, b ...byte) engine.Input {
	m.Feed(b)
	return m.Poll()
}

func TestPlainKeyMapping(t *testing.T) {
	m := NewMapper()
	cases := []struct {
		b    byte
		want string
	}{
		{'a', "left"}, {'A', "left"},
		{'d', "right"}, {'D', "right"},
		{'w', "up"}, {'W', "up"}, {' ', "up"},
		{'s', "down"}, {'S', "down"},
		{'x', "run"}, {'X', "run"},
	}
	for _, c := range cases {
		in := press(m, c.b)
		var got string
		switch {
		case in.Left:
			got = "left"
		case in.Right:
			got = "right"
		case in.Up:
			got = "up"
		case in.Down:
			got = "down"
		case in.Run:
			got = "run"
		default:
			got = "none"
		}
		if got != c.want {
			t.Errorf("key %q -> %s, want %s", c.b, got, c.want)
		}
		// Release the key so cases stay independent.
		for range holdWindow + 2 {
			m.Poll()
		}
	}
}

func TestArrowEscapeSequences(t *testing.T) {
	m := NewMapper()
	if in := press(m, "\x1b[D"[0], "\x1b[D"[1], "\x1b[D"[2]); !in.Left {
		t.Errorf("CSI D (left arrow) -> %+v", in)
	}
	for range holdWindow + 2 {
		m.Poll()
	}
	if in := press(m, 'q'); !in.Quit { // buffer must be empty after sequence
		t.Error("stale buffer after arrow sequence")
	}
}

func TestArrowSequencesArriveInOneChunk(t *testing.T) {
	for _, seq := range []struct {
		bytes string
		field string
	}{
		{"\x1b[A", "up"}, {"\x1b[B", "down"}, {"\x1b[C", "right"}, {"\x1b[D", "left"},
	} {
		m := NewMapper()
		in := press(m, []byte(seq.bytes)...)
		hold := map[string]bool{"up": in.Up, "down": in.Down, "right": in.Right, "left": in.Left}[seq.field]
		if !hold {
			t.Errorf("%q -> %v, want %s held", seq.bytes, in, seq.field)
		}
	}
}

func TestPartialEscapeSequenceAcrossFeeds(t *testing.T) {
	m := NewMapper()
	m.Feed([]byte{'\x1b'})
	if in := m.Poll(); in.Right || in.Quit {
		t.Errorf("incomplete sequence fired early: %+v", in)
	}
	m.Feed([]byte("[C"))
	if in := m.Poll(); !in.Right {
		t.Errorf("completed sequence not decoded: %+v", in)
	}
}

func TestSS3Arrows(t *testing.T) {
	m := NewMapper()
	if in := press(m, "\x1bOD"[0], "\x1bOD"[1], "\x1bOD"[2]); !in.Left {
		t.Errorf("SS3 D -> %+v", in)
	}
	for range holdWindow + 2 {
		m.Poll()
	}
	if in := press(m, "\x1bOA"[0], "\x1bOA"[1], "\x1bOA"[2]); !in.Up {
		t.Errorf("SS3 A -> %+v", in)
	}
}

func TestKittyPressAndRelease(t *testing.T) {
	m := NewMapper()
	// CSI 97;1:1 u = press 'a'
	m.Feed([]byte("\x1b[97;1:1u"))
	if in := m.Poll(); !in.Left {
		t.Fatalf("kitty press a -> %+v", in)
	}
	// Sticky: still held after the window with no repeats.
	for range holdWindow * 2 {
		m.Poll()
	}
	if in := m.Poll(); !in.Left {
		t.Errorf("kitty key expired without release: %+v", in)
	}
	// CSI 97;1:3 u = release 'a'
	m.Feed([]byte("\x1b[97;1:3u"))
	if in := m.Poll(); in.Left {
		t.Errorf("kitty release not honored: %+v", in)
	}
}

func TestKittyArrowWithEventTypes(t *testing.T) {
	m := NewMapper()
	m.Feed([]byte("\x1b[1;1:1C")) // press right arrow
	if in := m.Poll(); !in.Right {
		t.Fatalf("kitty arrow press -> %+v", in)
	}
	m.Feed([]byte("\x1b[1;1:3C")) // release right arrow
	if in := m.Poll(); in.Right {
		t.Errorf("kitty arrow release not honored: %+v", in)
	}
}

func TestLegacyDecayWindow(t *testing.T) {
	m := NewMapper()
	m.Feed([]byte{'d'})
	held := 0
	for i := range holdWindow + 2 {
		if m.Poll().Right {
			held = i + 1
		}
	}
	if held == 0 {
		t.Fatal("legacy press never reported held")
	}
	if held > holdWindow+1 {
		t.Errorf("key held for %d polls, want <= %d (press poll + window)", held, holdWindow+1)
	}
}

func TestRepeatKeepsKeyAlive(t *testing.T) {
	m := NewMapper()
	for i := range holdWindow * 3 {
		if i%10 == 0 {
			m.Feed([]byte{'d'})
		}
		if !m.Poll().Right {
			t.Fatalf("key expired at poll %d despite repeats", i)
		}
	}
}

func TestEdgeKeysFireOncePerPress(t *testing.T) {
	m := NewMapper()
	m.Feed([]byte{'q'})
	if !m.Poll().Quit {
		t.Fatal("quit edge missing")
	}
	if m.Poll().Quit {
		t.Error("quit edge fired twice for one press")
	}

	m.Feed([]byte{'p'})
	if !m.Poll().Pause {
		t.Fatal("pause edge missing")
	}
	if m.Poll().Pause {
		t.Error("pause edge repeated")
	}

	m.Feed([]byte{'r'})
	if !m.Poll().Restart {
		t.Fatal("restart edge missing")
	}
}

func TestAnyKeyOnEnterAndUnknown(t *testing.T) {
	m := NewMapper()
	if in := press(m, '\r'); !in.AnyKey {
		t.Error("enter did not produce AnyKey")
	}
	if in := press(m, 'z'); !in.AnyKey {
		t.Error("unknown key did not produce AnyKey")
	}
	if in := press(m, 'z'); in.Restart || in.Pause || in.Quit {
		t.Error("unknown key produced a special edge")
	}
}

func TestLoneEscapeQuits(t *testing.T) {
	m := NewMapper()
	m.Feed([]byte{'\x1b'})
	m.Poll() // sequence bytes may still be in flight
	m.Poll()
	in := m.Poll() // stale threshold reached: this was a real ESC press
	if !in.Quit {
		t.Errorf("lone ESC -> %+v, want quit", in)
	}
}

func TestUnknownCSIConsumedSilently(t *testing.T) {
	m := NewMapper()
	m.Feed([]byte("\x1b[12;24;80;1R")) // cursor position report
	if in := m.Poll(); in.Quit || in.Pause || in.Restart || in.AnyKey {
		t.Errorf("cursor report produced events: %+v", in)
	}
	if in := press(m, 'a'); !in.Left {
		t.Error("buffer corrupted by unknown CSI")
	}
}

func TestAltKeyTreatedAsPlain(t *testing.T) {
	m := NewMapper()
	if in := press(m, '\x1b', 'd'); !in.Right {
		t.Errorf("ESC d (alt-d) -> %+v, want right", in)
	}
}

func TestKittyReleaseOfUnmappedKeyClearsNothing(t *testing.T) {
	m := NewMapper()
	m.Feed([]byte{'a'}) // left (legacy decay)
	m.Feed([]byte("\x1b[122;1:1u"))
	m.Poll()
	// Releasing 'z' must not clear the held 'a'.
	m.Feed([]byte("\x1b[122;1:3u"))
	if in := m.Poll(); !in.Left {
		t.Errorf("unmapped release cleared left: %+v", in)
	}
}

func TestConcurrentFeedAndPoll(t *testing.T) {
	m := NewMapper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 500 {
			m.Feed([]byte("adwsx"))
		}
	}()
	for range 500 {
		m.Poll()
	}
	<-done
}

func TestCtrlCQuits(t *testing.T) {
	m := NewMapper()
	if in := press(m, 0x03); !in.Quit {
		t.Errorf("raw 0x03 (Ctrl+C) -> %+v, want quit", in)
	}
	m2 := NewMapper()
	m2.Feed([]byte("\x1b[99;5:1u")) // kitty ctrl+c press
	if in := m2.Poll(); !in.Quit {
		t.Errorf("kitty CSI 99;5:1u -> %+v, want quit", in)
	}
	m3 := NewMapper()
	m3.Feed([]byte("\x1b[99;1:1u")) // plain 'c' press, no ctrl
	if in := m3.Poll(); in.Quit {
		t.Errorf("plain 'c' -> quit, want any-key only: %+v", in)
	}
}

func TestStaleSequenceFlushFreesKeys(t *testing.T) {
	// An incomplete CSI (bytes lost on a slow link) used to swallow every
	// following key, because letters parse as CSI final bytes.
	m := NewMapper()
	m.Feed([]byte{'\x1b', '['})
	m.Poll()
	m.Poll()
	m.Poll() // stale: flushed
	m.Feed([]byte{'w'})
	if in := m.Poll(); !in.Up {
		t.Errorf("key after stale partial sequence not delivered: %+v", in)
	}
}

func TestSplitSequenceAcrossSlowFeed(t *testing.T) {
	// Arrow bytes split by SSH must not fire the lone-ESC quit while the
	// rest is still in flight.
	m := NewMapper()
	m.Feed([]byte{'\x1b'})
	if in := m.Poll(); in.Quit {
		t.Fatal("lone ESC quit fired too early (sequence still split)")
	}
	m.Feed([]byte("[C"))
	if in := m.Poll(); !in.Right {
		t.Errorf("split arrow not decoded: %+v", in)
	}
}

func TestLoneEscapeStillQuitsEventually(t *testing.T) {
	m := NewMapper()
	m.Feed([]byte{'\x1b'})
	m.Poll()
	m.Poll()
	if in := m.Poll(); !in.Quit {
		t.Errorf("held lone ESC -> %+v, want quit", in)
	}
}

func TestTildeSequencesAreSilent(t *testing.T) {
	m := NewMapper()
	m.Feed([]byte("\x1b[2~")) // shift-insert etc: must not produce AnyKey
	if in := m.Poll(); in.AnyKey || in.Pause || in.Quit {
		t.Errorf("CSI ~ produced events: %+v", in)
	}
}
