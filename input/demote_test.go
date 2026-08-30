package input

import (
	"testing"

	"github.com/Daviey/mario/engine"
)

// Single-key-repeat feel tests: Wayland compositors (and tmux passthrough,
// which lands the game in the legacy regime) give the OS autorepeat stream
// to the newest pressed key only — and never resume it for older ones.
// Holding a direction and jumping silences the direction's stream for the
// rest of the flight; these tests replay exactly that byte stream.

// holdDirThenSilence holds 'd' until its repeat stream is established, then
// returns: after this, no more 'd' bytes arrive (the compositor demoted it).
func holdDirThenSilence(t *testing.T, m *Mapper) {
	t.Helper()
	m.Feed([]byte{'d'})
	for i := range 40 {
		m.Poll()
		if i%2 == 0 {
			m.Feed([]byte{'d'}) // repeat stream, 2-tick cadence
		}
	}
}

// TestHoldDirectionSurvivesHeldJump: hold right, press and hold jump. The
// space key takes over the repeat stream; right's own bytes stop forever.
// Right must stay held for the whole flight (space repeats = the keyboard
// is demonstrably active).
func TestHoldDirectionSurvivesHeldJump(t *testing.T) {
	m, g, _ := feelGame(t)
	holdDirThenSilence(t, m)
	m.Feed([]byte{' '}) // jump press: right's stream falls silent here
	for i := range 80 {
		if i%2 == 0 {
			m.Feed([]byte{' '}) // only space repeats now
		}
		in := m.Poll()
		g.Update(in)
		if i > 2 && !in.Right {
			t.Fatalf("right lost %d ticks into a held jump: %+v", i, in)
		}
	}
}

// TestHoldDirectionSurvivesJumpTaps: hold right and tap jump hop after hop.
// Each tap is a single byte (taps are shorter than the OS repeat delay), so
// right is dead for ~the airborne time between taps and must resurrect on
// the next tap. The player keeps moving right across every cycle.
func TestHoldDirectionSurvivesJumpTaps(t *testing.T) {
	m := NewMapper()
	// Model the persisted calibration a real session starts with: the
	// direction key has a held habit and the repeat delay is measured.
	m.ApplyCalibration(Calibration{OSDelay: 36, HeldHabit: make([]bool, keyCount)})
	m.ks[kRight].heldHabit = true

	g := engine.NewGame(engine.DefaultLevels(), 30, 10)
	m.Feed([]byte{'\r'})
	g.Update(m.Poll()) // dismiss title -> world card
	m.Feed([]byte{'\r'})
	g.Update(m.Poll()) // any key skips the card

	holdDirThenSilence(t, m)
	for hop := range 3 {
		x0 := g.Player.Pos.X
		m.Feed([]byte{' '}) // tap: one byte, no repeats
		for range 56 {
			g.Update(m.Poll())
		}
		if x := g.Player.Pos.X; x < x0+1.0 {
			t.Fatalf("hop %d: player stopped moving right (x %.2f -> %.2f); direction lost between taps", hop, x0, x)
		}
	}
}

// TestHeldJumpKeepsHeightWhenSteering: hold jump, then press a direction
// mid-rise. The direction takes over the repeat stream and jump's own bytes
// stop; a premature Up expiry would trigger the engine's jump-cut and bleed
// most of the height. The demotion extension must carry Up past the rise.
func TestHeldJumpKeepsHeightWhenSteering(t *testing.T) {
	m, g, _ := feelGame(t)
	groundY := g.Player.Pos.Y
	m.Feed([]byte{' '}) // jump press
	for i := range 14 {
		if i%2 == 0 {
			m.Feed([]byte{' '}) // jump repeats: the hold is proven
		}
		g.Update(m.Poll())
	}
	m.Feed([]byte{'d'}) // steering press: jump's stream falls silent
	minY := g.Player.Pos.Y
	for i := range 50 {
		if i%2 == 0 {
			m.Feed([]byte{'d'}) // only 'd' repeats now
		}
		g.Update(m.Poll())
		if y := g.Player.Pos.Y; y < minY {
			minY = y
		}
	}
	if rise := groundY - minY; rise < 3.5 {
		t.Errorf("held jump cut short by steering: rose %.2f tiles, want >= 3.5 (a cut jump rises ~1.5)", rise)
	}
}

