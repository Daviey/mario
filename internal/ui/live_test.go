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

	"mario/board"
	"mario/engine"
	"mario/render"
)

func TestLiveUISubmit(t *testing.T) {
	if os.Getenv("LIVE") != "1" {
		t.Skip("set LIVE=1 to hit the real backend")
	}
	board.LoadDotEnv(".env")
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
