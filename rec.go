package main

// Session recording: one 16-bit input bitmask per 60 Hz tick. The engine is
// fully deterministic (no randomness, no wall-clock dependence), so a score
// claim is verifiable by replaying exactly this sequence through a fresh
// engine and comparing the final score.

import "mario/engine"

// scoreEngineVersion tags the gameplay rules a recording was made against.
// Bump whenever engine behaviour or built-in levels change in a way that
// affects outcomes; the verifier rejects rows from other versions.
const scoreEngineVersion = "v1"

// Input bits. Order is fixed by the recording format (v1).
const (
	bitLeft    = 1 << iota // 0x001
	bitRight               // 0x002
	bitUp                  // 0x004
	bitDown                // 0x008
	bitRun                 // 0x010
	bitQuit                // 0x020
	bitPause               // 0x040
	bitRestart             // 0x080
	bitAnyKey              // 0x100
)

// maxRecordTicks caps a recording at 30 minutes of play. Longer sessions are
// kept un-submittable rather than truncated (truncation would silently
// misrepresent the run).
const maxRecordTicks = 60 * 60 * 30

// recording is the JSON wire format stored in the scores.replay column.
type recording struct {
	V int      `json:"v"`
	I []uint16 `json:"i"`
}

// recorder captures inputs as they are polled from the live game loop.
type recorder struct {
	rec   recording
	lost  bool // session exceeded maxRecordTicks
	count int
}

func newRecorder() *recorder {
	return &recorder{rec: recording{V: 1}}
}

// record appends the input for one tick.
func (r *recorder) record(in engine.Input) {
	if r.count++; r.count > maxRecordTicks {
		r.lost = true
		return
	}
	r.rec.I = append(r.rec.I, encodeInput(in))
}

// submittable reports whether the recording is complete enough to verify.
func (r *recorder) submittable() bool {
	return !r.lost && len(r.rec.I) > 0 && r.rec.V == 1
}

// valid reports whether rec can be replayed at all.
func (rec recording) valid() bool {
	return rec.V == 1 && len(rec.I) > 0 && len(rec.I) <= maxRecordTicks
}

func encodeInput(in engine.Input) uint16 {
	var v uint16
	set := func(b uint16, on bool) {
		if on {
			v |= b
		}
	}
	set(bitLeft, in.Left)
	set(bitRight, in.Right)
	set(bitUp, in.Up)
	set(bitDown, in.Down)
	set(bitRun, in.Run)
	set(bitQuit, in.Quit)
	set(bitPause, in.Pause)
	set(bitRestart, in.Restart)
	set(bitAnyKey, in.AnyKey)
	return v
}

func decodeInput(v uint16) engine.Input {
	return engine.Input{
		Left:    v&bitLeft != 0,
		Right:   v&bitRight != 0,
		Up:      v&bitUp != 0,
		Down:    v&bitDown != 0,
		Run:     v&bitRun != 0,
		Quit:    v&bitQuit != 0,
		Pause:   v&bitPause != 0,
		Restart: v&bitRestart != 0,
		AnyKey:  v&bitAnyKey != 0,
	}
}
