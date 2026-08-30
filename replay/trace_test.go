package replay

import (
	"bytes"
	"strconv"
	"strings"
	"testing"

	"github.com/Daviey/mario/engine"
)

// TestTraceMatchesRun asserts the trace tool's contract: it replays the
// same stream to the same result as Run, prints the card→playing
// transition, flags a suicide death with its input note, and ends with a
// summary line. This is the tool behind `mario -replay`; keep it honest
// or leaderboard postmortems read fiction.
func TestTraceMatchesRun(t *testing.T) {
	levels := engine.DefaultLevels()
	g := engine.NewGame(levels, 40, engine.LevelHeight)
	g.Reset()

	var r Recorder
	r.Start()
	ticks := 0
	suicided := false
	for g.State != engine.StateGameOver && g.State != engine.StateWin && ticks < 3000 {
		in := botInput(ticks)
		if !suicided && ticks == 1500 && g.State == engine.StatePlaying {
			in = engine.Input{Suicide: true}
			suicided = true
		}
		g.Update(in)
		r.Record(in)
		ticks++
	}

	var buf bytes.Buffer
	res, err := Trace(levels, "classic", r.JSON(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	if res.Score != g.Score || res.State != g.State {
		t.Errorf("trace result %+v, live score=%d state=%v", res, g.Score, g.State)
	}
	out := buf.String()
	if !strings.Contains(out, "world-card  -> playing") {
		t.Errorf("trace lacks the run-start transition:\n%s", out)
	}
	if suicided && !strings.Contains(out, "dying") {
		t.Errorf("trace lacks the suicide death despite the input:\n%s", out)
	}
	if suicided && !strings.Contains(out, "suicide") {
		t.Errorf("trace does not flag the suicide input on the death line:\n%s", out)
	}
	if !strings.Contains(out, "END: score=") {
		t.Errorf("trace lacks the END summary:\n%s", out)
	}
}

// TestTraceRejectsUnknownMode mirrors Run's mode contract.
func TestTraceRejectsUnknownMode(t *testing.T) {
	if _, err := Trace(engine.DefaultLevels(), "turbo", "{}", &bytes.Buffer{}); err == nil {
		t.Fatal("trace accepted an unknown mode")
	}
}

// TestTraceDailyMode: Trace must bootstrap daily recordings exactly like
// Run (newReplayGame) — the dump is only useful for postmortems if it
// replays the same game the verifier did.
func TestTraceDailyMode(t *testing.T) {
	lv := engine.DailyLevelFor(2026, 8, 26)
	g := engine.NewGame([]*engine.Level{lv}, 40, engine.LevelHeight)
	g.Daily = true
	g.BeginDaily()
	var r Recorder
	r.Start()
	for t := 0; g.State != engine.StateGameOver && g.State != engine.StateWin && t < 2500; t++ {
		in := botInput(t)
		g.Update(in)
		r.Record(in)
	}
	levels, err := DayLevels("daily", "2026-08-26")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	res, err := Trace(levels, "daily", r.JSON(), &buf)
	if err != nil {
		t.Fatal(err)
	}
	if res.Score != g.Score || res.Level != g.LevelIndex()+1 {
		t.Errorf("daily trace %+v, live %d/L%d", res, g.Score, g.LevelIndex()+1)
	}
	if !strings.Contains(buf.String(), "END: score=") {
		t.Errorf("daily trace lacks the END summary:\n%s", buf.String())
	}
}

// TestTraceDeathCauseNoneFound pins the diagnosis fallback: a suicide
// right at the run's start kills on a clean stretch — no enemy, plant,
// lava, pit or clock — so the death dump must say "none found" instead
// of inventing a cause.
func TestTraceDeathCauseNoneFound(t *testing.T) {
	levels := engine.DefaultLevels()
	g := engine.NewGame(levels, 40, engine.LevelHeight)
	g.Reset()
	var r Recorder
	r.Start()
	killed := false
	for t := 0; g.State != engine.StateGameOver && g.State != engine.StateWin && t < 1200; t++ {
		var in engine.Input
		switch {
		case g.State == engine.StateWorldCard:
			in = engine.Input{AnyKey: true} // skip the card
		case g.State == engine.StatePlaying && !killed:
			in = engine.Input{Suicide: true}
			killed = true
		}
		g.Update(in)
		r.Record(in)
	}
	if !killed {
		t.Fatal("never reached a playable tick to suicide on")
	}
	var buf bytes.Buffer
	if _, err := Trace(levels, "classic", r.JSON(), &buf); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "cause: none found") {
		t.Errorf("clean suicide lacks the none-found fallback:\n%s", buf.String())
	}
}

// TestTraceWindow pins the per-tick window log behind -trace-window: the
// input bits/velocity around a jump takeoff (the "impossible jump" tool —
// the answer is usually the RUN bit decaying airspeed one tick after
// takeoff), strict containment in [x0,x1), and the transition/END lines
// Trace itself prints staying intact.
func TestTraceWindow(t *testing.T) {
	levels := engine.DefaultLevels()
	g := engine.NewGame(levels, 40, engine.LevelHeight)
	g.Reset()
	var r Recorder
	r.Start()
	jumped := false
	prevUp := false
	for ticks := 0; g.State != engine.StateGameOver && g.State != engine.StateWin && ticks < 3000; ticks++ {
		in := botInput(ticks) // progresses through 1-1 (jumps its pipes)
		in.Run = true
		if g.State == engine.StatePlaying && g.Player.Grounded && g.Player.Pos.X >= 30 && g.Player.Pos.X < 40 && !prevUp {
			in.Up = true // force one jump takeoff inside the window
			jumped = true
		}
		g.Update(in)
		r.Record(in)
		prevUp = in.Up
	}
	if !jumped {
		t.Fatal("script never jumped inside the window")
	}
	var buf bytes.Buffer
	res, err := TraceWindow(levels, "classic", r.JSON(), &buf, 30, 40)
	if err != nil {
		t.Fatal(err)
	}
	if res.Score != g.Score || res.State != g.State {
		t.Errorf("window trace result %+v, live score=%d state=%v", res, g.Score, g.State)
	}
	out := buf.String()
	if !strings.Contains(out, "JUMP PRESS") {
		t.Errorf("window log lacks the jump-press marker:\n%s", out)
	}
	if !strings.Contains(out, "RUN=1)") {
		t.Errorf("window log lacks the run-bit column:\n%s", out)
	}
	if !strings.Contains(out, "END: score=") || !strings.Contains(out, "world-card  -> playing") {
		t.Errorf("window trace dropped the plain-Trace output contract:\n%s", out)
	}
	windowLines := 0
	for _, ln := range strings.Split(out, "\n") {
		if !strings.Contains(ln, "in(L=") {
			continue
		}
		windowLines++
		i := strings.Index(ln, "pos=(")
		j := strings.Index(ln[i:], ",")
		x, err := strconv.ParseFloat(strings.TrimSpace(ln[i+5:i+j]), 64)
		if err != nil {
			t.Fatalf("window line not parseable: %q", ln)
		}
		if x < 30 || x >= 40 {
			t.Errorf("window line outside [30,40): %q", ln)
		}
	}
	if windowLines == 0 {
		t.Errorf("window log is empty:\n%s", out)
	}
}

// TestParseWindow: the -trace-window argument grammar.
func TestParseWindow(t *testing.T) {
	for _, tc := range []struct {
		in      string
		x0, x1  float64
		wantErr bool
	}{
		{"59:72", 59, 72, false},
		{"0.5:4", 0.5, 4, false},
		{"59", 0, 0, true},
		{"59:", 0, 0, true},
		{":72", 0, 0, true},
		{"a:b", 0, 0, true},
		{"72:59", 0, 0, true},
		{"59:59", 0, 0, true},
	} {
		x0, x1, err := ParseWindow(tc.in)
		if tc.wantErr {
			if err == nil {
				t.Errorf("ParseWindow(%q) = %v,%v,%v; want error", tc.in, x0, x1, err)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseWindow(%q): %v", tc.in, err)
			continue
		}
		if x0 != tc.x0 || x1 != tc.x1 {
			t.Errorf("ParseWindow(%q) = %v,%v; want %v,%v", tc.in, x0, x1, tc.x0, tc.x1)
		}
	}
}
