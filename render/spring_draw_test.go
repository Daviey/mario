package render

import (
	"math"
	"testing"

	"github.com/Daviey/mario/engine"
)

// TestSpringSquashStagesVisible pins the springboard's compression
// feedback: the three stages must differ in geometry or plate colour.
// The stand-then-launch mechanic is unreadable without it (2026-09-02:
// the old open/closed coil swap differed by four dark pixels and was
// invisible in play — a rider could not tell loaded from idle).
func TestSpringSquashStagesVisible(t *testing.T) {
	pal := paletteFor(newGame(t), testPal)
	frame := func(compress int) *Frame {
		g := newGame(t)
		g.Springs = []*engine.Spring{{X: 13, Y: 12.5, Compress: compress}}
		return worldFrame(nil, g, testPal)
	}

	// The spring fixture sits at world x=13, top y=12.5, camera at 0:
	// mirror TestNewEntitiesDrawn's coordinate math.
	camY := CameraY(newGame(t))
	px := 13 * Pix
	yTop := int(math.Round((12.5 - camY) * Pix))
	col := px + 3 // centre column: inside every plate/coil/base row

	open, half, armed := frame(0), frame(5), frame(engine.SpringFullTicks)

	// Idle: red plate at the box top, base on the ground row.
	if got := open.At(col, yTop); got != pal.Player {
		t.Errorf("idle plate colour at top row = %+v, want red (pal.Player)", got)
	}
	// Squashed: the plate has dropped a pixel — the top row opens to
	// sky, the plate is one row down, and the base stays bottom-anchored.
	if got := half.At(col, yTop); got != pal.Sky {
		t.Errorf("squashed top row = %+v, want sky (plate must drop a pixel)", got)
	}
	if got := half.At(col, yTop+1); got != pal.Player {
		t.Errorf("squashed plate at yTop+1 = %+v, want red", got)
	}
	if got := half.At(col, yTop+3); got != pal.Dark {
		t.Errorf("squashed base row = %+v, want dark (bottom anchor must hold)", got)
	}
	// Armed: same squashed geometry, but the plate is gold — the
	// jump-now tell must differ from the mid-squash red by colour alone.
	if got := armed.At(col, yTop+1); got != pal.GoldLight {
		t.Errorf("armed plate colour = %+v, want gold (pal.GoldLight)", got)
	}
	if got := armed.At(col, yTop); got != pal.Sky {
		t.Errorf("armed top row = %+v, want sky (armed keeps the squash)", got)
	}

	// And the stages pairwise differ as whole frames — guards against a
	// future edit collapsing two stages into one render.
	for _, tc := range []struct {
		name string
		a, b *Frame
	}{
		{"idle vs squashed", open, half},
		{"squashed vs armed", half, armed},
	} {
		diff := 0
		for y := 0; y < tc.a.H; y++ {
			for x := 0; x < tc.a.W; x++ {
				if tc.a.At(x, y) != tc.b.At(x, y) {
					diff++
				}
			}
		}
		if diff == 0 {
			t.Errorf("%s: stages render identical frames", tc.name)
		}
	}
}
