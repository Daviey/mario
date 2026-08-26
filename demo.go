package mario

// Level loading and the deterministic scripted demo used by -demo, the
// UI previews and the determinism tests.

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Daviey/mario/engine"
	"github.com/Daviey/mario/internal/ui"
	"github.com/Daviey/mario/render"
)

// LoadLevels returns the built-in levels, or a single custom level when
// levelPath is set.
func LoadLevels(levelPath string) ([]*engine.Level, error) {
	if levelPath == "" {
		return engine.DefaultLevels(), nil
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
	lvl, err := engine.ParseLevel(baseName(levelPath), rows)
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

func baseName(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}

// RunDemo plays a deterministic scripted session with no terminal needed.
func RunDemo(w io.Writer, levels []*engine.Level, trueColor bool, ticks int) {
	g := engine.NewGame(levels, 20, engine.LevelHeight)
	for t := range ticks {
		g.Update(ui.ScriptInput(t))
	}
	fmt.Fprintf(w, "demo: ticks=%d score=%d coins=%d lives=%d state=%s level=%s\n",
		ticks, g.Score, g.CoinCount, g.Lives, g.State, g.LevelName())
	fmt.Fprint(w, render.FrameANSI(g, render.NewPalette(trueColor)))
}
