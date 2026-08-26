package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Daviey/mario/board"
	"github.com/Daviey/mario/replay"
)

// runVerifyPending replays every unverified score that carries a recording
// and keeps only rows whose replay reproduces the claimed score and level.
// It runs in the GitHub Action verifier with the service-role key; the
// publishable key cannot see unverified rows.
func runVerifyPending() error {
	board.LoadDotEnv(".env")
	base := os.Getenv("SUPABASE_URL")
	key := os.Getenv("SUPABASE_SERVICE_KEY")
	if base == "" || key == "" {
		return fmt.Errorf("verify: SUPABASE_URL and SUPABASE_SERVICE_KEY must be set")
	}
	c := board.New(base, key)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	pending, err := c.Pending(ctx, 20)
	if err != nil {
		return fmt.Errorf("verify: fetch pending: %w", err)
	}
	kept, dropped := 0, 0
	for _, p := range pending {
		why := ""
		switch {
		case p.EngineVersion != board.EngineVersion:
			why = fmt.Sprintf("engine version %q != %q", p.EngineVersion, board.EngineVersion)
		default:
			levels, err := replay.DayLevels(p.Mode, p.Day)
			if err != nil {
				why = err.Error()
				break
			}
			res, err := replay.Run(levels, p.Mode, p.Replay)
			switch {
			case err != nil:
				why = err.Error()
			case res.Score != p.Score:
				why = fmt.Sprintf("replay scored %d, row claims %d", res.Score, p.Score)
			case res.Level != p.Level:
				why = fmt.Sprintf("replay reached level %d, row claims %d", res.Level, p.Level)
			}
			if why == "" {
				if err := c.SetVerified(ctx, p.ID); err != nil {
					return fmt.Errorf("verify: mark %s: %w", p.ID, err)
				}
				kept++
				fmt.Printf("KEEP   %-8s %6d  L%d %s\n", p.Name, p.Score, p.Level, p.Mode)
				continue
			}
		}
		if err := c.DeleteRow(ctx, p.ID); err != nil {
			return fmt.Errorf("verify: delete %s: %w", p.ID, err)
		}
		dropped++
		fmt.Printf("DROP   %-8s %6d  (%s)\n", p.Name, p.Score, why)
	}
	fmt.Printf("verified=%d dropped=%d pending=%d\n", kept, dropped, len(pending))
	return nil
}
