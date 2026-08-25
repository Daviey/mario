package main

// Live integration test: exercises the real submit path (prompt parsing,
// name handling, Entry marshalling, HTTP POST, RLS acceptance) against the
// configured Supabase project. Skipped unless LIVE=1:
//
//	LIVE=1 go test -run TestLiveSubmit -v .
//
// Requires SUPABASE_URL/SUPABASE_KEY in the environment or ./.env.

import (
	"os"
	"strings"
	"testing"

	"mario/board"
	"mario/engine"
)

func TestLiveSubmit(t *testing.T) {
	if os.Getenv("LIVE") != "1" {
		t.Skip("set LIVE=1 to hit the real backend")
	}
	board.LoadDotEnv(".env")
	if os.Getenv("SUPABASE_URL") == "" || os.Getenv("SUPABASE_KEY") == "" {
		t.Fatal("no SUPABASE_URL/SUPABASE_KEY in env or .env")
	}

	// A recording that provably scores: the demo script.
	rec := newRecorder()
	for t := range 6000 {
		rec.record(scriptInput(t))
	}
	score, state, ok := replayRecording(engine.DefaultLevels(), rec.rec)
	if !ok || score == 0 {
		t.Fatalf("demo recording must score, got %d %v", score, state)
	}
	res := &runResult{rec: rec, score: score, state: state}

	var out strings.Builder
	in := strings.NewReader("y\nLIVETST\n")
	if err := maybeSubmit(&out, in, res, true); err != nil {
		t.Fatalf("maybeSubmit: %v (out=%s)", err, out.String())
	}
	if !strings.Contains(out.String(), "pending verification") {
		t.Fatalf("unexpected output: %s", out.String())
	}
	t.Logf("submitted score=%d as LIVETST", score)
}
