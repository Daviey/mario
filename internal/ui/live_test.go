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
	"github.com/Daviey/mario/replay"
)

func TestLiveUISubmit(t *testing.T) {
	if os.Getenv("LIVE") != "1" {
		t.Skip("set LIVE=1 to hit the real backend")
	}
	board.LoadDotEnv("../../.env", "../.env", ".env")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // fresh player identity

	g := engine.NewGame(engine.DefaultLevels(), 20, engine.LevelHeight)
	var rec replay.Recorder
	for i := range 6000 {
		in := engine.Input{Right: true, Run: i%3 != 0, Up: i%97 < 22, AnyKey: i == 0}
		g.Update(in)
		if i >= 1 { // tick 0 dismissed the title; the recording starts at the card
			if i == 1 {
				rec.Start()
			}
			rec.Record(in)
		}
	}
	if g.State != engine.StateGameOver || g.Score == 0 {
		t.Fatalf("script should end game over with a score, got %v score=%d", g.State, g.Score)
	}
	rec.Finish()
	if res, err := replay.Run(engine.DefaultLevels(), "classic", rec.JSON()); err != nil || res.Score != g.Score {
		t.Fatalf("replay must reproduce the live run: %v %+v vs %d", err, res, g.Score)
	}

	ui := NewUI(nil, nil) // real board client from env
	ui.SetReplaySource(func() (string, bool) { return rec.JSON(), true })
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
	y, m, d := time.Now().UTC().Date()
	levels := []*engine.Level{engine.DailyLevelFor(y, int(m), d)}
	g := engine.NewGame(levels, 20, engine.LevelHeight)
	g.Daily = true
	g.BeginDaily()
	var rec replay.Recorder
	rec.Start()
	for t := 0; g.State != engine.StateGameOver && g.State != engine.StateWin && t < 6000; t++ {
		in := engine.Input{Right: true, Run: t%3 != 0, Up: t%97 < 22}
		g.Update(in)
		rec.Record(in)
	}
	rec.Finish()
	if res, err := replay.Run(levels, "daily", rec.JSON()); err != nil || res.Score != g.Score {
		t.Fatalf("daily replay must reproduce the run: %v %+v vs %d", err, res, g.Score)
	}
	if err := client.Submit(ctx, board.Entry{
		Name: "LIVEDAY", Score: g.Score, Level: g.LevelIndex() + 1, DeviceID: pc.DeviceID, Mode: "daily", Day: day,
		Replay: rec.JSON(),
	}); err != nil {
		t.Fatalf("daily submit: %v", err)
	}

	rows, err := client.TopMode(ctx, 10, pc.DeviceID, "daily", day)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rows {
		if r.Name == "LIVEDAY" && r.Score == g.Score {
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
		if r.Name == "LIVEDAY" && r.Score == g.Score {
			t.Fatal("daily row leaked into the classic board")
		}
	}
	t.Logf("LIVEDAY on the %s daily board, not on classic", day)
}

// TestLiveDailyGameOverBoard drives the real UI machine through a daily
// game over: submit, then the board that the DEFAULT fetch shows must be
// the daily one (the fetch closure branches via dailyMode), with the row
// present and the rank computed. LIVE only; delete LIVEDAY2 afterwards.
func TestLiveDailyGameOverBoard(t *testing.T) {
	if os.Getenv("LIVE") != "1" {
		t.Skip("set LIVE=1 to hit the real backend")
	}
	board.LoadDotEnv("../../.env", "../.env", ".env")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	lv := engine.DailyLevelFor(2026, 8, 26)
	g := engine.NewGame([]*engine.Level{lv}, 20, engine.LevelHeight)
	g.Daily = true
	g.State = engine.StatePlaying
	g.Score = 55500 // straight to game over below
	g.State = engine.StateGameOver

	ui := NewUI(nil, nil)
	if ui.submit == nil {
		t.Fatal("no leaderboard configured")
	}
	snap := ui.Tick(g)
	if snap == nil || snap.Mode != render.UIAsk || !snap.Daily {
		t.Fatalf("ask for a daily run: %+v", snap)
	}
	ui.FeedKeys([]byte("yLIVEDAY2\r"))
	ui.Tick(g)

	deadline := time.Now().Add(15 * time.Second)
	var board *render.ScoreUI
	for time.Now().Before(deadline) {
		board = ui.Tick(g)
		if board != nil && board.Mode == render.UIBoard && !board.Loading {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if board == nil || board.Mode != render.UIBoard {
		t.Fatal("board never finished loading")
	}
	if !board.Daily {
		t.Fatalf("board after a daily submit is not daily: %+v", board)
	}
	found := false
	for _, r := range board.Rows {
		if r.Name == "LIVEDAY2" && r.Score == 55500 {
			found = true
		}
	}
	if !found {
		t.Fatalf("LIVEDAY2 missing from the UI-fetched daily board: %+v", board.Rows)
	}
	t.Logf("daily board via UI fetch ok: rank=%d rows=%d", board.Rank, len(board.Rows))
}
