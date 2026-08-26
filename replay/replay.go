// Package replay records and replays per-tick input streams. The engine is
// fully deterministic, so a run's input log plus its level set reproduces
// the exact score — that is the leaderboard's verification basis.
package replay

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/Daviey/mario/engine"
)

// MaxTicks caps a recordable run (~36 minutes at 60 Hz). Longer runs keep
// playing but their recording is over-cap and cannot be submitted.
const MaxTicks = 130_000

// maskOf packs an Input into a bitmask; inputOf is its inverse.
func maskOf(in engine.Input) uint16 {
	var m uint16
	set := func(bit int, b bool) {
		if b {
			m |= 1 << bit
		}
	}
	set(0, in.Left)
	set(1, in.Right)
	set(2, in.Up)
	set(3, in.Down)
	set(4, in.Run)
	set(5, in.Quit)
	set(6, in.Pause)
	set(7, in.Restart)
	set(8, in.AnyKey)
	return m
}

func inputOf(m uint16) engine.Input {
	bit := func(n int) bool { return m&(1<<n) != 0 }
	return engine.Input{
		Left: bit(0), Right: bit(1), Up: bit(2), Down: bit(3),
		Run: bit(4), Quit: bit(5), Pause: bit(6), Restart: bit(7),
		AnyKey: bit(8),
	}
}

// Recorder accumulates one run's input stream, run-length compressed.
type Recorder struct {
	runs  [][2]uint32 // {mask, count}
	last  uint16
	n     int  // ticks recorded
	full  bool // over MaxTicks: recording continues but is unshippable
	live  bool // armed: recording ticks
	dirty bool // any tick was recorded
}

// Start arms the recorder for a fresh run.
func (r *Recorder) Start() {
	r.runs = nil
	r.n, r.full, r.dirty = 0, false, false
	r.live = true
}

// Reset disarms and forgets everything (title screen, session start).
func (r *Recorder) Reset() {
	r.live = false
	r.Start()
	r.live = false
}

// Live reports whether a run is currently being recorded.
func (r *Recorder) Live() bool { return r.live }

// Finish freezes the recording at run end (game over, win, quit).
func (r *Recorder) Finish() { r.live = false }

// Record appends one tick's input.
func (r *Recorder) Record(in engine.Input) {
	if !r.live {
		return
	}
	m := maskOf(in)
	if r.n == 0 {
		r.last = m
		r.runs = append(r.runs, [2]uint32{uint32(m), 1})
	} else if m == r.last && r.runs[len(r.runs)-1][1] < 1<<31 {
		r.runs[len(r.runs)-1][1]++
	} else {
		r.last = m
		r.runs = append(r.runs, [2]uint32{uint32(m), 1})
	}
	r.n++
	r.dirty = true
	if r.n > MaxTicks {
		r.full = true
	}
}

// Shippable reports whether the recording can back a submission.
func (r *Recorder) Shippable() bool { return r.dirty && !r.full && r.n > 0 && r.n <= MaxTicks }

// JSON encodes the wire format: {"v":1,"ticks":N,"runs":[[mask,count],...]}.
func (r *Recorder) JSON() string {
	if !r.dirty {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, `{"v":1,"ticks":%d,"runs":[`, r.n)
	for i, run := range r.runs {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, "[%d,%d]", run[0], run[1])
	}
	sb.WriteString("]}")
	return sb.String()
}

// stream is the decoded wire format.
type stream struct {
	V     int        `json:"v"`
	Ticks int        `json:"ticks"`
	Runs  [][2]int64 `json:"runs"`
}

func decode(data string) ([]engine.Input, error) {
	var s stream
	if err := json.Unmarshal([]byte(data), &s); err != nil {
		return nil, err
	}
	if s.V != 1 {
		return nil, fmt.Errorf("replay: unknown format version %d", s.V)
	}
	// Bounds come from the peer-controlled wire, so validate everything
	// BEFORE allocating: ticks and the run count are capped up front and
	// each run length is checked against the remaining budget (int64-safe,
	// no len+run overflow) so a hostile header can never drive the make()
	// or the expansion loop past MaxTicks.
	if s.Ticks < 0 || s.Ticks > MaxTicks {
		return nil, fmt.Errorf("replay: %d ticks exceeds cap %d", s.Ticks, MaxTicks)
	}
	if len(s.Runs) > MaxTicks {
		return nil, fmt.Errorf("replay: %d runs exceeds cap %d", len(s.Runs), MaxTicks)
	}
	inputs := make([]engine.Input, 0, s.Ticks)
	for _, run := range s.Runs {
		if run[0] >= 1<<16 {
			return nil, fmt.Errorf("replay: input mask %d out of range", run[0])
		}
		if run[1] <= 0 {
			return nil, fmt.Errorf("replay: non-positive run length %d", run[1])
		}
		if run[1] > MaxTicks || int64(len(inputs)) > int64(MaxTicks)-run[1] {
			return nil, fmt.Errorf("replay: runs exceed tick cap %d", MaxTicks)
		}
		in := inputOf(uint16(run[0]))
		for i := int64(0); i < run[1]; i++ {
			inputs = append(inputs, in)
		}
	}
	if len(inputs) != s.Ticks {
		return nil, fmt.Errorf("replay: runs sum to %d ticks, header says %d", len(inputs), s.Ticks)
	}
	if len(inputs) > MaxTicks {
		return nil, fmt.Errorf("replay: %d ticks exceeds cap %d", len(inputs), MaxTicks)
	}
	return inputs, nil
}

// Result is the outcome of replaying a stream: what the run actually scored.
type Result struct {
	Score int
	Level int // 1-based level reached at the end of the stream
	State engine.State
}

// Run executes a recorded stream against a fresh game over the given
// levels and reports what it scored. mode must match the submitted run
// ("classic" or "daily"); for daily the caller swaps in the challenge
// level for the recorded day beforehand.
func Run(levels []*engine.Level, mode string, data string) (Result, error) {
	inputs, err := decode(data)
	if err != nil {
		return Result{}, err
	}
	g := engine.NewGame(levels, 40, engine.LevelHeight)
	switch mode {
	case "", "classic":
		g.Reset() // title → world card, exactly like a real run start
	case "daily":
		g.Daily = true
		g.BeginDaily()
	default:
		return Result{}, fmt.Errorf("replay: unknown mode %q", mode)
	}
	for _, in := range inputs {
		g.Update(in)
	}
	return Result{Score: g.Score, Level: g.LevelIndex() + 1, State: g.State}, nil
}

// DayLevels rebuilds the level set for a run of the given mode and day.
func DayLevels(mode, day string) ([]*engine.Level, error) {
	if mode != "daily" {
		return engine.DefaultLevels(), nil
	}
	parts := strings.Split(day, "-")
	if len(parts) != 3 {
		return nil, fmt.Errorf("replay: bad day %q", day)
	}
	y, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	d, err3 := strconv.Atoi(parts[2])
	if err1 != nil || err2 != nil || err3 != nil {
		return nil, fmt.Errorf("replay: bad day %q", day)
	}
	return []*engine.Level{engine.DailyLevelFor(y, m, d)}, nil
}
