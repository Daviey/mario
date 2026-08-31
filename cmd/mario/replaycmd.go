package main

// Operator tooling for leaderboard recordings: a dropped or suspicious
// score row is diagnosed in two steps — dump the row's recording from
// the database, then trace it tick by tick against the current engine
// and read where the run dies (and why).

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Daviey/mario/board"
	"github.com/Daviey/mario/replay"
)

// runDumpReplays writes the latest n replay recordings to
// replay-<id>.json files in the working directory, printing one metadata
// line per row (score, level, mode, engine version, surface) so the right
// file is easy to pick before tracing.
func runDumpReplays(n int) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	db, err := board.DBFromEnv(ctx)
	if err != nil {
		return fmt.Errorf("dump-replays: %w; set SUPABASE_DB_PASSWORD in .env", err)
	}
	defer db.Close()
	rows, err := db.Latest(ctx, n)
	if err != nil {
		return fmt.Errorf("dump-replays: %w", err)
	}
	for _, r := range rows {
		name := fmt.Sprintf("replay-%s.json", r.ID)
		if err := os.WriteFile(name, []byte(r.Replay), 0o600); err != nil {
			return fmt.Errorf("dump-replays: %w", err)
		}
		fmt.Printf("%s name=%q score=%d level=%d mode=%s day=%s eng=%s surface=%s viewport=%s\n",
			name, r.Name, r.Score, r.Level, r.Mode, r.Day, r.EngineVersion, r.Surface, r.Viewport)
	}
	if len(rows) == 0 {
		fmt.Println("dump-replays: no replay-backed rows")
	}
	return nil
}

// runReplayTrace prints a recording's tick trace: state transitions with
// lives/score/position and a cause dump at every death. A non-empty
// window ("X0:X1" player-X tiles) adds the per-tick input/physics log
// while the player is inside it — the close-up view for jump postmortems.
// A recording from an older EngineVersion may trace differently than its
// claimed score — that is version skew; the verifier drops such rows as
// `version`.
func runReplayTrace(path string, daily bool, day, window string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("replay: %w", err)
	}
	var x0, x1 float64
	if window != "" {
		x0, x1, err = replay.ParseWindow(window)
		if err != nil {
			return fmt.Errorf("replay: %w", err)
		}
	}
	mode := "classic"
	if daily {
		mode = "daily"
		if day == "" {
			day = time.Now().UTC().Format("2006-01-02")
		}
	}
	levels, err := replay.DayLevels(mode, day)
	if err != nil {
		return fmt.Errorf("replay: %w", err)
	}
	fmt.Printf("replay: file=%s mode=%s", path, mode)
	if daily {
		// day= is the resolved challenge date (flag or today's, the
		// same default the level rebuild above used).
		fmt.Printf(" day=%s", day)
	}
	fmt.Printf(" engine=%s", board.EngineVersion)
	if window != "" {
		fmt.Printf(" window=[%g,%g)", x0, x1)
	}
	fmt.Println()
	var res replay.Result
	if window != "" {
		res, err = replay.TraceWindow(levels, mode, string(data), os.Stdout, x0, x1)
	} else {
		res, err = replay.Trace(levels, mode, string(data), os.Stdout)
	}
	if err != nil {
		return fmt.Errorf("replay: %w", err)
	}
	fmt.Printf("replayed: score=%d level=%d state=%v\n", res.Score, res.Level, res.State)
	return nil
}
