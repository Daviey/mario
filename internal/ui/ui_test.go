package ui

// Leaderboard UI state machine tests. Network is faked; the engine is
// driven to game over with the deterministic demo script.

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Daviey/mario/board"
	"github.com/Daviey/mario/engine"
	"github.com/Daviey/mario/input"
	"github.com/Daviey/mario/internal/persist"
	"github.com/Daviey/mario/render"
)

// testReplaySource stands in for the App's recorder in submit-flow tests.
func testReplaySource() (string, bool) { return `{"v":1,"ticks":2,"runs":[[0,2]]}`, true }

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
	ui.SetReplaySource(testReplaySource)

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
	ui.SetReplaySource(testReplaySource)
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

// The regression: after a daily run's game over, u.daily stays true; the
// title-'l' path inlined the board-open logic without resetting it, so
// the board shown at the title was the DAILY one (and the day stale).
func TestTitleLAfterDailyRunShowsClassicBoard(t *testing.T) {
	t.Setenv("SUPABASE_URL", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var mu sync.Mutex
	var modes []bool
	var ui *UI
	ui = NewUI(nil, func() ([]board.Row, error) {
		// Capture the mode the real default fetch would branch on.
		mu.Lock()
		modes = append(modes, ui.dailyMode())
		mu.Unlock()
		return []board.Row{{Name: "KIM", Score: 900}}, nil
	})

	g := gameOverGame(t)
	g.Daily = true // the run was a daily challenge
	if s := tickUntil(t, ui, g, 2); s == nil || s.Mode != render.UIAsk {
		t.Fatalf("daily game over should ask, got %+v", s)
	}
	ui.FeedKeys([]byte("n")) // decline: done, back to UIOff
	tickUntil(t, ui, g, 1)

	// Restart: the player is back at the title and opens the board.
	title := engine.NewGame(engine.DefaultLevels(), 20, engine.LevelHeight)
	ui.note([]byte("l"))
	if s := tickUntil(t, ui, title, 1); s == nil || s.Mode != render.UIBoard || !s.Loading {
		t.Fatalf("title l should open the classic board, got %+v", s)
	}
	waitFor(t, 2*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(modes) > 0
	})
	mu.Lock()
	defer mu.Unlock()
	if modes[0] {
		t.Fatalf("title board after a daily run fetched the daily board (modes=%v)", modes)
	}
}