// TestJumpRetapWhileHoldingDirectionFires: the jump-key extension must be
// bounded — while a direction is held forever (endless repeat stream), a
// released jump key must still expire so the next tap has a rising edge.
// This is the missed-jump regression guard for the demotion extension.
func TestJumpRetapWhileHoldingDirectionFires(t *testing.T) {
	m, g, _ := feelGame(t)
	tap := func() bool {
		m.Feed([]byte{' '})
		g.Update(m.Poll())
		return g.Player.Vel.Y < -0.4
	}
	holdDirThenSilence(t, m)
	if !tap() {
		t.Fatal("first jump tap did not fire")
	}
	// Fall, land, and keep holding right: its repeat stream never comes
	// back, but the demotion extension + space taps keep it held. The
	// second tap must still produce a fresh Up edge.
	for i := 0; i < 100 && !g.Player.Grounded; i++ {
		if i == 50 { // a mid-fall space tap refreshes right's resurrection clock
			m.Feed([]byte{' '})
		}
		g.Update(m.Poll())
	}
	if !g.Player.Grounded {
		t.Fatal("player never landed; test assumption broken")
	}
	if !tap() {
		t.Fatal("second jump tap eaten while holding right: demoted Up never expired")
	}
}

// TestDemotedHoldStillExpires: after the whole keyboard goes quiet, a
// demoted hold must expire within the hold grace — the phantom must be
// bounded (release-both-hands must stop the player).
func TestDemotedHoldStillExpires(t *testing.T) {
	m := NewMapper()
	m.ApplyCalibration(Calibration{OSDelay: 36, HeldHabit: make([]bool, keyCount)})
	m.ks[kRight].heldHabit = true
	holdDirThenSilence(t, m)
	m.Feed([]byte{' '}) // demote right, then total silence
	dead := 0
	for i := range maxHoldWindow + 2 {
		if !m.Poll().Right {
			dead = i + 1
			break
		}
	}
	if dead == 0 {
		t.Errorf("demoted right never expired within %d ticks of silence", maxHoldWindow+2)
	}
}

// TestPlainReleaseNeverResurrects: a hold that expired while the keyboard
// was otherwise quiet was genuinely released — a later key press must not
// stand it back up. Resurrection is only for deaths that happened while
// demoted (silenced by a newer key), whose release was never confirmed.
func TestPlainReleaseNeverResurrects(t *testing.T) {
	m := NewMapper()
	holdDirThenSilence(t, m)
	for range 3 * holdWindow {
		m.Poll() // silence: right expires undemoted
	}
	if m.Poll().Right {
		t.Fatal("right did not expire after release")
	}
	m.Feed([]byte{' '}) // later key press
	if in := m.Poll(); in.Right {
		t.Error("plain release resurrected right on a later key press")
	}
}

// TestLatePressDoesNotResurrectDemotedHold: past the resurrection window,
// even a demoted death counts as released.
func TestLatePressDoesNotResurrectDemotedHold(t *testing.T) {
	m := NewMapper()
	holdDirThenSilence(t, m)
	m.Feed([]byte{' '}) // demote, then long silence: the window runs from
	// the hold's death, which itself lingers one hold grace past the last
	// byte, so wait well past both.
	for range resurrectWindow + maxHoldWindow {
		m.Poll()
	}
	m.Feed([]byte{'x'}) // far too late
	if in := m.Poll(); in.Right {
		t.Error("press beyond the resurrection window resurrected a dead hold")
	}
}

