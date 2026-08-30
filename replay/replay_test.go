package replay

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	"github.com/Daviey/mario/engine"
)

func TestMaskRoundtrip(t *testing.T) {
	in := engine.Input{Left: true, Up: true, Run: true, Pause: true, Suicide: true, AnyKey: true}
	if got := inputOf(maskOf(in)); got != in {
		t.Errorf("roundtrip: %+v -> %+v", in, got)
	}
	// Exhaustive: every single-bit input survives.
	for bit := range 10 {
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
		case 9:
			in.Suicide = true
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
		// Bounds must be enforced BEFORE any allocation (audit fix: a
		// hostile header once drove make()/expansion to OOM on the
		// verifier). These cases must error cheaply.
		`{"v":1,"ticks":4000000000,"runs":[[0,1]]}`, // header ticks over cap
		`{"v":1,"ticks":-1,"runs":[]}`,              // negative ticks
		`{"v":1,"ticks":1,"runs":[[0,4000000000]]}`, // single run over cap
		`{"v":1,"ticks":` + strconv.Itoa(MaxTicks-1) + `,"runs":[[0,1],[0,` + strconv.Itoa(MaxTicks) + `]]}`, // runs overflow cap mid-stream
		// run count over cap (each run covers >=1 tick): rejected by the
		// run-count bound before any expansion.
		`{"v":1,"ticks":1,"runs":` + runsOf(MaxTicks+1) + `}`,
	}
	for _, s := range bad {
		if _, err := decode(s); err == nil {
			t.Errorf("decode(%q) accepted a bad stream", s)
		}
	}
}

// runsOf builds a runs array of n entries (each [0,1]).
func runsOf(n int) string {
	var b strings.Builder
	b.WriteByte('[')
	for i := range n {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString("[0,1]")
	}
	b.WriteByte(']')
	return b.String()
}

// TestDecodeAcceptsCapBoundaries pins the inclusive edges of the decode
// bounds: exactly MaxTicks ticks (one run) and exactly MaxTicks runs
// (one tick each) are both legal — the reject cases above fire only
// past the cap.
func TestDecodeAcceptsCapBoundaries(t *testing.T) {
	in, err := decode(`{"v":1,"ticks":` + strconv.Itoa(MaxTicks) + `,"runs":[[0,` + strconv.Itoa(MaxTicks) + `]]}`)
	if err != nil {
		t.Fatalf("single run covering the cap: %v", err)
	}
	if len(in) != MaxTicks {
		t.Fatalf("single-run stream decoded %d ticks, want %d", len(in), MaxTicks)
	}
	in, err = decode(`{"v":1,"ticks":` + strconv.Itoa(MaxTicks) + `,"runs":` + runsOf(MaxTicks) + `}`)
	if err != nil {
		t.Fatalf("run count at the cap: %v", err)
	}
	if len(in) != MaxTicks {
		t.Fatalf("max-run stream decoded %d ticks, want %d", len(in), MaxTicks)
	}
}

// TestDecodeMultiRunStream: several runs summing to the header's tick
// count decode in order.
func TestDecodeMultiRunStream(t *testing.T) {
	in, err := decode(`{"v":1,"ticks":5,"runs":[[2,3],[0,2]]}`)
	if err != nil {
		t.Fatal(err)
	}
	want := []engine.Input{{Right: true}, {Right: true}, {Right: true}, {}, {}}
	if len(in) != len(want) {
		t.Fatalf("decoded %d inputs, want %d", len(in), len(want))
	}
	for i := range want {
		if in[i] != want[i] {
			t.Errorf("input %d = %+v, want %+v", i, in[i], want[i])
		}
	}
}

// TestRecorderRLEMerge: consecutive identical inputs compress into ONE
// [mask,count] run — the RLE is what keeps a 130k-tick recording small
// enough to ship.
func TestRecorderRLEMerge(t *testing.T) {
	var r Recorder
	r.Start()
	r.Record(engine.Input{Right: true})
	r.Record(engine.Input{Right: true})
	var s stream
	if err := json.Unmarshal([]byte(r.JSON()), &s); err != nil {
		t.Fatal(err)
	}
	if len(s.Runs) != 1 || s.Runs[0] != [2]int64{int64(maskOf(engine.Input{Right: true})), 2} {
		t.Errorf("runs = %v, want one merged [mask,2] run", s.Runs)
	}
}

// TestRecorderResetForgets: Reset wipes the recording and disarms it —
// JSON stays "" even when stray Record calls follow, so a title-screen
// blip can never ship a stale fragment.
func TestRecorderResetForgets(t *testing.T) {
	var r Recorder
	r.Start()
	r.Record(engine.Input{Right: true})
	r.Reset()
	if s := r.JSON(); s != "" {
		t.Errorf("JSON after Reset = %q, want empty", s)
	}
	if r.Live() || r.Shippable() {
		t.Error("Reset must disarm and unship the recorder")
	}
	r.Record(engine.Input{Left: true})
	if s := r.JSON(); s != "" {
		t.Errorf("Record after Reset produced %q, want nothing", s)
	}
}