func TestFetchRetriesTransientError(t *testing.T) {
	// One transient fetch failure must not read as OFFLINE: the board
	// retries once (observed live — a fetch in a blue moon hangs to the
	// 15s timeout against the Supabase/Cloudflare edge).
	t.Setenv("SUPABASE_URL", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	calls := 0
	ui := NewUI(nil, func() ([]board.Row, error) {
		calls++
		if calls == 1 {
			return nil, errors.New("transient edge hiccup")
		}
		return []board.Row{{Name: "KIM", Score: 900}}, nil
	})
	g := engine.NewGame(engine.DefaultLevels(), 20, engine.LevelHeight)
	ui.note([]byte("l"))
	tickUntil(t, ui, g, 1)
	waitFor(t, 2*time.Second, func() bool {
		ui.mu.Lock()
		defer ui.mu.Unlock()
		return len(ui.rows) == 1
	})
	if calls != 2 {
		t.Fatalf("fetch called %d times, want 2 (one retry)", calls)
	}
	ui.mu.Lock()
	status := ui.status
	ui.mu.Unlock()
	if status == "OFFLINE" {
		t.Fatal("transient failure surfaced as OFFLINE")
	}
}

// A status the server chose deliberately (400/429/PoW/RLS) fails
// identically again: one call, no retry.
func TestFetchDoesNotRetryHTTPStatus(t *testing.T) {
	t.Setenv("SUPABASE_URL", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var calls atomic.Int32
	ui := NewUI(nil, func() ([]board.Row, error) {
		calls.Add(1)
		return nil, &board.HTTPError{Method: "GET", Path: "/rest/v1/board_rows", Status: 400, StatusText: "400 Bad Request", Body: "nope"}
	})
	g := engine.NewGame(engine.DefaultLevels(), 20, engine.LevelHeight)
	ui.note([]byte("l"))
	tickUntil(t, ui, g, 1)
	waitFor(t, 2*time.Second, func() bool {
		ui.mu.Lock()
		defer ui.mu.Unlock()
		return ui.status == "OFFLINE"
	})
	if n := calls.Load(); n != 1 {
		t.Fatalf("fetch called %d times, want 1 (4xx is not retried)", n)
	}
}

// If the board closed under the fetch (player hit q during the retry
// window), the second attempt is pointless: it would land on a screen
// that no longer exists. No second call.
func TestFetchDoesNotRetryAfterBoardClosed(t *testing.T) {
	t.Setenv("SUPABASE_URL", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var calls atomic.Int32
	first := make(chan struct{})
	release := make(chan struct{})
	ui := NewUI(nil, func() ([]board.Row, error) {
		if calls.Add(1) == 1 {
			close(first)
			<-release // hold the first call while the board closes
			return nil, errors.New("edge hiccup")
		}
		return []board.Row{{Name: "KIM", Score: 900}}, nil
	})
	g := engine.NewGame(engine.DefaultLevels(), 20, engine.LevelHeight)
	ui.note([]byte("l"))
	tickUntil(t, ui, g, 1) // board open, first fetch in flight
	<-first
	ui.FeedKeys([]byte("q"))
	if s := tickUntil(t, ui, g, 1); s != nil {
		t.Fatalf("board should be closed: %+v", s)
	}
	close(release) // first call fails transiently — now
	waitFor(t, 2*time.Second, func() bool {
		ui.mu.Lock()
		defer ui.mu.Unlock()
		return ui.status == "OFFLINE"
	})
	time.Sleep(50 * time.Millisecond) // a wrongful retry would land here
	if n := calls.Load(); n != 1 {
		t.Fatalf("fetch called %d times after close, want 1", n)
	}
}

func TestSubmitCarriesPlayContext(t *testing.T) {
	t.Setenv("SUPABASE_URL", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	var mu sync.Mutex
	var got board.Entry
	ui := NewUI(func(e board.Entry) error {
		mu.Lock()
		got = e
		mu.Unlock()
		return nil
	}, nil)
	ui.SetReplaySource(func() (string, bool) { return `{"v":1,"ticks":2,"runs":[[0,2]]}`, true })
	ui.SetPlayContext(func() board.Entry {
		return board.Entry{Surface: "ssh", Term: "xterm-256color", ColorTerm: "truecolor",
			InputRegime: "legacy", Viewport: "60x14"}
	})
	g := gameOverGame(t)
	tickUntil(t, ui, g, 1) // ask
	ui.FeedKeys([]byte("y"))
	tickUntil(t, ui, g, 1) // entry
	ui.FeedKeys([]byte("\r"))
	tickUntil(t, ui, g, 1) // submit
	waitFor(t, 2*time.Second, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return got.Replay != ""
	})
	if got.Surface != "ssh" || got.Term != "xterm-256color" || got.ColorTerm != "truecolor" ||
		got.InputRegime != "legacy" || got.Viewport != "60x14" {
		t.Fatalf("play context missing from submission: %+v", got)
	}
}

// The played→submitted funnel flag: Submitted() turns true only after a
// submission actually lands, and stays false when the backend errors.
func TestSubmittedFlag(t *testing.T) {
	t.Setenv("SUPABASE_URL", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	ok := NewUI(func(board.Entry) error { return nil }, nil)
	ok.SetReplaySource(func() (string, bool) { return `{"v":1,"ticks":2,"runs":[[0,2]]}`, true })
	g := gameOverGame(t)
	tickUntil(t, ok, g, 1)
	ok.FeedKeys([]byte("y"))
	tickUntil(t, ok, g, 1)
	ok.FeedKeys([]byte("\r"))
	tickUntil(t, ok, g, 1)
	waitFor(t, 2*time.Second, ok.Submitted)
	if !ok.Submitted() {
		t.Fatal("successful submit must set Submitted")
	}

	fail := NewUI(func(board.Entry) error { return errors.New("down") }, nil)
	fail.SetReplaySource(func() (string, bool) { return `{"v":1,"ticks":2,"runs":[[0,2]]}`, true })
	g2 := gameOverGame(t)
	tickUntil(t, fail, g2, 1)
	fail.FeedKeys([]byte("y"))
	tickUntil(t, fail, g2, 1)
	fail.FeedKeys([]byte("\r"))
	tickUntil(t, fail, g2, 1)
	waitFor(t, 2*time.Second, func() bool {
		fail.mu.Lock()
		defer fail.mu.Unlock()
		return fail.status == "SUBMIT FAILED"
	})
	if fail.Submitted() {
		t.Fatal("failed submit must not set Submitted")
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
	if g.State != engine.StateWorldCard {
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

	// Through the flag sequence: slide, castle walk, score countdown.
	for i := 0; i < 6000 && g.State == engine.StatePlaying; i++ {
		g.Update(engine.Input{Right: true})
	}
	for i := 0; i < 1500 && g.State != engine.StateWorldCard; i++ {
		g.Update(engine.Input{})
	}
	if g.State != engine.StateWorldCard {
		t.Fatalf("never cleared level 1: %v", g.State)
	}
	for i := 0; i < engine.WorldCardTicks+10 && g.State != engine.StatePlaying; i++ {
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
	ui.SetReplaySource(testReplaySource)
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
