package mario

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/Daviey/mario/render"

	"github.com/Daviey/mario/engine"
	"github.com/Daviey/mario/replay"
)

func TestRunDemo(t *testing.T) {
	var buf bytes.Buffer
	RunDemo(&buf, engine.DefaultLevels(), render.Colors24, 6000)
	out := buf.String()
	if !strings.Contains(out, "demo: ticks=6000") {
		t.Errorf("demo summary missing: %q", out[:min(80, len(out))])
	}
	if !strings.Contains(out, "score=") || !strings.Contains(out, "state=") {
		t.Error("demo summary incomplete")
	}
	if !strings.Contains(out, "\x1b[H") {
		t.Error("demo should render a final ANSI frame")
	}
	// Deterministic: same input script, same outcome.
	var buf2 bytes.Buffer
	RunDemo(&buf2, engine.DefaultLevels(), render.Colors24, 6000)
	if buf.String() != buf2.String() {
		t.Error("demo output is not deterministic")
	}
}

func TestResizeAppliesOnNextStep(t *testing.T) {
	app := New(nil)
	if app.Game.ViewW != 40 || app.Game.ViewH != engine.LevelHeight {
		t.Fatalf("default viewport = %dx%d", app.Game.ViewW, app.Game.ViewH)
	}
	app.Resize(20, 8)
	if app.Game.ViewW != 40 {
		t.Fatal("resize must not land before the next Step")
	}
	app.Step()
	if app.Game.ViewW != 20 || app.Game.ViewH != 8 {
		t.Fatalf("viewport after Step = %dx%d, want 20x8", app.Game.ViewW, app.Game.ViewH)
	}

	// Same policy as New: clamped, never fatal.
	app.Resize(5, 2)
	app.Step()
	if app.Game.ViewW != 16 || app.Game.ViewH != 4 {
		t.Fatalf("clamped viewport = %dx%d, want 16x4", app.Game.ViewW, app.Game.ViewH)
	}
	// High-end clamps land too: width tops out at 60, height at the
	// level's own height (SetViewport's ceiling — a viewport taller than
	// the level has nothing to show).
	app.Resize(61, 999)
	app.Step()
	if app.Game.ViewW != 60 || app.Game.ViewH != engine.LevelHeight {
		t.Fatalf("oversize resize = %dx%d, want 60x%d", app.Game.ViewW, app.Game.ViewH, engine.LevelHeight)
	}
}

// The race-detector tripwire for Resize's cross-goroutine path: a
// signal goroutine hammering Resize (as SIGWINCH would) while the tick
// goroutine Steps. Run with -race; also passes sequentially.
func TestResizeConcurrentWithStep(t *testing.T) {
	app := New(nil)
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-done:
				return
			default:
			}
			app.Resize(16+i%45, 4+i%10)
		}
	}()
	for range 300 {
		app.Step()
	}
	close(done)
	wg.Wait()

	app.Step() // apply anything requested in the final instants
	if app.Game.ViewW < 16 || app.Game.ViewW > 60 || app.Game.ViewH < 4 {
		t.Fatalf("viewport after concurrent resizes = %dx%d", app.Game.ViewW, app.Game.ViewH)
	}
}

func TestSuicideKeyEndToEnd(t *testing.T) {
	// The full terminal path: raw stdin bytes → router → mapper →
	// engine death. 'k' must kill a live run.
	a := New(nil)
	a.Feed([]byte("\r")) // AnyKey: title → world card
	for range 600 {
		a.Step()
		if a.Game.State == engine.StatePlaying {
			break
		}
	}
	if a.Game.State != engine.StatePlaying {
		t.Fatalf("never reached playing: %v", a.Game.State)
	}
	for range 30 {
		a.Step()
	}
	lives := a.Game.Lives
	a.Feed([]byte("k"))
	died := false
	for range 30 {
		a.Step()
		if a.Game.State == engine.StateDying {
			died = true
			break
		}
	}
	if !died || a.Game.Lives != lives-1 {
		t.Fatalf("died = %v state = %v lives = %d (was %d), want suicide death",
			died, a.Game.State, a.Game.Lives, lives)
	}
}

func TestLoadLevelsDefault(t *testing.T) {
	levels, err := LoadLevels("")
	if err != nil {
		t.Fatalf("LoadLevels: %v", err)
	}
	if levels != nil {
		t.Errorf("levels = %v, want nil (nil lets New use the built-ins)", levels)
	}
	// Regression: an explicitly-passed level set — even one equal to the
	// built-ins — is untrusted, and untrusted runs show UNRECORDED and
	// can never submit. LoadLevels("") returning the built-ins made the
	// native and ssh paths pass them explicitly, silently untrusting
	// every run.
	a := New(&Options{Levels: levels})
	if !a.levelsTrust {
		t.Fatal("default (nil) levels must keep runs replay-trusted")
	}
	if len(a.Game.Levels) != 32 {
		t.Errorf("New defaulted to %d levels, want 32", len(a.Game.Levels))
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "c.txt")
	rows := strings.Repeat(strings.Repeat(" ", 14)+"\n", 12) +
		"  M  G     F  \n##############\n##############"
	if err := os.WriteFile(path, []byte(rows), 0o644); err != nil {
		t.Fatal(err)
	}
	custom, err := LoadLevels(path)
	if err != nil {
		t.Fatalf("custom level: %v", err)
	}
	if New(&Options{Levels: custom}).levelsTrust {
		t.Fatal("custom levels must not be replay-trusted")
	}
}

