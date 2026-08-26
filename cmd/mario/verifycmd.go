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
	// The Bearer key must never travel to a URL that came from a
	// CWD-relative .env: a planted file could otherwise redirect the
	// service key to a peer host. Real-env values win per-key in
	// LoadDotEnv, so capture the pre-file state and refuse the one
	// dangerous combination (file-supplied URL + env-supplied key).
	envURL := os.Getenv("SUPABASE_URL")
	envKey := os.Getenv("SUPABASE_SERVICE_KEY")
	board.LoadDotEnv(".env")
	base := os.Getenv("SUPABASE_URL")
	key := os.Getenv("SUPABASE_SERVICE_KEY")
	if base == "" || key == "" {
		return fmt.Errorf("verify: SUPABASE_URL and SUPABASE_SERVICE_KEY must be set")
	}
	if envURL == "" && envKey != "" {
		return fmt.Errorf("verify: SUPABASE_URL is set only by .env while SUPABASE_SERVICE_KEY comes from the environment — refusing to send the service key to a file-configured URL")
	}
	c := board.New(base, key)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Pending carries full 256 KB replay strings per row, so fetch in
	// small batches: two rows fit the client's raised response cap with
	// margin even fully JSON-escaped, and rows are consumed (verified or
	// deleted) as we go, so the loop drains the queue. One bad row can
	// never wedge the run — decode/replay failures delete just that row.
	kept, dropped := 0, 0
	for kept+dropped < 1000 {
		pending, err := c.Pending(ctx, 2)
		if err != nil {
			return fmt.Errorf("verify: fetch pending: %w", err)
		}
		if len(pending) == 0 {
			break
		}
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
					fmt.Printf("KEEP   %-8s %6d  L%d %s\n", board.SanitizeDisplayName(p.Name), p.Score, p.Level, p.Mode)
					continue
				}
			}
			if err := c.DeleteRow(ctx, p.ID); err != nil {
				return fmt.Errorf("verify: delete %s: %w", p.ID, err)
			}
			dropped++
			fmt.Printf("DROP   %-8s %6d  (%s)\n", board.SanitizeDisplayName(p.Name), p.Score, why)
		}
		if len(pending) < 2 {
			break
		}
	}
	fmt.Printf("verified=%d dropped=%d\n", kept, dropped)
	return nil
}
