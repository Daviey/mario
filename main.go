// Command mario is a fully terminal-based Mario-style platformer.
//
// Controls: a/d or arrows move, w/space jump, x run, p pause, q quit,
// r restart (after game over / win).
//
// Flags:
//
//	-demo        run a headless scripted demo and exit
//	-level FILE  play a custom ASCII level instead of the built-ins
//	-width N     viewport width in tiles (default: terminal width)
//	-scores N    print the top N leaderboard scores and exit
//
// Scores can be submitted to a Supabase-backed leaderboard after game over;
// see the board package and .env.
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"mario/engine"
	"mario/render"
)

// newStream wires raw output straight through: each frame is one diff
// write, so buffering would only stall output until the buffer filled.
func newStream(out io.Writer, pal *render.Palette) *render.Stream {
	return render.NewStream(out, pal)
}

// loadLevels returns the built-in levels, or a single custom level when
// levelPath is set.
func loadLevels(levelPath string) ([]*engine.Level, error) {
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

// scriptInput is the deterministic demo script: hold right, run most ticks,
// hop regularly, dismiss the title screen on tick 0.
func scriptInput(t int) engine.Input {
	return engine.Input{
		Right:  true,
		Run:    t%3 != 0,
		Up:     t%97 < 22,
		AnyKey: t == 0, // dismiss the title screen
	}
}

// runDemo plays a deterministic scripted session with no terminal needed.
func runDemo(w io.Writer, levels []*engine.Level, trueColor bool, ticks int) {
	g := engine.NewGame(levels, 20, engine.LevelHeight)
	for t := range ticks {
		g.Update(scriptInput(t))
	}
	fmt.Fprintf(w, "demo: ticks=%d score=%d coins=%d lives=%d state=%s level=%s\n",
		ticks, g.Score, g.CoinCount, g.Lives, g.State, g.LevelName())
	fmt.Fprint(w, render.FrameANSI(g, render.NewPalette(trueColor)))
}

func isTTY(f *os.File) bool {
	st, err := f.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}

// play runs the game loop, drawing differential frames until quit.
// Shared by the native terminal runner and the browser (WASM) build.
func play(g *engine.Game, io *gameIO, st *render.Stream) {
	draw := func(ui *render.ScoreUI) { st.Draw(g, ui) }
	draw(nil) // first frame: full repaint

	ticker := time.NewTicker(time.Second / engine.TicksPerSecond)
	defer ticker.Stop()
	for {
		<-ticker.C
		in := io.poll()
		g.Update(in)
		draw(io.uiTick(g))
		if in.Quit || io.quitRequested() {
			return
		}
	}
}
