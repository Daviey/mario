package ui

// Live integration test: drives the real in-game UI machine (game-over
// prompt → name entry → submit) against the configured Supabase project.
// Skipped unless LIVE=1:
//
//	LIVE=1 go test -run TestLiveUISubmit -v .
import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Daviey/mario/board"
	"github.com/Daviey/mario/engine"
	"github.com/Daviey/mario/internal/persist"
	"github.com/Daviey/mario/render"
)

func TestLiveUISubmit(t *testing.T) {
	if os.Getenv("LIVE") != "1" {
		t.Skip("set LIVE=1 to hit the real backend")
	}
	board.LoadDotEnv("../../.env", "../.env", ".env")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // fresh player identity

	g := engine.NewGame(engine.DefaultLevels(), 20, engine.LevelHeight)
	for i := range 6000 {
		g.Update(engine.Input{Right: true, Run: i%3 != 0, Up: i%97 < 22, AnyKey: i == 0})
	}
	if g.State != engine.StateGameOver || g.Score == 0 {
		t.Fatalf("script should end game over with a score, got %v score=%d", g.State, g.Score)
	}

	ui := NewUI(nil, nil) // real board client from env
	if ui.submit == nil {
		t.Fatal("no leaderboard configured")
	}

	snap := ui.Tick(g)
	if snap == nil || snap.Mode != render.UIAsk {
		t.Fatalf("expected ask prompt, got %+v", snap)
	}
	ui.FeedKeys([]byte("yLIVEUI\r"))
	snap = ui.Tick(g)
	if snap.Mode != render.UIBoard || snap.Status != "SUBMITTING" {
		t.Fatalf("expected submitting board, got %+v", snap)
	}

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		ui.mu.Lock()
		status := ui.status
		ui.mu.Unlock()
		if status == "SUBMITTED!" {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	ui.mu.Lock()
	status := ui.status
	ui.mu.Unlock()
	if status != "SUBMITTED!" {
		t.Fatalf("submit did not land: %q", status)
	}

	client, err := board.FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	rows, err := client.Top(context.Background(), 50, "")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rows {
		if r.Name == "LIVEUI" && r.Score == g.Score {
			found = true
		}
	}
	if !found {
		t.Fatalf("LIVEUI %d not on the board: %+v", g.Score, rows)
	}
	t.Logf("LIVEUI %d submitted and visible", g.Score)
}

// TestLiveDailyRoundTrip submits a daily-mode row and checks both boards:
// present on today's daily board, absent from the classic one. LIVE only;
// delete the LIVEDAY row from the scores table when done.
func TestLiveDailyRoundTrip(t *testing.T) {
	if os.Getenv("LIVE") != "1" {
		t.Skip("set LIVE=1 to hit the real backend")
	}
	board.LoadDotEnv("../../.env", "../.env", ".env")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	client, err := board.FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	pc, err := persist.LoadPlayer()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	day := time.Now().UTC().Format("2006-01-02")
	if err := client.Submit(ctx, board.Entry{
		Name: "LIVEDAY", Score: 32100, DeviceID: pc.DeviceID, Mode: "daily", Day: day,
	}); err != nil {
		t.Fatalf("daily submit: %v", err)
	}

	rows, err := client.TopMode(ctx, 10, pc.DeviceID, "daily", day)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rows {
		if r.Name == "LIVEDAY" && r.Score == 32100 {
			found = r.Mine
		}
	}
	if !found {
		t.Fatalf("LIVEDAY missing from the daily board (or not mine): %+v", rows)
	}

	classic, err := client.Top(ctx, 50, pc.DeviceID)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range classic {
		if r.Name == "LIVEDAY" && r.Score == 32100 {
			t.Fatal("daily row leaked into the classic board")
		}
	}
	t.Logf("LIVEDAY on the %s daily board, not on classic", day)
}
