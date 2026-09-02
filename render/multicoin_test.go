package render

import (
	"testing"

	"github.com/Daviey/mario/engine"
)

// TestBrickCoinIsDisguisedAsBrick pins the multi-coin brick's visual
// contract: it renders pixel-identical to a plain brick — the disguise
// IS the mechanic (SMB1's ten-coin block looks like every other brick
// until bumped).
func TestBrickCoinIsDisguisedAsBrick(t *testing.T) {
	g := newGame(t)
	g.Level.Set(10, 9, engine.Brick)
	g.Level.Set(12, 9, engine.BrickCoin)
	f := worldFrame(nil, g, testPal)
	for y := 0; y < Pix; y++ {
		for x := 0; x < Pix; x++ {
			if f.At(10*Pix+x, 9*Pix+y) != f.At(12*Pix+x, 9*Pix+y) {
				t.Fatalf("disguise broken at sprite pixel (%d,%d): brick and brick-coin differ", x, y)
			}
		}
	}
}
