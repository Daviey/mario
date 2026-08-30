package render

import (
	"math"
	"testing"

	"github.com/Daviey/mario/engine"
)

// TestCastleFlagMastMeetsCastle raises the victory flag fully and checks
// the mast column between the pennant and the castle roof: every pixel
// must be painted (pole or flag), never sky — the mast must meet the
// castle with no floating gap.
func TestCastleFlagMastMeetsCastle(t *testing.T) {
	g := newGame(t)
	g.Player.Pos = engine.Vec{X: float64(g.Level.FlagX + 5), Y: float64(engine.GroundTop - engine.SmallH)}
	g.InCastle = true
	g.CastleFlag = 1
	g.CameraX = float64(g.Level.Width - g.ViewW) // castle on-screen, clamped
	s := Render(g, testPal)

	cx, cy, _, _ := castleRect(g)
	oy := int(math.Round(CameraY(g) * Pix))       // mirrors worldFrame
	sx := cx*Pix + 2*Pix + 2 - int(g.CameraX)*Pix // drawCastleFlag: mx = x0 + 2*Pix + 2
	base := cy*Pix - oy                           // screen-pixel row of the roof top

	for y := base - 8; y < base; y++ { // h=8 in drawCastleFlag
		if got := worldPx(s, sx, y); got == testPal.Sky {
			t.Fatalf("sky gap in victory-flag mast at (%d,%d): mast floats above the castle", sx, y)
		}
	}
}
