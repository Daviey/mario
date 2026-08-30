package input

import (
	"testing"

	"github.com/Daviey/mario/engine"
)

// Feel tests: the three reported gameplay complaints, replayed through the
// real mapper + engine exactly as a terminal session would drive them.
func feelGame(t *testing.T) (*Mapper, *engine.Game, float64) {
	t.Helper()
	g := engine.NewGame(engine.DefaultLevels(), 30, 10)
	m := NewMapper()
	// Dismiss the title screen the way a real session does (Enter), so
	// the tests exercise StatePlaying rather than the any-key gate that
	// would otherwise consume the first movement press.
	m.Feed([]byte{'\r'})
	g.Update(m.Poll()) // title -> world card
	m.Feed([]byte{'\r'})
	g.Update(m.Poll()) // any key skips the card -> playing
	for range 3 {
		g.Update(m.Poll()) // settle on the ground
	}
	return m, g, g.Player.Pos.X
}

// nudge, not half a second of walking. The old 30-tick phantom window moved
// Mario ~2.4 tiles per tap — the "one keypress moves 3 spaces" overrun.
func TestLegacyTapDoesNotOverrun(t *testing.T) {
	m, g, x0 := feelGame(t)
	m.Feed([]byte{'d'})
	for range 90 {
		g.Update(m.Poll())
	}
	if moved := g.Player.Pos.X - x0; moved > 0.9 {
		t.Errorf("single legacy tap moved %.2f tiles, want <= 0.9", moved)
	}
}

// Quick left-right: tapping left while right is still inside its hold
// window must reverse the player, not cancel to a standstill (the old both-
// held dir=0 stall). Red pre-fix: the two directions cancelled for ~20
// ticks and the player never came back.
func TestQuickLeftRightReverses(t *testing.T) {
	m, g, _ := feelGame(t)
	m.Feed([]byte{'d'})
	for range 10 {
		g.Update(m.Poll()) // moving right, phantom right still active
	}
	m.Feed([]byte{'a'})
	xTap := g.Player.Pos.X
	for i := range 30 {
		if i%5 == 0 {
			m.Feed([]byte{'a'}) // OS key-repeat keeps left held
		}
		g.Update(m.Poll())
	}
	if x := g.Player.Pos.X; x >= xTap-0.1 {
		t.Errorf("quick left tap did not reverse the player: x %.2f -> %.2f", xTap, x)
	}
}

// Same reversal under the kitty protocol, with the held right key
// streaming repeat events: repeats must not outrank the fresh press.
func TestKittyReversalBeatsHeldRepeats(t *testing.T) {
	m, g, _ := feelGame(t)
	m.Feed([]byte("\x1b[100;1:1u")) // 'd' press
	for i := range 40 {
		if i%2 == 0 {
			m.Feed([]byte("\x1b[100;1:2u")) // OS repeat stream
		}
		g.Update(m.Poll())
	}
	m.Feed([]byte("\x1b[97;1:1u")) // fresh 'a' press while 'd' still held
	xTap := g.Player.Pos.X
	for range 30 {
		g.Update(m.Poll())
	}
	if x := g.Player.Pos.X; x >= xTap-0.1 {
		t.Errorf("fresh left press lost to right's repeat stream: x %.2f -> %.2f", xTap, x)
	}
}

// Jump taps: each space press must produce a rising edge. With release
// events clearing Up between taps, a retap after landing fires a full
// second jump instead of being eaten by a stale hold.
func TestJumpRetapFiresImmediately(t *testing.T) {
	m, g, _ := feelGame(t)
	tap := func() bool {
		m.Feed([]byte("\x1b[32;1:1u")) // space press
		g.Update(m.Poll())
		fired := g.Player.Vel.Y < -0.4
		m.Feed([]byte("\x1b[32;1:3u")) // space release
		g.Update(m.Poll())
		return fired
	}
	if !tap() {
		t.Fatal("first jump tap did not fire")
	}
	for i := 0; i < 300 && !g.Player.Grounded; i++ {
		g.Update(m.Poll())
	}
	if !g.Player.Grounded {
		t.Fatal("player never landed; test assumption broken")
	}
	if !tap() {
		t.Fatal("second jump tap was eaten by a stale Up hold")
	}
}

// The reported bug: every process starts with a blank mapper, so the first
// hold of each key stalled for the OS repeat delay — every session, forever,
// because a player who lets go during the stall never calibrates. Learning
// must survive a restart: the first hold of a new session plays like the
// last hold of the previous one.
func TestCalibrationSurvivesRestart(t *testing.T) {
	m := NewMapper()
	calibrateHold(m, 'd', 20)
	next := NewMapper()
	next.ApplyCalibration(m.Calibration())
	next.Feed([]byte{'d'}) // first hold of the next session
	if held := countHeldRight(next, maxHoldWindow+2); held < 18 {
		t.Errorf("session restart re-stutters: first hold survived %d ticks, want >= 18", held)
	}
}

// ApplyCalibration is the trust boundary for persisted state: whatever
// the (possibly corrupt or stale) file says gets clamped into range, and
// a habit array that doesn't match this mapper's key set is ignored
// wholesale rather than partially applied.
func TestApplyCalibrationClamps(t *testing.T) {
	m := NewMapper()
	m.ks[kRight].heldHabit = true  // seeded habit for the ignore-rows to preserve
	full := make([]bool, keyCount) // a full array: applied wholesale
	full[kDown] = true
	cases := []struct {
		// after this row's apply: the clamped osDelay, and the habits
		// of kDown and kRight (a full-length array replaces wholesale,
		// a wrong-length one is ignored completely).
		name        string
		cal         Calibration
		osWant      int
		down, right bool
	}{
		{"negative osDelay clamps to 0", Calibration{OSDelay: -7}, 0, false, true},
		{"osDelay above maxHoldWindow clamps down", Calibration{OSDelay: 9999}, maxHoldWindow, false, true},
		{"wrong-length habit array ignored", Calibration{OSDelay: 36, HeldHabit: []bool{true}}, 36, false, true},
		{"right-length habit array applied", Calibration{HeldHabit: full}, 0, true, false},
	}
	for _, c := range cases {
		m.ApplyCalibration(c.cal)
		got := m.Calibration()
		if got.OSDelay != c.osWant {
			t.Errorf("%s: osDelay = %d, want %d", c.name, got.OSDelay, c.osWant)
		}
		if got.HeldHabit[kDown] != c.down {
			t.Errorf("%s: heldHabit[kDown] = %v, want %v", c.name, got.HeldHabit[kDown], c.down)
		}
		if got.HeldHabit[kRight] != c.right {
			t.Errorf("%s: heldHabit[kRight] = %v, want %v", c.name, got.HeldHabit[kRight], c.right)
		}
	}
}
