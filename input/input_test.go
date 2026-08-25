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

func TestKittyLetterPressAndRelease(t *testing.T) {
	// With kitty flags 1|2|8 pushed, plain letters arrive as CSI-u with
	// release events. Without releases a single tap phantom-holds for the
	// whole legacy window — the "one keypress moves 3 spaces" overrun.
	m := NewMapper()
	m.Feed([]byte("\x1b[97;1:1u")) // 'a' press
	if in := m.Poll(); !in.Left {
		t.Fatalf("kitty 'a' press -> %+v, want left", in)
	}
	m.Feed([]byte("\x1b[97;1:3u")) // 'a' release
	if in := m.Poll(); in.Left {
		t.Errorf("kitty 'a' release not honored: %+v", in)
	}
}

func TestKittySpaceReleaseClearsUp(t *testing.T) {
	// Jump: the release must drop Up, otherwise the next tap's rising
	// edge (Up && !prevUp) is eaten and the jump is missed.
	m := NewMapper()
	m.Feed([]byte("\x1b[32;1:1u")) // space press
	if in := m.Poll(); !in.Up {
		t.Fatalf("kitty space press -> %+v, want up", in)
	}
	m.Feed([]byte("\x1b[32;1:3u")) // space release
	if in := m.Poll(); in.Up {
		t.Errorf("kitty space release left Up held: %+v", in)
	}
}

func TestOppositeKeysNewestPressWins(t *testing.T) {
	// Quick left-right: a fresh tap must win over the opposite key still
	// inside its hold window, instead of both cancelling to a standstill.
	m := NewMapper()
	m.Feed([]byte{'d'})
	m.Poll()
	m.Feed([]byte{'a'})
	for i := range 3 {
		in := m.Poll()
		if in.Left && !in.Right {
			return // reversed immediately
		}
		if !in.Left || in.Right {
			t.Fatalf("poll %d: fresh left did not take over from phantom right: %+v", i, in)
		}
	}
}

func TestKittyReleaseOneSourceKeepsOtherHeld(t *testing.T) {
	// Arrow-right and 'd' both mean Right. With all keys reporting
	// release events, releasing one must not drop the other.
	m := NewMapper()
	m.Feed([]byte("\x1b[1;1:1C"))   // right arrow press
	m.Feed([]byte("\x1b[100;1:1u")) // 'd' press
	if in := m.Poll(); !in.Right {
		t.Fatalf("both right sources pressed -> %+v, want right", in)
	}
	m.Feed([]byte("\x1b[1;1:3C")) // right arrow release; 'd' still held
	if in := m.Poll(); !in.Right {
		t.Errorf("arrow release dropped the still-held 'd': %+v", in)
	}
	m.Feed([]byte("\x1b[100;1:3u")) // 'd' release: nothing holds right now
	if in := m.Poll(); in.Right {
		t.Errorf("last right source released but Right still held: %+v", in)
	}
}

func TestUntrackedReleaseStillClears(t *testing.T) {
	// A release whose press we never tracked (e.g. pressed before the
	// protocol push took effect) falls back to clearing the key outright.
	m := NewMapper()
	m.Feed([]byte("\x1b[1;1:1C")) // right arrow press
	m.Poll()
	m.Feed([]byte("\x1b[100;1:3u")) // untracked 'd' release
	if in := m.Poll(); in.Right {
		t.Errorf("untracked release did not clear right: %+v", in)
	}
}

func TestFreshPressBeatsOngoingRepeats(t *testing.T) {
	// A genuinely held key streams repeat events; those repeats must not
	// outrank a NEWER press of the opposite direction.
	m := NewMapper()
	m.Feed([]byte("\x1b[100;1:1u")) // 'd' press
	for range 5 {
		m.Feed([]byte("\x1b[100;1:2u")) // 'd' repeat
		m.Poll()
	}
	m.Feed([]byte("\x1b[97;1:1u")) // fresh 'a' press
	if in := m.Poll(); !in.Left || in.Right {
		t.Fatalf("fresh left lost to right's repeat stream: %+v", in)
	}
}