// TestDemotedJumpExpiresBeforeLanding: the jump key's demotion extension is
// capped at upExtendTicks — long enough to cover the jump-cut window (the
// full rise), short enough that the retap edge after landing exists even if
// the steering key never stops repeating.
func TestDemotedJumpExpiresBeforeLanding(t *testing.T) {
	m := NewMapper()
	m.Feed([]byte{' '})
	for i := range 10 {
		if i%2 == 0 {
			m.Feed([]byte{' '}) // proven jump hold
		}
		m.Poll()
	}
	m.Feed([]byte{'d'}) // steering takes the repeat stream
	up := 0
	for i := range 40 {
		if i%2 == 0 {
			m.Feed([]byte{'d'}) // endless steering stream
		}
		if m.Poll().Up {
			up++
		}
	}
	if up < upExtendTicks-4 {
		t.Errorf("demoted jump expired too early: Up %d/40 ticks, want >= %d", up, upExtendTicks-4)
	}
	if up > upExtendTicks+2 {
		t.Errorf("demoted jump outlived its bound: Up %d/40 ticks, want <= %d", up, upExtendTicks+2)
	}
}

// TestJumpNeverResurrects: a hold that died while demoted can be stood
// back up by a later byte for another key — except the jump key, which
// never resurrects (a phantom Up eats retap edges, the missed-jump bug).
// Both Up and Right die demoted here, both well inside the window; one
// later byte brings Right back and must leave Up dead.
func TestJumpNeverResurrects(t *testing.T) {
	m := NewMapper()
	holdDirThenSilence(t, m) // Right: proven hold, stream silenced
	// Prove a jump hold, then hand the repeat stream to Run: the silence
	// demotes Right (jump live), then jump in turn (Run live) — and the
	// demoted jump dies at its upExtendTicks cap while Right stands.
	for i := range 20 {
		if i%2 == 0 {
			m.Feed([]byte{' '})
		}
		m.Poll()
	}
	for i := range upExtendTicks + 6 {
		if i%2 == 0 {
			m.Feed([]byte{'x'})
		}
		if in := m.Poll(); !in.Up && i > upExtendTicks+2 {
			break // the demoted jump has expired at its cap
		}
	}
	// Total silence: Run expires, then the demoted Right after its grace.
	for range maxHoldWindow + 2 {
		m.Poll()
	}
	if m.ks[kUp].deadAt == 0 || m.ks[kRight].deadAt == 0 {
		t.Fatalf("setup: want both keys dead with a resurrection marker, got up=%d right=%d",
			m.ks[kUp].deadAt, m.ks[kRight].deadAt)
	}
	// One byte for another key: Right's death is reopened, Up's never is.
	m.Feed([]byte{'s'})
	in := m.Poll()
	if !in.Down {
		t.Fatal("setup: the later byte never registered")
	}
	if !in.Right {
		t.Error("fresh demoted death did not resurrect right")
	}
	if in.Up {
		t.Error("jump key resurrected: a phantom Up eats retap edges")
	}
}

// TestResurrectionWindowBoundary: the window is inclusive — a byte
// arriving exactly resurrectWindow after the demoted death still stands
// the hold back up; one tick later it counts as released.
func TestResurrectionWindowBoundary(t *testing.T) {
	for _, tc := range []struct {
		name  string
		ticks int
		want  bool
	}{
		{"byte exactly resurrectWindow after death", resurrectWindow - 1, true},
		{"byte one tick past the window", resurrectWindow, false},
	} {
		m := NewMapper()
		holdDirThenSilence(t, m)
		m.Feed([]byte{' '}) // demote right, then total silence
		for range 8 * maxHoldWindow {
			m.Poll()
			if m.ks[kRight].deadAt != 0 {
				break
			}
		}
		if m.ks[kRight].deadAt == 0 {
			t.Fatalf("%s: right never died demoted; test assumption broken", tc.name)
		}
		for range tc.ticks {
			m.Poll()
		}
		m.Feed([]byte{'s'})
		if in := m.Poll(); in.Right != tc.want {
			t.Errorf("%s: right resurrected = %v, want %v", tc.name, in.Right, tc.want)
		}
	}
}
