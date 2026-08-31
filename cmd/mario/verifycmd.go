package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/Daviey/mario/board"
	"github.com/Daviey/mario/replay"
)

// verifyBackend is the three operations the verifier loop needs. Both
// the PostgREST service-key client and the direct-Postgres fallback
// implement it.
type verifyBackend interface {
	Pending(ctx context.Context, n int) ([]board.PendingRow, error)
	SetVerified(ctx context.Context, id string) error
	DeleteRow(ctx context.Context, id string) error
}

// runVerifyPending replays every unverified score that carries a recording
// and keeps only rows whose replay reproduces the claimed score and level.
// It runs in the GitHub Action verifier with the service-role key; the
// publishable key cannot see unverified rows. Locally, the direct
// database path (SUPABASE_DB_PASSWORD) is the fallback when no service
// key is present.
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
	if base == "" {
		return fmt.Errorf("verify: SUPABASE_URL must be set")
	}
	if envURL == "" && envKey != "" {
		return fmt.Errorf("verify: SUPABASE_URL is set only by .env while SUPABASE_SERVICE_KEY comes from the environment — refusing to send the service key to a file-configured URL")
	}
	var c verifyBackend
	switch {
	case key != "":
		c = board.New(base, key)
	default:
		ctx0, cancel0 := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel0()
		db, err := board.DBFromEnv(ctx0)
		if err != nil {
			return fmt.Errorf("verify: no SUPABASE_SERVICE_KEY and no direct-DB fallback (%w); set one of SUPABASE_SERVICE_KEY / SUPABASE_DB_PASSWORD", err)
		}
		defer db.Close()
		fmt.Println("verify: direct database connection (no service key)")
		c = db
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Pending carries full 256 KB replay strings per row, so fetch in
	// small batches: two rows fit the client's raised response cap with
	// margin even fully JSON-escaped. Rows are consumed (verified or
	// deleted) as we go; the 1000-row bound is a runaway guard, not a
	// queue limit — a backlog past it spills to the next scheduled run
	// (the Action verifies every 15 minutes). One bad row can never
	// wedge the run — decode/replay failures delete just that row.
	kept, versionDrops := 0, 0
	var detDrops []string // determinism failures: replay != claim
	for kept+versionDrops+len(detDrops) < 1000 {
		pending, err := c.Pending(ctx, 2)
		if err != nil {
			return fmt.Errorf("verify: fetch pending: %w", err)
		}
		if len(pending) == 0 {
			break
		}
		for _, p := range pending {
			why := ""
			versioned := false
			switch {
			case p.EngineVersion != board.EngineVersion:
				// Expected once per gameplay-change release: pending
				// rows recorded on the previous build cannot replay
				// against the new one. Not a bug signal.
				versioned = true
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
			}
			if why == "" {
				if err := c.SetVerified(ctx, p.ID); err != nil {
					return fmt.Errorf("verify: mark %s: %w", p.ID, err)
				}
				kept++
				fmt.Printf("KEEP   %-8s %6d  L%d %-7s %s\n", board.SanitizeDisplayName(p.Name), p.Score, p.Level, p.Mode, playContext(p))
				continue
			}
			if err := c.DeleteRow(ctx, p.ID); err != nil {
				return fmt.Errorf("verify: delete %s: %w", p.ID, err)
			}
			if versioned {
				versionDrops++
			} else {
				detDrops = append(detDrops, fmt.Sprintf("%-8s %6d (%s)", board.SanitizeDisplayName(p.Name), p.Score, why))
			}
			fmt.Printf("DROP   %-8s %6d  (%s)\n", board.SanitizeDisplayName(p.Name), p.Score, why)
		}
		if len(pending) < 2 {
			break
		}
	}
	dropped := versionDrops + len(detDrops)
	fmt.Printf("verified=%d dropped=%d (version=%d determinism=%d)\n", kept, dropped, versionDrops, len(detDrops))

	// Step summary for the Action run: one glance shows the queue's
	// health without reading the raw log.
	if p := os.Getenv("GITHUB_STEP_SUMMARY"); p != "" {
		if f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			fmt.Fprintf(f, "## verify-pending\n\n- verified: %d\n- dropped: %d (version: %d, determinism: %d)\n", kept, dropped, versionDrops, len(detDrops))
			for _, d := range detDrops {
				fmt.Fprintf(f, "- determinism drop: %s\n", d)
			}
			f.Close()
		}
	}

	// Two or more rows whose replays disagree with their claims is not
	// one corrupted submission — it is a systematic determinism or
	// recording bug deleting real players' scores (the recorder-wipe
	// bug of 2026-08-30 dropped EVERY death-containing run this way,
	// silently, for weeks). Fail the run so it shows red on the
	// dashboard instead of dying quietly in a green log.
	if len(detDrops) >= 2 {
		return fmt.Errorf("verify: %d rows failed replay verification — systematic drop suspected (see DROP lines above)", len(detDrops))
	}
	return nil
}

// playContext renders a pending row's operator-only diagnostics: surface,
// input regime, viewport, TERM, and the user agent (trimmed).
func playContext(p board.PendingRow) string {
	ua := p.UserAgent
	if len(ua) > 60 {
		ua = ua[:60] + "…"
	}
	return fmt.Sprintf("[%s %s %s term=%s ua=%s]", p.Surface, p.InputRegime, p.Viewport, p.Term, ua)
}
