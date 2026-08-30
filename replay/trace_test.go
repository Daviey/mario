package replay

import (
	"bytes"
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