func TestLoadLevelsCustom(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "custom.txt")
	rows := []string{
		"              ",
		"              ",
		"              ",
		"   c          ",
		"              ",
		"              ",
		"              ",
		"              ",
		"              ",
		"              ",
		"              ",
		"              ",
		"  M  G        ",
		"##############",
		"##############",
	}
	// Place flag in the last rows via rewrite: keep it simple with a 'T'/'F'.
	rows[12] = "  M  G     F  "
	if err := os.WriteFile(path, []byte(strings.Join(rows, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
	levels, err := LoadLevels(path)
	if err != nil {
		t.Fatalf("LoadLevels: %v", err)
	}
	if len(levels) != 1 || levels[0].Name != "custom.txt" {
		t.Fatalf("levels = %+v", levels)
	}

	// The custom level is playable through the demo path.
	var buf bytes.Buffer
	RunDemo(&buf, levels, render.Colors16, 6000)
	if !strings.Contains(buf.String(), "demo:") {
		t.Error("demo failed on custom level")
	}
}

func TestLoadLevelsErrors(t *testing.T) {
	if _, err := LoadLevels("/nonexistent/level.txt"); err == nil {
		t.Error("missing file: want error")
	}
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.txt")
	if err := os.WriteFile(bad, []byte("&&&\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadLevels(bad); err == nil {
		t.Error("invalid level: want error")
	}
}

func TestReadLevelRowsStripsCR(t *testing.T) {
	rows, err := readLevelRows(strings.NewReader("abc\r\ndef\r\n"))
	if err != nil || len(rows) != 2 || rows[0] != "abc" {
		t.Errorf("rows = %q err = %v", rows, err)
	}
}

func TestCheatsDisableRecording(t *testing.T) {
	// Cheat runs are deliberately unrecorded: the recorder must never
	// arm, so Shippable stays false and the leaderboard UI can refuse
	// submission on the existing UNRECORDED path.
	a := New(&Options{Cheats: true})
	if !a.Game.Cheats {
		t.Fatal("Options.Cheats must reach the engine")
	}
	a.Feed([]byte("\r")) // title → world card
	for range 600 {
		a.Step()
		if a.Game.State == engine.StatePlaying {
			break
		}
	}
	if a.Game.State != engine.StatePlaying {
		t.Fatalf("never reached playing: %v", a.Game.State)
	}
	for range 30 {
		a.Step()
	}
	if a.rec.Live() || a.rec.Shippable() {
		t.Fatal("cheat run must not be recorded")
	}
	// Cheats must survive a mid-run restart: Reset() reuses the same Game
	// object and must not clear the flag (pause-menu 'r' path).
	a.Feed([]byte("p"))
	for range 5 {
		a.Step()
	}
	a.Feed([]byte("r"))
	for range 5 {
		a.Step()
	}
	if !a.Game.Cheats {
		t.Fatal("Game.Cheats must survive Reset")
	}
	// A normal run arms the recorder at its first world card.
	b := New(nil)
	b.Feed([]byte("\r"))
	for range 600 {
		b.Step()
		if b.Game.State == engine.StatePlaying {
			break
		}
	}
	if !b.rec.Live() {
		t.Fatal("normal run should be recording")
	}
}

func TestSoundHookDeliversCoinEvent(t *testing.T) {
	// The full path: engine coin pickup → Options.Sound notification.
	// The hook must notify without consuming Game.Events (the browser
	// build still reads them for its synth).
	bld := engine.NewBuilder(40, engine.LevelHeight)
	bld.Ground(0, 39)
	bld.Flag(30)
	bld.Set(1, 12, 'c') // coin on the default spawn: collected on the first playing tick
	lvl, err := engine.ParseLevel("soundtest", bld.Rows())
	if err != nil {
		t.Fatalf("ParseLevel: %v", err)
	}
	var got []string
	a := New(&Options{
		Levels: []*engine.Level{lvl},
		Sound:  func(ev string) { got = append(got, ev) },
	})
	a.Feed([]byte("\r")) // AnyKey: title → world card → playing
	for range 600 {
		before := len(got)
		a.Step()
		for _, ev := range got[before:] {
			if !slices.Contains(a.Game.Events, ev) {
				t.Fatalf("hook event %q missing from Game.Events (the hook must not consume)", ev)
			}
		}
		if slices.Contains(got, "coin") {
			break
		}
	}
	if !slices.Contains(got, "coin") {
		t.Fatalf("sound hook never saw a coin event; events = %v", got)
	}
}

func recordedTicks(t *testing.T, a *App) int {
	t.Helper()
	var wire struct {
		Ticks int `json:"ticks"`
	}
	if err := json.Unmarshal([]byte(a.rec.JSON()), &wire); err != nil {
		t.Fatalf("recording JSON: %v", err)
	}
	return wire.Ticks
}

func TestRecorderSurvivesDeaths(t *testing.T) {
	// Regression (live bug, found 2026-08-30): prevState was assigned
	// AFTER Update, so when the recorder-arming switch examined the
	// first card tick of a death respawn, prevState had already become
	// StateWorldCard and the Dying/ScoreTick continuation case never
	// matched. Every death called rec.Start(), wiping the recording
	// down to the final life's segment; the replay verifier then
	// deleted the submission because the fragment replayed to a
	// different (smaller) game than the player actually played. A
	// recording must span the WHOLE run — deaths included — and must
	// reproduce it when replayed.
	a := New(nil)
	a.Feed([]byte("\r")) // AnyKey: title → world card
	for range 600 {
		a.Step()
		if a.Game.State == engine.StatePlaying {
			break
		}
	}
	if a.Game.State != engine.StatePlaying {
		t.Fatalf("never reached playing: %v", a.Game.State)
	}
	// Play long enough that the pre-death segment has substance.
	a.Feed([]byte("d")) // hold right (legacy byte → inferred hold)
	for range 240 {
		a.Step()
	}
	mid := recordedTicks(t, a)
	if mid == 0 {
		t.Fatal("run should be recording before the first death")
	}
	// Die repeatedly (natural deaths welcome) until the run is over.
	for i := 0; a.Game.State != engine.StateGameOver && i < 4000; i++ {
		if a.Game.State == engine.StatePlaying && i%60 == 0 {
			a.Feed([]byte("k"))
		}
		a.Step()
	}
	if a.Game.State != engine.StateGameOver {
		t.Fatalf("never game over: %v", a.Game.State)
	}
	final := recordedTicks(t, a)
	if final <= mid {
		t.Fatalf("recording restarted on death: mid=%d final=%d — a death-respawn card must continue the same recording", mid, final)
	}
	if !a.rec.Shippable() {
		t.Fatal("multi-death recording must stay shippable")
	}
	// The recording must replay to the run the player actually played.
	res, err := replay.Run(a.Game.Levels, "classic", a.rec.JSON())
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if res.Score != a.Game.Score || res.Level != a.Game.LevelIndex()+1 || res.State != a.Game.State {
		t.Fatalf("replay mismatch: replay scored=%d level=%d state=%s, live scored=%d level=%d state=%s",
			res.Score, res.Level, res.State, a.Game.Score, a.Game.LevelIndex()+1, a.Game.State)
	}
}

// The submit ask is once per RUN, not once per session: after declining
// and restarting from the game-over banner (mapped 'r', not the board's
// 'r' which re-arms itself), the next game over must offer submission
// again. The ask flag used to survive the restart forever — one decline
// silenced the prompt for the whole session.
func TestDeclineThenBannerRestartRearmsAsk(t *testing.T) {
	t.Setenv("SUPABASE_URL", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	a := New(nil)
	a.Feed([]byte("\r")) // AnyKey: title → world card → run
	for range 600 {
		a.Step()
		if a.Game.State == engine.StatePlaying {
			break
		}
	}
	if a.Game.State != engine.StatePlaying {
		t.Fatalf("never started run 1: %v", a.Game.State)
	}
	gameOver := func(score int) *render.ScoreUI {
		t.Helper()
		a.Game.Lives = 1
		a.Game.Score = score
		a.Feed([]byte("k"))
		for range 600 {
			a.Step()
			if a.Game.State == engine.StateGameOver {
				break
			}
		}
		if a.Game.State != engine.StateGameOver {
			t.Fatalf("never reached game over: %v", a.Game.State)
		}
		return a.UI()
	}

	// Run 1 ends with a score: the ask appears, the player declines.
	if ui := gameOver(120); ui == nil || ui.Mode != render.UIAsk {
		t.Fatalf("first game over should ask, got %+v", ui)
	}
	a.Feed([]byte("n"))
	for range 10 {
		a.Step()
		if a.UI() == nil {
			break
		}
	}
	if a.UI() != nil {
		t.Fatalf("decline should close the ask: %+v", a.UI())
	}

	// Banner restart: mapped 'r' straight from the game-over screen.
	a.Feed([]byte("r"))
	for range 600 {
		a.Step()
		if a.Game.State == engine.StatePlaying {
			break
		}
	}
	if a.Game.State != engine.StatePlaying {
		t.Fatalf("restart never reached playing: %v", a.Game.State)
	}

	// Run 2's game over must ask again.
	if ui := gameOver(200); ui == nil || ui.Mode != render.UIAsk {
		t.Fatalf("ask was not re-offered after banner restart, got %+v", ui)
	}
}
