// Command shots renders the README screenshots: it drives a real game with
// the deterministic attract script and writes truecolor ANSI frames, which
// ansi2png.py (same directory) rasterizes. Nothing here draws by itself —
// every frame comes from the shipped renderer, so the screenshots cannot
// drift from the game.
//
//	CGO_ENABLED=0 go run ./tools/shots -scene title -out /tmp/title.ansi
//	CGO_ENABLED=0 go run ./tools/shots -scene play -level 2 -tick 700 -out /tmp/l2.ansi
//	CGO_ENABLED=0 go run ./tools/shots -scene board -out /tmp/board.ansi
//	CGO_ENABLED=0 go run ./tools/shots -scene strip -from 600 -to 1400 -step 2 -out /tmp/gif/frame
//
// Levels are 1-based (1-1 .. 2-4). All scenes default to a 40-tile viewport.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/Daviey/mario/board"
	"github.com/Daviey/mario/engine"
	"github.com/Daviey/mario/internal/ui"
	"github.com/Daviey/mario/render"
)

func main() {
	scene := flag.String("scene", "title", "title | play | board | strip")
	level := flag.Int("level", 1, "1-based level number (play/strip)")
	tick := flag.Int("tick", 600, "frame captured at this tick (play)")
	from := flag.Int("from", 600, "strip start tick")
	to := flag.Int("to", 1400, "strip end tick (inclusive)")
	step := flag.Int("step", 2, "strip tick step")
	width := flag.Int("width", 40, "viewport width in tiles")
	out := flag.String("out", "", "output file (strip: printf prefix)")
	flag.Parse()

	if *out == "" {
		fmt.Fprintln(os.Stderr, "shots: -out is required")
		os.Exit(2)
	}

	switch *scene {
	case "title":
		g := engine.NewGame(engine.DefaultLevels(), *width, engine.LevelHeight)
		writeFrame(*out, g, nil)
	case "play":
		writeFrame(*out, playTo(*level, *tick, *width), nil)
	case "board":
		boardFrame(*out)
	case "strip":
		if *step < 1 || *to < *from {
			fmt.Fprintln(os.Stderr, "shots: bad strip range")
			os.Exit(2)
		}
		g := engine.NewGame(oneLevel(*level), *width, engine.LevelHeight)
		for t := 0; t <= *to; t++ {
			if t >= *from && (t-*from)%*step == 0 {
				writeFrame(fmt.Sprintf("%s-%04d.ansi", *out, t), g, nil)
			}
			g.Update(ui.ScriptInput(t))
		}
	default:
		fmt.Fprintln(os.Stderr, "shots: unknown scene", *scene)
		os.Exit(2)
	}
}

func oneLevel(n int) []*engine.Level {
	levels := engine.DefaultLevels()
	if n < 1 || n > len(levels) {
		fmt.Fprintln(os.Stderr, "shots: level out of range 1..", len(levels))
		os.Exit(2)
	}
	return levels[n-1 : n]
}

// playTo runs the attract script (hold right, run, hop) on one level.
func playTo(level, ticks, width int) *engine.Game {
	g := engine.NewGame(oneLevel(level), width, engine.LevelHeight)
	for t := range ticks {
		g.Update(ui.ScriptInput(t))
	}
	return g
}

// boardFrame drives the demo to game over, then shows the leaderboard
// against canned rows — the same path as `mario -ui-preview board`, with a
// fuller board so the screenshot reads like a real one.
func boardFrame(path string) {
	g := engine.NewGame(engine.DefaultLevels(), 40, engine.LevelHeight)
	for t := range 6000 {
		g.Update(ui.ScriptInput(t))
	}
	rows := []board.Row{
		{Name: "LUIGI", Score: 66800, Level: 5, Verified: true},
		{Name: "TOAD", Score: 51250, Level: 4, Verified: true},
		{Name: "BIFF", Score: 32100, Level: 3, Verified: true},
		{Name: "PEACH", Score: 28400, Level: 3},
		{Name: "DAVE", Score: 12500, Level: 2, Mine: true, Verified: true},
		{Name: "KIM", Score: 9900, Level: 1},
		{Name: "WARIO", Score: 6400, Level: 1, Verified: true},
	}
	u := ui.NewUI(nil, func() ([]board.Row, error) { return rows, nil })
	u.Tick(g)
	u.ShowBoard()
	time.Sleep(100 * time.Millisecond) // let the fake fetch land
	writeFrame(path, g, u.Tick(g))
}

func writeFrame(path string, g *engine.Game, overlay *render.ScoreUI) {
	f, err := os.Create(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "shots:", err)
		os.Exit(1)
	}
	defer f.Close()
	if overlay != nil {
		fmt.Fprint(f, render.FrameANSI(g, render.NewPalette(render.Colors24), overlay))
	} else {
		fmt.Fprint(f, render.FrameANSI(g, render.NewPalette(render.Colors24)))
	}
}
