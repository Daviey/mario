package mario

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/Daviey/mario/board"
	"github.com/Daviey/mario/engine"
	"github.com/Daviey/mario/internal/ui"
	"github.com/Daviey/mario/render"
)

// UIPreview renders one leaderboard UI screen headless: the demo script
// runs to game over, then the machine is stepped to the requested mode
// and one ANSI frame is written (for visual checks, debugging and the
// CLI's -ui-preview flag).
func UIPreview(w io.Writer, mode string, colors render.ColorMode) error {
	g := engine.NewGame(engine.DefaultLevels(), 40, engine.LevelHeight)
	for t := range 6000 {
		g.Update(ui.ScriptInput(t))
	}
	if g.Score == 0 {
		return errors.New("demo script scored 0; cannot preview")
	}

	canned := []board.Row{
		{Name: "BIFF", Score: 32100, Level: 3},
		{Name: "DAVE", Score: 12500, Level: 2, Mine: true}, // "you"
		{Name: "KIM", Score: 9900, Level: 1},
	}
	mach := ui.NewUI(nil, func() ([]board.Row, error) { return canned, nil })

	var frameG *engine.Game
	switch mode {
	case "ask":
		mach.Tick(g) // game over auto-asks
	case "entry":
		mach.Tick(g)
		mach.FeedKeys([]byte("yDAVE")) // a half-typed name, cursor after it
		mach.Tick(g)
		frameG = g
	case "board":
		// Direct board view (the submit path needs a real backend).
		mach.Tick(g)
		mach.ShowBoard()
		if _, err := settleBoard(mach, g); err != nil {
			return err
		}
	case "about":
		g2 := engine.NewGame(engine.DefaultLevels(), 40, engine.LevelHeight)
		mach.Tick(g2)
		mach.ShowAbout()
		fmt.Fprint(w, render.FrameANSI(g2, render.NewPalette(colors), mach.Tick(g2)))
		return nil
	case "title-board":
		g2 := engine.NewGame(engine.DefaultLevels(), 40, engine.LevelHeight)
		mach.Tick(g2)
		mach.ShowBoard()
		snap, err := settleBoard(mach, g2)
		if err != nil {
			return err
		}
		fmt.Fprint(w, render.FrameANSI(g2, render.NewPalette(colors), snap))
		return nil
	default:
		return fmt.Errorf("unknown preview %q (want ask, entry, board, title-board, about)", mode)
	}
	if frameG == nil {
		frameG = g
	}
	fmt.Fprint(w, render.FrameANSI(frameG, render.NewPalette(colors), mach.Tick(frameG)))
	return nil
}

// settleBoard ticks the machine until the fetch lands (Loading cleared),
// bounded: the canned fetch returns instantly, so the deadline only
// guards against a wedged machine hanging the preview forever.
func settleBoard(u *ui.UI, g *engine.Game) (*render.ScoreUI, error) {
	deadline := time.Now().Add(2 * time.Second)
	for {
		snap := u.Tick(g)
		if snap == nil || !snap.Loading {
			return snap, nil
		}
		if time.Now().After(deadline) {
			return nil, errors.New("board fetch did not settle")
		}
		time.Sleep(2 * time.Millisecond)
	}
}
