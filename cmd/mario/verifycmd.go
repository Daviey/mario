package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/Daviey/mario/board"
	"github.com/Daviey/mario/replay"
)

// verifyBackend is the operations the verifier loop needs. Both the
// PostgREST service-key client and the direct-Postgres fallback
// implement it.
type verifyBackend interface {
	Pending(ctx context.Context, n int) ([]board.PendingRow, error)
	SetVerified(ctx context.Context, id string) error
	SetScore(ctx context.Context, id string, score int) error
	SetHidden(ctx context.Context, id string) error
	DeleteRow(ctx context.Context, id string) error
}

// errVersionSkew marks a row whose recording predates the running
// engine: the replay is intentionally skipped (cross-version recordings
// diverge by design) and the switch must treat the row as unreplayable,
// never as a score correction.
var errVersionSkew = errors.New("engine version skew")

// runVerifyPending replays every unverified score that carries a
// recording and settles each row: reproduces claim and level → verified;
// reproduces a different score → the replay is authoritative, so the row
// is CORRECTED to the replayed score and verified (the claim came from
// the same machine that recorded the inputs — an input-delivery loss
// must fix the row, never delete it); wrong level, replay failure, or a
// cross-version recording → corrected score if replayable, hidden but
// KEPT either way (row + recording stay in the database for forensics;
// board_rows filters hidden out). It runs in the GitHub Action verifier
// with the service-role key; the publishable key cannot see unverified
// rows. Locally, the direct database path (SUPABASE_DB_PASSWORD) is the
// fallback when no service key is present.
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
	// margin even fully JSON-escaped. Rows are consumed (verified,
	// corrected, or hidden) as we go; the 1000-row bound is a runaway
	// guard, not a queue limit — a backlog past it spills to the next
	// scheduled run (the Action verifies every 15 minutes). One bad row
	// can never wedge the run — decode/replay failures hide just that
	// row.
	kept, corrected, hidden, versionHides := 0, 0, 0, 0
	var detHides []string // hidden rows: level mismatch or replay failure
	for kept+corrected+hidden+versionHides < 1000 {
		pending, err := c.Pending(ctx, 2)
		if err != nil {
			return fmt.Errorf("verify: fetch pending: %w", err)
		}
		if len(pending) == 0 {
			break
		}
		for _, p := range pending {
			versioned := p.EngineVersion != board.EngineVersion
			var res replay.Result
			var runErr error
			if versioned {
				// Expected once per gameplay-change release: pending
				// rows recorded on a previous build cannot replay
				// against this one. Not a bug signal — hide, keep.
				// The sentinel keeps the switch's FIX branch (which
				// trusts res) away from a row the replay never ran.
				runErr = errVersionSkew
			} else {
				levels, err := replay.DayLevels(p.Mode, p.Day)
				if err != nil {
					runErr = err
				} else {
					res, runErr = replay.Run(levels, p.Mode, p.Replay)
				}
			}
			switch {
			case runErr == nil && res.Score != p.Score:
				// The replay is authoritative: it re-executes the
				// very inputs the engine consumed live, so a
				// disagreement means the claim overstated what the
				// recording did (input loss en route to the engine).
				// Correct the row, never delete it.
				if err := c.SetScore(ctx, p.ID, res.Score); err != nil {
					return fmt.Errorf("verify: correct %s: %w", p.ID, err)
				}
				if err := c.SetVerified(ctx, p.ID); err != nil {
					return fmt.Errorf("verify: mark %s: %w", p.ID, err)
				}
				corrected++
				fmt.Printf("FIX    %-8s %6d→%d  L%d %-7s %s\n", board.SanitizeDisplayName(p.Name), p.Score, res.Score, p.Level, p.Mode, playContext(p))
			case runErr == nil && res.Level != p.Level:
				// Wrong level claim: not a plain score correction
				// (the level column feeds L<n> display), so hide.
				if err := c.SetHidden(ctx, p.ID); err != nil {
					return fmt.Errorf("verify: hide %s: %w", p.ID, err)
				}
				hidden++
				detHides = append(detHides, fmt.Sprintf("%-8s %6d (replay reached level %d, row claims %d)", board.SanitizeDisplayName(p.Name), p.Score, res.Level, p.Level))
				fmt.Printf("HIDE   %-8s %6d  (replay reached level %d, row claims %d)\n", board.SanitizeDisplayName(p.Name), p.Score, res.Level, p.Level)
			case runErr != nil:
				// Version-skewed, undecodable, or unreplayable
				// recording: hide, keep.
				if err := c.SetHidden(ctx, p.ID); err != nil {
					return fmt.Errorf("verify: hide %s: %w", p.ID, err)
				}
				hidden++
				if versioned {
					versionHides++
					fmt.Printf("HIDE   %-8s %6d  (engine version %q != %q)\n", board.SanitizeDisplayName(p.Name), p.Score, p.EngineVersion, board.EngineVersion)
				} else {
					detHides = append(detHides, fmt.Sprintf("%-8s %6d (%s)", board.SanitizeDisplayName(p.Name), p.Score, runErr))
					fmt.Printf("HIDE   %-8s %6d  (%s)\n", board.SanitizeDisplayName(p.Name), p.Score, runErr)
				}
			default:
				if err := c.SetVerified(ctx, p.ID); err != nil {
					return fmt.Errorf("verify: mark %s: %w", p.ID, err)
				}
				kept++
				fmt.Printf("KEEP   %-8s %6d  L%d %-7s %s\n", board.SanitizeDisplayName(p.Name), p.Score, p.Level, p.Mode, playContext(p))
			}
		}
		if len(pending) < 2 {
			break
		}
	}
	fmt.Printf("verified=%d corrected=%d hidden=%d (version=%d determinism=%d)\n", kept, corrected, hidden, versionHides, len(detHides))

	// Step summary for the Action run: one glance shows the queue's
	// health without reading the raw log.
	if p := os.Getenv("GITHUB_STEP_SUMMARY"); p != "" {
		if f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
			fmt.Fprintf(f, "## verify-pending\n\n- verified: %d\n- corrected: %d\n- hidden: %d (version: %d, determinism: %d)\n", kept, corrected, hidden, versionHides, len(detHides))
			for _, d := range detHides {
				fmt.Fprintf(f, "- determinism hide: %s\n", d)
			}
			f.Close()
		}
	}

	// Two or more hidden rows beyond a version boundary is not one
	// corrupted submission — it is a systematic determinism or recording
	// bug silently hiding real players' scores (the recorder-wipe bug of
	// 2026-08-30 dropped EVERY death-containing run this way, silently,
	// for weeks). Corrections are healthy — the replay agreeing with
	// itself enough to fix the row is determinism working; only hiding
	// is failure. Fail the run so it shows red on the dashboard instead
	// of dying quietly in a green log.
	if len(detHides) >= 2 {
		return fmt.Errorf("verify: %d rows hidden by replay failures — systematic loss suspected (see HIDE lines above)", len(detHides))
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
