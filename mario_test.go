package mario

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Daviey/mario/engine"
)

func TestRunDemo(t *testing.T) {
	var buf bytes.Buffer
	RunDemo(&buf, engine.DefaultLevels(), true, 6000)
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
	RunDemo(&buf2, engine.DefaultLevels(), true, 6000)
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
	if len(levels) != 7 {
		t.Errorf("levels = %d, want 7", len(levels))
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
	RunDemo(&buf, levels, false, 6000)
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
	if err := os.WriteFile(bad, []byte("ZZZ\n"), 0o644); err != nil {
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

func TestBaseName(t *testing.T) {
	for path, want := range map[string]string{
		"/tmp/lv.txt": "lv.txt",
		"lv.txt":      "lv.txt",
	} {
		if got := baseName(path); got != want {
			t.Errorf("baseName(%q) = %q", path, got)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
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
