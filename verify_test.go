package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"mario/board"
	"mario/engine"
)

// demoRecording replays the same scripted session runDemo uses and returns
// its recording.
func demoRecording(t *testing.T, ticks int) recording {
	t.Helper()
	r := newRecorder()
	for t := range ticks {
		r.record(scriptInput(t))
	}
	return r.rec
}

func TestReplayReproducesDemoScore(t *testing.T) {
	// The load-bearing property of the whole leaderboard: recorded inputs,
	// replayed through a fresh engine, reproduce the live score exactly.
	levels := engine.DefaultLevels()
	rec := demoRecording(t, 6000)

	g := engine.NewGame(levels, 20, engine.LevelHeight)
	for t := range 6000 {
		g.Update(scriptInput(t))
	}
	score, state, ok := replayRecording(levels, rec)
	if !ok {
		t.Fatal("demo recording should replay")
	}
	if score != g.Score {
		t.Fatalf("replay score %d != live score %d", score, g.Score)
	}
	if state != g.State {
		t.Fatalf("replay state %v != live state %v", state, g.State)
	}
}

func TestReplayDeterministicAcrossRuns(t *testing.T) {
	levels := engine.DefaultLevels()
	rec := demoRecording(t, 3000)
	s1, st1, _ := replayRecording(levels, rec)
	s2, st2, _ := replayRecording(levels, rec)
	if s1 != s2 || st1 != st2 {
		t.Fatalf("replay not deterministic: (%d,%v) vs (%d,%v)", s1, st1, s2, st2)
	}
}

// Viewport size must not affect simulation — players on different terminal
// widths submit into the same verified pool.
func TestReplayViewportIndependent(t *testing.T) {
	rec := demoRecording(t, 2000)
	score, _, ok := replayRecording(engine.DefaultLevels(), rec)
	if !ok {
		t.Fatal("replay should work")
	}
	g := engine.NewGame(engine.DefaultLevels(), 60, engine.LevelHeight)
	for _, v := range rec.I {
		g.Update(decodeInput(v))
	}
	if g.Score != score {
		t.Fatalf("score depends on viewport: %d vs %d", g.Score, score)
	}
}

func makeRow(name string, score int, rec recording, version string) board.Row {
	replay, _ := json.Marshal(rec)
	return board.Row{Name: name, Score: score, Replay: replay, EngineVersion: version}
}

func TestVerifyRowDecisions(t *testing.T) {
	levels := engine.DefaultLevels()
	rec := demoRecording(t, 3000)
	score, _, _ := replayRecording(levels, rec)

	cases := []struct {
		row  board.Row
		want verifyResult
	}{
		{makeRow("GOOD", score, rec, scoreEngineVersion), verifyOK},
		{makeRow("LIAR", score+100, rec, scoreEngineVersion), verifyScoreMismatch},
		{makeRow("OLD", score, rec, "v0"), verifyBadVersion},
		{makeRow("JUNK", score, recording{V: 1}, scoreEngineVersion), verifyBadReplay},
		{makeRow("BADVER", score, recording{V: 9, I: []uint16{1}}, scoreEngineVersion), verifyBadReplay},
	}
	for _, c := range cases {
		if got := verifyRow(levels, c.row); got != c.want {
			t.Errorf("verifyRow(%s) = %v, want %v", c.row.Name, got, c.want)
		}
	}
}

func TestVerifyRowGarbageReplayJSON(t *testing.T) {
	row := board.Row{
		Name: "BROKEN", Score: 10, EngineVersion: scoreEngineVersion,
		Replay: json.RawMessage(`{"not":"a recording"}`),
	}
	if got := verifyRow(engine.DefaultLevels(), row); got != verifyBadReplay {
		t.Errorf("garbage replay = %v, want verifyBadReplay", got)
	}
}

func TestParseRecording(t *testing.T) {
	rec, err := parseRecording([]byte(`{"v":1,"i":[1,2,3]}`))
	if err != nil || rec.V != 1 || len(rec.I) != 3 {
		t.Fatalf("parseRecording = %+v, %v", rec, err)
	}
	if _, err := parseRecording([]byte(`nope`)); err == nil {
		t.Fatal("invalid JSON must error")
	}
}

func TestPrintScores(t *testing.T) {
	var buf bytes.Buffer
	printScores(&buf, nil)
	if !strings.Contains(buf.String(), "no verified scores") {
		t.Fatalf("empty board message missing: %q", buf.String())
	}
	buf.Reset()
	printScores(&buf, []board.Row{{Name: "DAVE", Score: 12500}})
	out := buf.String()
	for _, want := range []string{"DAVE", "12500", "1"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q in %q", want, out)
		}
	}
}
