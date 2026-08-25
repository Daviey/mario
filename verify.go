package main

// Replay-based score verification: a submitted run is replayed headless
// through the same deterministic engine and must reproduce the claimed score
// exactly. Used by -replay (local debugging) and -verify-pending (the GitHub
// Action verifier).

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"mario/board"
	"mario/engine"
)

// replayRecording runs a recording through a fresh engine and returns the
// final score and state. ok is false when the recording is not replayable.
func replayRecording(levels []*engine.Level, rec recording) (score int, state engine.State, ok bool) {
	if !rec.valid() {
		return 0, 0, false
	}
	g := engine.NewGame(levels, 20, engine.LevelHeight)
	for _, v := range rec.I {
		g.Update(decodeInput(v))
	}
	return g.Score, g.State, true
}

// parseRecording decodes the JSON wire format.
func parseRecording(data []byte) (recording, error) {
	var rec recording
	if err := json.Unmarshal(data, &rec); err != nil {
		return rec, err
	}
	return rec, nil
}

// replayFile loads and replays a recording file, printing the outcome.
func replayFile(path string, w io.Writer) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	rec, err := parseRecording(data)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	score, state, ok := replayRecording(engine.DefaultLevels(), rec)
	if !ok {
		return fmt.Errorf("%s: not a valid v1 recording", path)
	}
	fmt.Fprintf(w, "replay: ticks=%d score=%d state=%s\n", len(rec.I), score, state)
	return nil
}

// printScores renders the leaderboard as a plain text table.
func printScores(w io.Writer, rows []board.Row) {
	if len(rows) == 0 {
		fmt.Fprintln(w, "no verified scores yet — be the first!")
		return
	}
	fmt.Fprintf(w, "%4s  %-8s %9s  %s\n", "#", "NAME", "SCORE", "WHEN")
	for i, r := range rows {
		fmt.Fprintf(w, "%4d  %-8s %9d  %s\n", i+1, r.Name, r.Score, r.CreatedAt.Local().Format("2006-01-02 15:04"))
	}
}

// verifyPending fetches every unverified row, replays it and keeps only
// scores that reproduce exactly. Needs the service role key.
func verifyPending(ctx context.Context, c *board.Client, w io.Writer) error {
	rows, err := c.Pending(ctx, 200)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		fmt.Fprintln(w, "verify: nothing pending")
		return nil
	}
	levels := engine.DefaultLevels()
	var kept, rejected int
	for _, r := range rows {
		decision := verifyRow(levels, r)
		switch decision {
		case verifyOK:
			err = c.SetVerified(ctx, r.ID, true)
			kept++
			fmt.Fprintf(w, "verify: KEPT    %-8s %7d (%s)\n", r.Name, r.Score, reason(decision))
		default:
			err = c.Delete(ctx, r.ID)
			rejected++
			fmt.Fprintf(w, "verify: DELETED %-8s %7d (%s)\n", r.Name, r.Score, reason(decision))
		}
		if err != nil {
			return fmt.Errorf("row %s: %w", r.ID, err)
		}
	}
	fmt.Fprintf(w, "verify: %d kept, %d rejected\n", kept, rejected)
	return nil
}

type verifyResult int

const (
	verifyOK verifyResult = iota
	verifyBadVersion
	verifyBadReplay
	verifyScoreMismatch
)

func verifyRow(levels []*engine.Level, r board.Row) verifyResult {
	if r.EngineVersion != scoreEngineVersion {
		return verifyBadVersion
	}
	rec, err := parseRecording(r.Replay)
	if err != nil {
		return verifyBadReplay
	}
	score, _, ok := replayRecording(levels, rec)
	if !ok {
		return verifyBadReplay
	}
	if score != r.Score {
		return verifyScoreMismatch
	}
	return verifyOK
}

func reason(v verifyResult) string {
	switch v {
	case verifyOK:
		return "replay reproduces score"
	case verifyBadVersion:
		return "engine version mismatch"
	case verifyBadReplay:
		return "undecodable/invalid replay"
	case verifyScoreMismatch:
		return "replay diverges from claimed score"
	}
	return "unknown"
}