func TestPressReleaseInSameTickStillRegisters(t *testing.T) {
	// A frame hitch can deliver a whole tap between two polls; the
	// release then clears the key before any Poll ever sampled the
	// press, and the tap would vanish. Press edges must be latched for
	// exactly one Poll.
	m := NewMapper()
	m.Feed([]byte("\x1b[97;1:1u\x1b[97;1:3u")) // 'a' tap in one chunk
	if in := m.Poll(); !in.Left {
		t.Fatalf("collapsed tap invisible to Poll: %+v", in)
	}
	if in := m.Poll(); in.Left {
		t.Error("collapsed tap leaked into a second Poll")
	}
	m2 := NewMapper()
	m2.Feed([]byte("\x1b[32;1:1u\x1b[32;1:3u")) // space tap = jump
	if in := m2.Poll(); !in.Up {
		t.Fatalf("collapsed jump tap invisible to Poll: %+v", in)
	}
	if in := m2.Poll(); in.Up {
		t.Error("collapsed jump tap leaked into a second Poll")
	}
}

func TestRepeatCadenceTightensExpiry(t *testing.T) {
	// First keydown gets the full grace window (the ~500ms OS repeat
	// delay must not drop a held key). Once repeats establish a
	// cadence, silence must expire the key on that cadence instead of
	// the full window.
	m := NewMapper()
	m.Feed([]byte{'d'})
	m.Poll()
	m.Poll()
	m.Feed([]byte{'d'}) // repeat 2 ticks later -> window ~6 ticks
	for i := 1; i <= 7; i++ {
		if !m.Poll().Right {
			t.Fatalf("key expired too early, poll %d after repeat", i)
		}
	}
	// The old fixed window (12) would still hold here (through poll
	// 13); the measured cadence must have tightened expiry to 6.
	for i := 8; i <= 11; i++ {
		if m.Poll().Right {
			t.Fatalf("still held at poll %d after repeat; repeat cadence did not tighten expiry", i)
		}
	}
}

// calibrateHold simulates the legacy-terminal lifecycle of one held key:
// keydown, silence past the initial grace, the OS repeat stream after the
// repeat delay, then release. Afterwards the terminal's repeat delay is
// calibrated and the key has a held habit.
func calibrateHold(m *Mapper, b byte, delayTicks int) {
	m.Feed([]byte{b})
	for range delayTicks + 2 {
		m.Poll() // initial grace expires; the OS delay passes in silence
	}
	m.Feed([]byte{b}) // first repeat (resumed press): delay candidate
	m.Feed([]byte{b}) // cadence byte: candidate adopted, sawRepeat set
	for range 10 {
		m.Poll()
	}
	m.Feed([]byte{b}) // repeat stream continues
	m.Feed([]byte{b})
	for range 40 {
		m.Poll() // release: silence expires the press on its cadence
	}
}

func countHeldRight(m *Mapper, max int) int {
	n := 0
	for i := 0; i < max; i++ {
		if !m.Poll().Right {
			break
		}
		n++
	}
	return n
}

func TestHeldKeyStopsStutteringAfterCalibration(t *testing.T) {
	// On a keydown-only terminal every fresh hold used to expire during
	// the ~500ms OS repeat delay and come back with the first repeat:
	// a stutter on every keydown. After one calibration hold, the next
	// keydown must survive the measured delay in silence.
	m := NewMapper()
	calibrateHold(m, 'd', 20) // 20-tick (~333ms) repeat delay
	m.Feed([]byte{'d'})       // fresh keydown of a calibrated held key
	held := countHeldRight(m, 40)
	if held < 18 {
		t.Errorf("fresh keydown of a calibrated held key survived only %d silent ticks — hold stutters", held)
	}
	if held > 26 {
		t.Errorf("calibrated grace runaway: held %d ticks", held)
	}
}

func TestTapsStaySharpAfterHolds(t *testing.T) {
	// The first tap after a hold may inherit the long grace (a tap and
	// a hold are byte-identical), but its silent expiry must reset the
	// habit so the NEXT tap is precise again.
	m := NewMapper()
	calibrateHold(m, 'd', 20)
	m.Feed([]byte{'d'})
	for range 40 {
		m.Poll() // habit resets when this press expires silently
	}
	m.Feed([]byte{'d'}) // second tap: short grace again
	if held := countHeldRight(m, 40); held > holdWindow+1 {
		t.Errorf("tap after habit reset held %d ticks, want <= %d", held, holdWindow+1)
	}
}

func TestJumpKeyNeverGetsLongGrace(t *testing.T) {
	// A long phantom Up eats retap edges (the missed-jump bug), so jump
	// never inherits the calibrated grace even after held jumps.
	m := NewMapper()
	calibrateHold(m, ' ', 20) // calibrate via a held jump key
	m.Feed([]byte{' '})
	n := 0
	for i := 0; i < 40; i++ {
		if !m.Poll().Up {
			break
		}
		n++
	}
	if n > holdWindow+1 {
		t.Errorf("jump key inherited the calibrated grace: held %d ticks, want <= %d", n, holdWindow+1)
	}
}
