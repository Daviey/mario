package render

import (
	"testing"

	"github.com/Daviey/mario/engine"
)

// TestCloudsNeverOverlapGeometryE2E scans full frames of every built-in
// level at sweeping camera positions: no cloud pixel may sit on a solid
// tile or the castle.
func TestCloudsNeverOverlapGeometryE2E(t *testing.T) {
	for _, lv := range engine.DefaultLevels() {
		g := engine.NewGame([]*engine.Level{lv}, 30, 12)
		g.State = engine.StatePlaying
		for camX := 0; camX < lv.Width-30; camX += 7 {
			g.CameraX = float64(camX)
			f := worldFrame(g, testPal)
			ox := int(g.CameraX * Pix)
			oy := int(CameraY(g) * Pix)
			for y := 0; y < f.H; y++ {
				for x := 0; x < f.W; x++ {
					if f.At(x, y) != testPal.Cloud {
						continue
					}
					tx := (x + ox) / Pix
					ty := (y + oy) / Pix
					if g.Level.At(tx, ty) != engine.Empty {
						t.Fatalf("%s camX=%d: cloud pixel on %v tile at (%d,%d)",
							lv.Name, camX, g.Level.At(tx, ty), tx, ty)
					}
				}
			}
		}
	}
}
