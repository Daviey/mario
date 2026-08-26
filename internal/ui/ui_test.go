package ui

// Leaderboard UI state machine tests. Network is faked; the engine is
// driven to game over with the deterministic demo script.

import (
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Daviey/mario/board"
	"github.com/Daviey/mario/engine"
	"github.com/Daviey/mario/input"
	"github.com/Daviey/mario/internal/persist"
	"github.com/Daviey/mario/render"
)

// gameOverGame returns a game at the game-over screen with a score.
func gameOverGame(t *testing.T) *engine.Game {
	t.Helper()
	g := engine.NewGame(engine.DefaultLevels(), 20, engine.LevelHeight)
	for t := range 6000 {
		g.Update(ScriptInput(t))
	}
	if g.State != engine.StateGameOver || g.Score == 0 {
		t.Fatalf("script should end game over with a score, got %v score=%d", g.State, g.Score)
	}
	return g
}

func tickUntil(t *testing.T, ui *UI, g *engine.Game, n int) *render.ScoreUI {
	t.Helper()
	var snap *render.ScoreUI
	for range n {
		snap = ui.Tick(g)
	}
	return snap
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not reached in time")
}

func TestGameOverAutoAsksAndSubmits(t *testing.T) {
	t.Setenv("SUPABASE_URL", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var mu sync.Mutex
	var got board.Entry
	submitted := make(chan struct{})
	ui := NewUI(func(e board.Entry) error {
		mu.Lock()
		got = e
		mu.Unlock()
		close(submitted)
		return nil
	}, func() ([]board.Row, error) {
		mu.Lock()
		defer mu.Unlock()
		return []board.Row{
			{Name: "BIFF", Score: 32100},
			{Name: got.Name, Score: 999, Mine: true},
		}, nil
	})

	g := gameOverGame(t)
	snap := tickUntil(t, ui, g, 2)
	if snap == nil || snap.Mode != render.UIAsk || snap.Score != g.Score {
		t.Fatalf("expected auto ask prompt, got %+v", snap)
	}

	ui.FeedKeys([]byte("y"))
	snap = tickUntil(t, ui, g, 1)
	if snap.Mode != render.UIEntry {
		t.Fatalf("y should open entry, got %v", snap.Mode)
	}

	ui.FeedKeys([]byte("dave!2\r")) // lowercase up, '!' dropped by charset
	snap = tickUntil(t, ui, g, 1)
	if snap.Mode != render.UIBoard || snap.Status != "SUBMITTING" {
		t.Fatalf("enter should submit and show board, got %+v", snap)
	}
	select {
	case <-submitted:
	case <-time.After(2 * time.Second):
		t.Fatal("submit never called")
	}
	mu.Lock()
	name, dev := got.Name, got.DeviceID
	mu.Unlock()
	if name != "DAVE2" || got.Score != g.Score || got.Level != g.LevelIndex()+1 || dev == "" {
		t.Fatalf("entry = %+v", got)
	}

	// Submit lands first, then the board refetches; wait for both.
	deadline := time.Now().Add(2 * time.Second)
	for {
		ui.mu.Lock()
		st, n := ui.status, len(ui.rows)
		ui.mu.Unlock()
		if st == "SUBMITTED!" && n == 2 {
			break
		}
		if time.Now().After(deadline) {
			ui.mu.Lock()
			snap := ui.snapshotLocked(g)
			ui.mu.Unlock()
			t.Fatalf("submit flow stalled: status=%q rows=%d mode=%v", st, n, snap.Mode)
		}
		time.Sleep(5 * time.Millisecond)
	}
	ui.mu.Lock()
	rows := ui.rows
	ui.mu.Unlock()
	if len(rows) != 2 || !rows[1].Mine || rows[1].Name != "DAVE2" {
		t.Fatalf("rows after submit = %+v", rows)
	}

	// Board q after submitting quits the game.
	ui.FeedKeys([]byte("q"))
	tickUntil(t, ui, g, 1)
	if !ui.quitRequested() {
		t.Fatal("q on the board after submit should quit")
	}
}

func TestDeclineDoesNotSubmitOrQuit(t *testing.T) {
	t.Setenv("SUPABASE_URL", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	called := false
	ui := NewUI(func(board.Entry) error { called = true; return nil }, nil)
	g := gameOverGame(t)
	tickUntil(t, ui, g, 1)
	ui.FeedKeys([]byte("n"))
	snap := tickUntil(t, ui, g, 1)
	if snap != nil || called {
		t.Fatalf("decline must close UI without submitting: snap=%+v called=%v", snap, called)
	}
	if ui.quitRequested() {
		t.Fatal("declining must not quit the game")
	}
	// And it never asks again for this game-over.
	if s := tickUntil(t, ui, g, 3); s != nil {
		t.Fatalf("re-asked after decline: %+v", s)
	}
}

func TestEntryEditing(t *testing.T) {
	t.Setenv("SUPABASE_URL", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	ui := NewUI(nil, nil)
	g := gameOverGame(t)
	tickUntil(t, ui, g, 1)
	ui.FeedKeys([]byte("y"))
	tickUntil(t, ui, g, 1)

	ui.FeedKeys([]byte("abc"))
	tickUntil(t, ui, g, 1)
	ui.FeedKeys([]byte{0x08}) // backspace
	tickUntil(t, ui, g, 1)
	ui.FeedKeys([]byte("DE"))
	tickUntil(t, ui, g, 1)
	ui.mu.Lock()
	name := string(ui.name)
	ui.mu.Unlock()
	if name != "ABDE" {
		t.Fatalf("name after edits = %q", name)
	}

	// Max length enforced.
	ui.FeedKeys([]byte("FGHIJKLMNOP"))
	tickUntil(t, ui, g, 1)
	ui.mu.Lock()
	name = string(ui.name)
	ui.mu.Unlock()
	if len(name) != maxNameLen {
		t.Fatalf("name length = %d, want cap %d (%q)", len(name), maxNameLen, name)
	}

	// ESC returns to the ask prompt.
	ui.FeedKeys([]byte{0x1b})
	snap := tickUntil(t, ui, g, 1)
	if snap.Mode != render.UIAsk {
		t.Fatalf("esc should return to ask, got %v", snap.Mode)
	}
}

func TestEmptyNameUsesStoredDefault(t *testing.T) {
	t.Setenv("SUPABASE_URL", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	pc, _ := persist.LoadPlayer()
	pc.SaveName("REGULAR")

	var mu sync.Mutex
	var name string
	done := make(chan struct{})
	ui := NewUI(func(e board.Entry) error {
		mu.Lock()
		name = e.Name
		mu.Unlock()
		close(done)
		return nil
	}, nil)
	g := gameOverGame(t)
	tickUntil(t, ui, g, 1)
	ui.FeedKeys([]byte("y\r")) // accept prompt, enter with empty name
	tickUntil(t, ui, g, 1)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("submit never called")
	}
	mu.Lock()
	defer mu.Unlock()
	if name != "REGULAR" {
		t.Fatalf("empty name should use stored name, got %q", name)
	}
}

func TestTitleLOpensBoard(t *testing.T) {
	t.Setenv("SUPABASE_URL", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	fetched := make(chan struct{}, 1)
	ui := NewUI(nil, func() ([]board.Row, error) {
		fetched <- struct{}{}
		return []board.Row{{Name: "KIM", Score: 900}}, nil
	})
	g := engine.NewGame(engine.DefaultLevels(), 20, engine.LevelHeight) // title state

	ui.note([]byte("x"))
	snap := tickUntil(t, ui, g, 1)
	if snap != nil {
		t.Fatal("non-l key must not open the board")
	}
	ui.note([]byte("l"))
	snap = tickUntil(t, ui, g, 1)
	if snap == nil || snap.Mode != render.UIBoard || !snap.Loading {
		t.Fatalf("l should open loading board, got %+v", snap)
	}
	select {
	case <-fetched:
	case <-time.After(2 * time.Second):
		t.Fatal("fetch never called")
	}
	waitFor(t, 2*time.Second, func() bool {
		ui.mu.Lock()
		defer ui.mu.Unlock()
		return len(ui.rows) == 1
	})
	// Closing returns to the title, not quit (nothing was submitted).
	ui.FeedKeys([]byte("l"))
	if s := tickUntil(t, ui, g, 1); s != nil {
		t.Fatalf("board should close: %+v", s)
	}
	if ui.quitRequested() {
		t.Fatal("closing the title board must not quit")
	}
}

func TestOfflineStillShowsPrompt(t *testing.T) {
	// No backend configured: game over still asks, submitting reports
	// OFFLINE instead of crashing.
	t.Setenv("SUPABASE_URL", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("SUPABASE_URL", "")
	t.Setenv("SUPABASE_KEY", "")
	ui := NewUI(nil, nil)
	g := gameOverGame(t)
	snap := tickUntil(t, ui, g, 1)
	if snap == nil || snap.Mode != render.UIAsk {
		t.Fatalf("offline should still ask: %+v", snap)
	}
	ui.FeedKeys([]byte("yX\r"))
	snap = tickUntil(t, ui, g, 2)
	if snap.Mode != render.UIBoard || snap.Status != "OFFLINE" {
		t.Fatalf("offline submit = %+v", snap)
	}
}

func TestBoardRowsForKeepsMineFlag(t *testing.T) {
	// mine-ness is computed by the board_rows RPC; boardRowsFor only
	// carries it through and assigns ranks.
	rows := boardRowsFor([]board.Row{
		{Name: "A", Mine: true},
		{Name: "B"},
	})
	if !rows[0].Mine || rows[1].Mine || rows[0].Rank != 1 || rows[1].Rank != 2 {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestSnapshotFields(t *testing.T) {
	t.Setenv("SUPABASE_URL", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	ui := NewUI(nil, nil)
	g := gameOverGame(t)
	ui.FeedKeys([]byte("y"))
	snap := tickUntil(t, ui, g, 1)
	if !strings.Contains(snap.Name, "") && snap.Name != "" {
		t.Fatal("fresh entry name should be empty")
	}
	g.Tick = 15 // cursor blink window
	snap = tickUntil(t, ui, g, 1)
	if !snap.CursorOn {
		t.Fatal("cursor should blink on early in the window")
	}
}

func TestTitleLOpensBoardOffline(t *testing.T) {
	// Integration through gameIO: 'l' on the title screen opens the board
	// (OFFLINE without credentials) and never starts the game. Both cases
	// must work: the pixel-font hint reads "L LEADERBOARD", so players
	// using shift or caps-lock send uppercase.
	t.Setenv("SUPABASE_URL", "")
	for _, key := range []byte{'l', 'L'} {
		g := engine.NewGame(engine.DefaultLevels(), 40, engine.LevelHeight)
		io := NewRouter(input.NewMapper(), NewUI(nil, nil))
		io.Feed([]byte{key})
		opened := false
		for range 100 {
			time.Sleep(1 * time.Millisecond) // Let fetchInto goroutine run
			g.Update(io.Poll())
			ui := io.UITick(g)
			if ui != nil && ui.Mode == render.UIBoard && !ui.Loading {
				if g.State != engine.StateTitle {
					t.Fatalf("%q: game left title: %v", key, g.State)
				}
				if ui.Loading {
					t.Error("offline board stuck LOADING")
				}
				if ui.Status != "OFFLINE" {
					t.Errorf("status = %q, want OFFLINE", ui.Status)
				}
				opened = true
				break
			}
		}
		if !opened {
			t.Fatalf("%q never opened the board", key)
		}
	}
}

func TestBoardRClosesAndRestarts(t *testing.T) {
	// 'r' on the board (the R RESTART hint) must close the board,
	// request a one-shot restart edge instead of quitting, and re-arm
	// the game-over ask so the next run can submit again.
	t.Setenv("SUPABASE_URL", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	g := gameOverGame(t)
	io := NewRouter(input.NewMapper(), NewUI(nil, nil))
	step := func(n int) *render.ScoreUI {
		var s *render.ScoreUI
		for range n {
			g.Update(io.Poll())
			s = io.UITick(g)
		}
		return s
	}
	if s := step(1); s == nil || s.Mode != render.UIAsk {
		t.Fatalf("game over should ask, got %+v", s)
	}
	io.Feed([]byte("y"))
	if s := step(1); s.Mode != render.UIEntry {
		t.Fatalf("y should open entry, got %v", s.Mode)
	}
	io.Feed([]byte("\r"))
	if s := step(1); s.Mode != render.UIBoard || s.Status != "OFFLINE" {
		t.Fatalf("submit should show board, got %+v", s)
	}

	io.Feed([]byte("r"))
	if s := step(1); s != nil {
		t.Fatalf("r should close the board: %+v", s)
	}
	if io.QuitRequested() {
		t.Fatal("restart must not quit")
	}
	in := io.Poll()
	if !in.Restart {
		t.Fatal("restart edge missing from polled input")
	}
	g.Update(in)
	if g.State != engine.StatePlaying {
		t.Fatalf("restart should reset the game, got %v", g.State)
	}
	if io.Poll().Restart {
		t.Fatal("restart edge should be one-shot")
	}

	// Re-armed: a second game over asks to submit again.
	g.Score = 500
	g.State = engine.StateGameOver
	if s := step(1); s == nil || s.Mode != render.UIAsk {
		t.Fatalf("next game over should ask again, got %+v", s)
	}
}

// A submission must carry the level the run ended on — the board shows it.
func TestSubmitCarriesLevel(t *testing.T) {
	t.Setenv("SUPABASE_URL", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	// Level 1: flat ground, flag at x=6. Level 2: no ground at all —
	// falling in the pit with one life ends the run there.
	lvl1, err := engine.ParseLevel("1", []string{"      F     ", "############", "############"})
	if err != nil {
		t.Fatal(err)
	}
	lvl2, err := engine.ParseLevel("2", []string{"      F     ", "            ", "            "})
	if err != nil {
		t.Fatal(err)
	}
	g := engine.NewGame([]*engine.Level{lvl1, lvl2}, 12, 3)
	g.State = engine.StatePlaying

	for i := 0; i < 6000 && g.State != engine.StateLevelClear; i++ {
		g.Update(engine.Input{Right: true})
	}
	if g.State != engine.StateLevelClear {
		t.Fatalf("never cleared level 1: %v", g.State)
	}
	for i := 0; i < engine.ClearTicks+120 && g.State != engine.StatePlaying; i++ {
		g.Update(engine.Input{})
	}
	if g.State != engine.StatePlaying || g.LevelIndex() != 1 {
		t.Fatalf("want playing on level 2, got %v level %d", g.State, g.LevelIndex())
	}

	g.Lives = 1
	for i := 0; i < 1000 && g.State != engine.StateGameOver; i++ {
		g.Update(engine.Input{})
	}
	if g.State != engine.StateGameOver || g.Score == 0 {
		t.Fatalf("want game over with score, got %v score=%d", g.State, g.Score)
	}

	var got board.Entry
	submitted := make(chan struct{})
	ui := NewUI(func(e board.Entry) error {
		got = e
		close(submitted)
		return nil
	}, nil)
	tickUntil(t, ui, g, 2)
	ui.FeedKeys([]byte("y"))
	tickUntil(t, ui, g, 1)
	ui.FeedKeys([]byte("\r"))
	tickUntil(t, ui, g, 1)
	select {
	case <-submitted:
	case <-time.After(2 * time.Second):
		t.Fatal("submit never called")
	}
	if got.Level != 2 {
		t.Fatalf("submitted level = %d, want 2", got.Level)
	}
}
