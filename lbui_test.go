package main

// Leaderboard UI state machine tests. Network is faked; the engine is
// driven to game over with the deterministic demo script.

import (
	"strings"
	"sync"
	"testing"
	"time"

	"mario/board"
	"mario/engine"
	"mario/input"
	"mario/render"
)

// gameOverGame returns a game at the game-over screen with a score.
func gameOverGame(t *testing.T) *engine.Game {
	t.Helper()
	g := engine.NewGame(engine.DefaultLevels(), 20, engine.LevelHeight)
	for t := range 6000 {
		g.Update(scriptInput(t))
	}
	if g.State != engine.StateGameOver || g.Score == 0 {
		t.Fatalf("script should end game over with a score, got %v score=%d", g.State, g.Score)
	}
	return g
}

func tickUntil(t *testing.T, ui *scoreUI, g *engine.Game, n int) *render.ScoreUI {
	t.Helper()
	var snap *render.ScoreUI
	for range n {
		snap = ui.tick(g)
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
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var mu sync.Mutex
	var got board.Entry
	submitted := make(chan struct{})
	ui := newScoreUI(func(e board.Entry) error {
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
			{Name: got.Name, Score: 999, DeviceID: got.DeviceID},
		}, nil
	})

	g := gameOverGame(t)
	snap := tickUntil(t, ui, g, 2)
	if snap == nil || snap.Mode != render.UIAsk || snap.Score != g.Score {
		t.Fatalf("expected auto ask prompt, got %+v", snap)
	}

	ui.feedKeys([]byte("y"))
	snap = tickUntil(t, ui, g, 1)
	if snap.Mode != render.UIEntry {
		t.Fatalf("y should open entry, got %v", snap.Mode)
	}

	ui.feedKeys([]byte("dave!2\r")) // lowercase up, '!' dropped by charset
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
	if name != "DAVE2" || got.Score != g.Score || dev == "" {
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
	ui.feedKeys([]byte("q"))
	tickUntil(t, ui, g, 1)
	if !ui.quitRequested() {
		t.Fatal("q on the board after submit should quit")
	}
}

func TestDeclineDoesNotSubmitOrQuit(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	called := false
	ui := newScoreUI(func(board.Entry) error { called = true; return nil }, nil)
	g := gameOverGame(t)
	tickUntil(t, ui, g, 1)
	ui.feedKeys([]byte("n"))
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
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	ui := newScoreUI(nil, nil)
	g := gameOverGame(t)
	tickUntil(t, ui, g, 1)
	ui.feedKeys([]byte("y"))
	tickUntil(t, ui, g, 1)

	ui.feedKeys([]byte("abc"))
	tickUntil(t, ui, g, 1)
	ui.feedKeys([]byte{0x08}) // backspace
	tickUntil(t, ui, g, 1)
	ui.feedKeys([]byte("DE"))
	tickUntil(t, ui, g, 1)
	ui.mu.Lock()
	name := string(ui.name)
	ui.mu.Unlock()
	if name != "ABDE" {
		t.Fatalf("name after edits = %q", name)
	}

	// Max length enforced.
	ui.feedKeys([]byte("FGHIJKLMNOP"))
	tickUntil(t, ui, g, 1)
	ui.mu.Lock()
	name = string(ui.name)
	ui.mu.Unlock()
	if len(name) != maxNameLen {
		t.Fatalf("name length = %d, want cap %d (%q)", len(name), maxNameLen, name)
	}

	// ESC returns to the ask prompt.
	ui.feedKeys([]byte{0x1b})
	snap := tickUntil(t, ui, g, 1)
	if snap.Mode != render.UIAsk {
		t.Fatalf("esc should return to ask, got %v", snap.Mode)
	}
}

func TestEmptyNameUsesStoredDefault(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	pc, _ := loadPlayer()
	pc.saveName("REGULAR")

	var mu sync.Mutex
	var name string
	done := make(chan struct{})
	ui := newScoreUI(func(e board.Entry) error {
		mu.Lock()
		name = e.Name
		mu.Unlock()
		close(done)
		return nil
	}, nil)
	g := gameOverGame(t)
	tickUntil(t, ui, g, 1)
	ui.feedKeys([]byte("y\r")) // accept prompt, enter with empty name
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
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	fetched := make(chan struct{}, 1)
	ui := newScoreUI(nil, func() ([]board.Row, error) {
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
	ui.feedKeys([]byte("l"))
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
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("SUPABASE_URL", "")
	t.Setenv("SUPABASE_KEY", "")
	ui := newScoreUI(nil, nil)
	g := gameOverGame(t)
	snap := tickUntil(t, ui, g, 1)
	if snap == nil || snap.Mode != render.UIAsk {
		t.Fatalf("offline should still ask: %+v", snap)
	}
	ui.feedKeys([]byte("yX\r"))
	snap = tickUntil(t, ui, g, 2)
	if snap.Mode != render.UIBoard || snap.Status != "OFFLINE" {
		t.Fatalf("offline submit = %+v", snap)
	}
}

func TestBoardRowsForMarksMine(t *testing.T) {
	rows := boardRowsFor([]board.Row{
		{Name: "A", DeviceID: "me"},
		{Name: "B", DeviceID: "other"},
		{Name: "C"}, // API may omit device_id
	}, "me")
	want := []bool{true, false, false}
	for i, w := range want {
		if rows[i].Mine != w || rows[i].Rank != i+1 {
			t.Fatalf("row %d = %+v, want Mine=%v Rank=%d", i, rows[i], w, i+1)
		}
	}
}

func TestSnapshotFields(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	ui := newScoreUI(nil, nil)
	g := gameOverGame(t)
	ui.feedKeys([]byte("y"))
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
	// (OFFLINE without credentials) and never starts the game.
	g := engine.NewGame(engine.DefaultLevels(), 40, engine.LevelHeight)
	io := newGameIO(input.NewMapper(), newScoreUI(nil, nil))
	io.feed([]byte("l"))
	for range 3 {
		g.Update(io.poll())
		ui := io.uiTick(g)
		if ui != nil && ui.Mode == render.UIBoard {
			if g.State != engine.StateTitle {
				t.Fatalf("game left title: %v", g.State)
			}
			if ui.Loading {
				t.Error("offline board stuck LOADING")
			}
			if ui.Status != "OFFLINE" {
				t.Errorf("status = %q, want OFFLINE", ui.Status)
			}
			return
		}
	}
	t.Fatal("'l' never opened the board")
}
