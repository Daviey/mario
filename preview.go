package mario

// UIPreview renders one leaderboard UI screen headless: the demo script
// runs to game over, then the machine is stepped to the requested mode and
// one ANSI frame is written (for visual checks, debugging and the CLI's
// -ui-preview flag).

import (
	"fmt"
	"io"
	"time"

	"mario/board"
	"mario/engine"
	"mario/render"
)

func UIPreview(w io.Writer, mode string, trueColor bool) error {
	g := engine.NewGame(engine.DefaultLevels(), 40, engine.LevelHeight)
	for t := range 6000 {
		g.Update(scriptInput(t))
	}
	if g.Score == 0 {
		return fmt.Errorf("demo script scored 0; cannot preview")
	}

	canned := []board.Row{
		{Name: "BIFF", Score: 32100},
		{Name: "DAVE", Score: 12500, Mine: true}, // "you"
		{Name: "KIM", Score: 9900},
	}
	ui := newScoreUI(nil, func() ([]board.Row, error) { return canned, nil })

	var frameG *engine.Game
	switch mode {
	case "ask":
		ui.tick(g) // game over auto-asks
	case "entry":
		ui.tick(g)
		ui.feedKeys([]byte("yDAVE")) // a half-typed name, cursor after it
		ui.tick(g)
		frameG = g
	case "board":
		// Direct board view (the submit path needs a real backend).
		ui.tick(g)
		ui.showBoard()
		time.Sleep(100 * time.Millisecond) // let the fake fetch land
		ui.tick(g)
	case "title-board":
		g2 := engine.NewGame(engine.DefaultLevels(), 40, engine.LevelHeight)
		ui.tick(g2)
		ui.showBoard()
		time.Sleep(100 * time.Millisecond)
		fmt.Fprint(w, render.FrameANSI(g2, render.NewPalette(trueColor), ui.tick(g2)))
		return nil
	default:
		return fmt.Errorf("unknown preview %q (want ask, entry, board, title-board)", mode)
	}
	if frameG == nil {
		frameG = g
	}
	fmt.Fprint(w, render.FrameANSI(frameG, render.NewPalette(trueColor), ui.tick(frameG)))
	return nil
}
