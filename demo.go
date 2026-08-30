package mario

// Level loading and the deterministic scripted demo used by -demo, the
// UI previews and the determinism tests.

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Daviey/mario/engine"
	"github.com/Daviey/mario/internal/ui"
	"github.com/Daviey/mario/render"
)

// LoadLevels returns nil when the built-in set should be used (nil is
// mario.New's "default levels" signal — and what keeps those runs
// replay-verifiable: New marks explicitly-passed level sets untrusted,
// because a custom -level file cannot be reconstructed by the server's
// verifier), or a single custom level when levelPath is set.
func LoadLevels(levelPath string) ([]*engine.Level, error) {
	if levelPath == "" {
		return nil, nil
	}
	f, err := os.Open(levelPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	rows, err := readLevelRows(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", levelPath, err)
	}
	lvl, err := engine.ParseLevel(filepath.Base(levelPath), rows)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", levelPath, err)
	}
	return []*engine.Level{lvl}, nil
}

func readLevelRows(r io.Reader) ([]string, error) {
	var rows []string
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		rows = append(rows, strings.TrimRight(sc.Text(), "\r"))
	}
	return rows, sc.Err()
}

// RunDemo plays a deterministic scripted session with no terminal
// needed. A nil or empty levels slice falls back to the built-in set,
// matching New's "nil means default levels" rule.
func RunDemo(w io.Writer, levels []*engine.Level, colors render.ColorMode, ticks int) {
	if len(levels) == 0 {
		levels = engine.DefaultLevels()
	}
	g := engine.NewGame(levels, 20, engine.LevelHeight)
	for t := range ticks {
		g.Update(ui.ScriptInput(t))
	}
	fmt.Fprintf(w, "demo: ticks=%d score=%d coins=%d lives=%d state=%s level=%s\n",
		ticks, g.Score, g.CoinCount, g.Lives, g.State, g.LevelName())
	fmt.Fprint(w, render.FrameANSI(g, render.NewPalette(colors)))
}
