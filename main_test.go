package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"mario/engine"
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

func TestStdinPumpHandoff(t *testing.T) {
	// Post-game keystrokes must reach the submit prompt — never the game's
	// input mapper — once play ends. The prompt reads through the same
	// os.Pipe run() wires into maybeSubmit.
	var mu sync.Mutex
	var gameGot []string
	game := func(b []byte) {
		mu.Lock()
		defer mu.Unlock()
		gameGot = append(gameGot, string(b))
	}
	promptR, promptW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	p := &stdinPump{toGame: true, game: game, prompt: promptW}

	p.route([]byte("d")) // during play: game keys go to the mapper
	p.switchToPrompt()
	p.route([]byte("n\n")) // after play: the answer goes to the prompt

	buf := make([]byte, 8)
	n, rerr := promptR.Read(buf)
	if rerr != nil || string(buf[:n]) != "n\n" {
		t.Fatalf("prompt received %q (err %v); want %q", buf[:n], rerr, "n\n")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(gameGot) != 1 || gameGot[0] != "d" {
		t.Fatalf("game sink received %q; want only the pre-handoff bytes", gameGot)
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
