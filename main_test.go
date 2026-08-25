package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mario/engine"
	"mario/input"
	"mario/render"
)

func TestRunDemo(t *testing.T) {
	var buf bytes.Buffer
	runDemo(&buf, engine.DefaultLevels(), true, 6000)
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
	runDemo(&buf2, engine.DefaultLevels(), true, 6000)
	if buf.String() != buf2.String() {
		t.Error("demo output is not deterministic")
	}
}

func TestGameIOKeyboardOwnership(t *testing.T) {
	// While the leaderboard UI captures input, keystrokes must never reach
	// the game mapper — typing a name with 'p' in it must not pause, 'r'
	// must not restart. One consumer owns the keyboard at a time.
	m := input.NewMapper()
	ui := newScoreUI(nil, nil)
	io := newGameIO(m, ui)

	io.feed([]byte("p"))
	if in := io.poll(); !in.Pause {
		t.Fatal("during play the mapper must receive keys")
	}

	ui.mu.Lock()
	ui.mode = render.UIEntry
	ui.mu.Unlock()
	io.feed([]byte("p")) // also a valid name letter
	if in := io.poll(); in.Pause {
		t.Fatal("key leaked to the mapper during name entry")
	}
	ui.mu.Lock()
	got := string(ui.keys)
	ui.mu.Unlock()
	if got != "p" {
		t.Fatalf("UI captured %q; want %q", got, "p")
	}
}

// With kitty flags 1|2|8 pushed, letters arrive as CSI-u events. The
// byte-oriented UI and the title 'l' trigger must still see plain bytes,
// while the mapper consumes the raw stream natively.
func TestGameIODecodesKittyForUI(t *testing.T) {
	m := input.NewMapper()
	ui := newScoreUI(nil, nil)
	io := newGameIO(m, ui)

	io.feed([]byte("\x1b[100;1:1u")) // press 'd'
	if in := io.poll(); !in.Right {
		t.Fatal("kitty press must reach the mapper during play")
	}
	ui.mu.Lock()
	if noted := string(ui.noted); noted != "d" {
		ui.mu.Unlock()
		t.Fatalf("title trigger noted %q; want %q", noted, "d")
	}
	ui.mu.Unlock()

	ui.mu.Lock()
	ui.mode = render.UIEntry
	ui.mu.Unlock()
	io.feed([]byte("\x1b[112;1:1u")) // press 'p': a valid name letter
	if in := io.poll(); in.Pause {
		t.Fatal("kitty key leaked to the mapper during name entry")
	}
	ui.mu.Lock()
	got := string(ui.keys)
	ui.mu.Unlock()
	if got != "p" {
		t.Fatalf("UI captured %q; want %q", got, "p")
	}
	ui.mu.Lock()
	ui.keys = nil // drain: only the release event follows
	ui.mu.Unlock()

	io.feed([]byte("\x1b[112;1:3u")) // release 'p': no edge, no effect
	ui.mu.Lock()
	got = string(ui.keys)
	ui.mu.Unlock()
	if got != "" {
		t.Fatalf("release event reached the UI as %q", got)
	}
}

func TestLoadLevelsDefault(t *testing.T) {
	levels, err := loadLevels("")
	if err != nil {
		t.Fatalf("loadLevels: %v", err)
	}
	if len(levels) != 3 {
		t.Errorf("levels = %d, want 3", len(levels))
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
	levels, err := loadLevels(path)
	if err != nil {
		t.Fatalf("loadLevels: %v", err)
	}
	if len(levels) != 1 || levels[0].Name != "custom.txt" {
		t.Fatalf("levels = %+v", levels)
	}

	// The custom level is playable through the demo path.
	var buf bytes.Buffer
	runDemo(&buf, levels, false, 6000)
	if !strings.Contains(buf.String(), "demo:") {
		t.Error("demo failed on custom level")
	}
}

func TestLoadLevelsErrors(t *testing.T) {
	if _, err := loadLevels("/nonexistent/level.txt"); err == nil {
		t.Error("missing file: want error")
	}
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.txt")
	if err := os.WriteFile(bad, []byte("ZZZ\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := loadLevels(bad); err == nil {
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
