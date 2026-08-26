package replay

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Daviey/mario/engine"
)

func TestMaskRoundtrip(t *testing.T) {
	in := engine.Input{Left: true, Up: true, Run: true, Pause: true, AnyKey: true}
	if got := inputOf(maskOf(in)); got != in {
		t.Errorf("roundtrip: %+v -> %+v", in, got)
	}
	// Exhaustive: every single-bit input survives.
	for bit := range 9 {
		var in engine.Input
		switch bit {
		case 0:
			in.Left = true
		case 1:
			in.Right = true
		case 2:
			in.Up = true
		case 3:
			in.Down = true
		case 4:
			in.Run = true
		case 5:
			in.Quit = true
		case 6:
			in.Pause = true
		case 7:
			in.Restart = true
		case 8:
			in.AnyKey = true
		}
		if got := inputOf(maskOf(in)); got != in {
			t.Errorf("bit %d roundtrip: %+v -> %+v", bit, in, got)
		}
	}
}

func TestRecorderRLEAndWire(t *testing.T) {
	var r Recorder
	r.Start()
	r.Record(engine.Input{Right: true})
	r.Record(engine.Input{Right: true})
	r.Record(engine.Input{Right: true, Run: true})
	r.Record(engine.Input{})
	r.Finish()
	r.Record(engine.Input{Left: true}) // finished: ignored

	s := r.JSON()
	if !strings.Contains(s, `"ticks":4`) {
		t.Errorf("wire: %s", s)
	}
	var probe struct {
		Ticks int `json:"ticks"`
	}
	if err := json.Unmarshal([]byte(s), &probe); err != nil || probe.Ticks != 4 {
		t.Errorf("wire decode: %v %+v", err, probe)
	}
	in, err := decode(s)
	if err != nil {
		t.Fatal(err)
	}
	want := []engine.Input{
		{Right: true}, {Right: true}, {Right: true, Run: true}, {},
	}
	if len(in) != 4 {
		t.Fatalf("decoded %d inputs", len(in))
	}
	for i := range want {
		if in[i] != want[i] {
			t.Errorf("input %d = %+v, want %+v", i, in[i], want[i])
		}
	}
}

func TestRecorderCapAndShippable(t *testing.T) {
	var r Recorder
	r.Start()
	for range MaxTicks + 1 {
		r.Record(engine.Input{Right: true})
	}
	if r.Shippable() {
		t.Error("over-cap recording must not be shippable")
	}
	var fresh Recorder
	fresh.Start()
	fresh.Finish()
	if fresh.Shippable() {
		t.Error("empty recording must not be shippable")
	}
	fresh.Start()
	fresh.Record(engine.Input{})
	if !fresh.Shippable() {
		t.Error("one-tick recording should be shippable")
	}
}

// botInput is a deterministic scripted player (same shape the UI tests use).
func botInput(i int) engine.Input {
	return engine.Input{Right: true, Run: i%3 != 0, Up: i%97 < 22}
}

// TestReplayReproducesLiveRun is the whole point: feeding the same inputs
// through a fresh game must land on the identical score and level.
func TestReplayReproducesLiveRun(t *testing.T) {
	levels := engine.DefaultLevels()
	g := engine.NewGame(levels, 40, engine.LevelHeight)
	g.Reset() // the state replay.Run starts from

	var r Recorder
	r.Start()
	ticks := 0
	for g.State != engine.StateGameOver && g.State != engine.StateWin && ticks < 3000 {
		in := botInput(ticks)
		g.Update(in)
		r.Record(in)
		ticks++
	}

	res, err := Run(levels, "classic", r.JSON())
	if err != nil {
		t.Fatal(err)
	}
	if res.Score != g.Score {
		t.Errorf("replay score %d, live %d", res.Score, g.Score)
	}
	if res.Level != g.LevelIndex()+1 {
		t.Errorf("replay level %d, live %d", res.Level, g.LevelIndex()+1)
	}
	if res.State != g.State {
		t.Errorf("replay state %v, live %v", res.State, g.State)
	}
}

// TestReplayViewportIndependent: the recording was made at ViewW 40; a
// different viewport (the WASM build resizes) must score identically.
func TestReplayViewportIndependent(t *testing.T) {
	levels := engine.DefaultLevels()
	g := engine.NewGame(levels, 20, engine.LevelHeight)
	g.Reset()
	var r Recorder
	r.Start()
	for t := 0; g.State != engine.StateGameOver && g.State != engine.StateWin && t < 2000; t++ {
		in := botInput(t)
		g.Update(in)
		r.Record(in)
	}
	res, err := Run(levels, "classic", r.JSON())
	if err != nil {
		t.Fatal(err)
	}
	if res.Score != g.Score || res.Level != g.LevelIndex()+1 {
		t.Errorf("viewport changed the outcome: replay %d/L%d, live %d/L%d",
			res.Score, res.Level, g.Score, g.LevelIndex()+1)
	}
}

func TestDailyReplayRegeneratesLevel(t *testing.T) {
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
	res, err := Run(levels, "daily", r.JSON())
	if err != nil {
		t.Fatal(err)
	}
	if res.Score != g.Score {
		t.Errorf("daily replay score %d, live %d", res.Score, g.Score)
	}
}

func TestDecodeRejects(t *testing.T) {
	bad := []string{
		``,
		`{"v":2,"ticks":0,"runs":[]}`,
		`{"v":1,"ticks":2,"runs":[[0,1]]}`,      // sum mismatch
		`{"v":1,"ticks":1,"runs":[[999999,1]]}`, // mask out of range
		`{"v":1,"ticks":1,"runs":[[0,0]]}`,      // zero-length run
		`{"v":1,"ticks":1,"runs":[[0,-3]]}`,     // negative run
	}
	for _, s := range bad {
		if _, err := decode(s); err == nil {
			t.Errorf("decode(%q) accepted a bad stream", s)
		}
	}
}
