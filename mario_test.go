package mario

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
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

func TestLoadLevelsDefault(t *testing.T) {
	levels, err := LoadLevels("")
	if err != nil {
		t.Fatalf("LoadLevels: %v", err)
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
