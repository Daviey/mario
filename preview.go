package mario

// UIPreview renders one leaderboard UI screen headless: the demo script
// runs to game over, then the machine is stepped to the requested mode and
// one ANSI frame is written (for visual checks, debugging and the CLI's
// -ui-preview flag).

import (
	"fmt"
	"io"
	"time"

	"github.com/Daviey/mario/board"
	"github.com/Daviey/mario/engine"
	"github.com/Daviey/mario/internal/ui"
	"github.com/Daviey/mario/render"
)

func UIPreview(w io.Writer, mode string, colors int) error {
	g := engine.NewGame(engine.DefaultLevels(), 40, engine.LevelHeight)
	for t := range 6000 {
		g.Update(ui.ScriptInput(t))
	}
	if g.Score == 0 {
		return fmt.Errorf("demo script scored 0; cannot preview")
	}

	canned := []board.Row{
		{Name: "BIFF", Score: 32100, Level: 3},
		{Name: "DAVE", Score: 12500, Level: 2, Mine: true}, // "you"
		{Name: "KIM", Score: 9900, Level: 1},
	}
	ui := ui.NewUI(nil, func() ([]board.Row, error) { return canned, nil })

	var frameG *engine.Game
	switch mode {
	case "ask":
		ui.Tick(g) // game over auto-asks
	case "entry":
		ui.Tick(g)
		ui.FeedKeys([]byte("yDAVE")) // a half-typed name, cursor after it
		ui.Tick(g)
		frameG = g
	case "board":
		// Direct board view (the submit path needs a real backend).
		ui.Tick(g)
		ui.ShowBoard()
		time.Sleep(100 * time.Millisecond) // let the fake fetch land
		ui.Tick(g)
	case "about":
		g2 := engine.NewGame(engine.DefaultLevels(), 40, engine.LevelHeight)
		ui.Tick(g2)
		ui.ShowAbout()
		fmt.Fprint(w, render.FrameANSI(g2, render.NewPalette(colors), ui.Tick(g2)))
		return nil
	case "title-board":
		g2 := engine.NewGame(engine.DefaultLevels(), 40, engine.LevelHeight)
		ui.Tick(g2)
		ui.ShowBoard()
		time.Sleep(100 * time.Millisecond)
		fmt.Fprint(w, render.FrameANSI(g2, render.NewPalette(colors), ui.Tick(g2)))
		return nil
	default:
		return fmt.Errorf("unknown preview %q (want ask, entry, board, title-board, about)", mode)
	}
	if frameG == nil {
		frameG = g
	}
	fmt.Fprint(w, render.FrameANSI(frameG, render.NewPalette(colors), ui.Tick(frameG)))
	return nil
}
