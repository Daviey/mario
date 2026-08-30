package render

import (
	"testing"

	"github.com/Daviey/mario/engine"
)

var testPal = NewPalette(Colors24)

// newGame builds a playing game on a decorated level (20-tile viewport).
// Everything interesting sits inside the first 20 tiles so it is visible
// with the camera at 0; the flag is far right for camera-scroll tests.
func newGame(t *testing.T) *engine.Game {
	t.Helper()
	b := engine.NewBuilder(80, engine.LevelHeight)
	b.Ground(0, 79)
	b.Set(2, 12, 'M')
	b.Set(6, 9, '?')
	b.Set(8, 9, 'B')
	b.Pipe(10, 2)
	b.Set(14, 12, 'G')
	b.Set(17, 12, 'K')
	b.Coins(8, 5, 6)
	b.Flag(70)
	l, err := engine.ParseLevel("t", b.Rows())
	if err != nil {
		t.Fatalf("ParseLevel: %v", err)
	}
	g := engine.NewGame([]*engine.Level{l}, 20, 9)
	g.State = engine.StatePlaying
	return g
}

// rowText returns the glyphs of one screen row.
func rowText(s *Screen, y int) string { return s.RowString(y) }

// worldPx reads a world pixel back out of the blitted screen (fg = upper
// half of the cell, bg = lower half). Row 0 is the first world pixel row.
func worldPx(s *Screen, x, y int) Color {
	c := s.At(x, 1+y/2)
	if y%2 == 0 {
		return c.Fg
	}
	return c.Bg
}
