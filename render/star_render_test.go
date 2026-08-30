package render

import (
	"testing"

	"github.com/Daviey/mario/engine"
)

// TestStarRender pins the visual contracts of star power: the star sprite
// draws, and the palette flicker changes player pixels over time.
func TestStarRender(t *testing.T) {
	l := engine.DefaultLevels()[2] // 1-3 carries the star block at (75, 9)
	g := engine.NewGame([]*engine.Level{l}, 40, 12)
	g.State = engine.StatePlaying
	g.CameraX = 70
	g.Mushrooms = append(g.Mushrooms, &engine.Mushroom{
		Pos:  engine.Vec{X: 74, Y: 11 - engine.MushroomH},
		Kind: engine.MushStar,
	})
	f := worldFrame(g, testPal)
	starGold := 0
	x0 := int((74 - 70) * Pix)
	for y := 5 * Pix; y < 12*Pix; y++ {
		for x := x0; x < x0+Pix; x++ {
			if f.At(x, y) == testPal.Coin {
				starGold++
			}
		}
	}
	if starGold == 0 {
		t.Error("star sprite not drawn in its tile")
	}

	// Star flicker: identical scene two flicker phases apart must differ.
	g.Player.Star = 100
	g.Player.Pos = engine.Vec{X: 72, Y: 11}
	g.Player.Vel = engine.Vec{}
	g.Tick = 0
	h0 := regionHash(worldFrame(g, testPal), 6, 42, 30, 66)
	g.Tick = 3
	h1 := regionHash(worldFrame(g, testPal), 6, 42, 30, 66)
	if h0 == h1 {
		t.Error("star flicker did not change player pixels between phases")
	}
}

// TestHiddenBlocksInvisible: hidden blocks render as plain sky until used.
func TestHiddenBlocksInvisible(t *testing.T) {
	// Pick a column no deterministic cloud decorates (anchors span 3 tiles).
	col := 3
	for x := 3; x < 12; x++ {
		clear := true
		for a := x - 2; a <= x+2; a++ {
			if _, ok := CloudAt(a); ok {
				clear = false
				break
			}
		}
		if clear {
			col = x
			break
		}
	}
	blank := "              "
	rows := make([]string, 15)
	for i := range rows {
		rows[i] = blank
	}
	rows[13] = "##############"
	rows[14] = "##############"
	setCh := func(row string, ch byte) string {
		b := []byte(row)
		b[col] = ch
		return string(b)
	}
	rows[5] = setCh(blank, 'H')
	rows[10] = setCh(blank, '1')
	flagRow := []byte(blank)
	flagRow[9] = 'F'
	rows[11] = string(flagRow)
	lvl, err := engine.ParseLevel("hidden", rows)
	if err != nil {
		t.Fatalf("ParseLevel: %v", err)
	}
	g := engine.NewGame([]*engine.Level{lvl}, 14, 12)
	g.State = engine.StatePlaying
	g.CameraX = 0
	f := worldFrame(g, testPal)
	oy := int(CameraY(g) * Pix)
	for _, ty := range []int{5, 10} {
		for y := ty*Pix - oy; y < ty*Pix-oy+Pix; y++ {
			for x := col * Pix; x < col*Pix+Pix; x++ {
				if f.At(x, y) != testPal.Sky {
					t.Errorf("hidden block at (%d,%d) is visible", col, ty)
				}
			}
		}
	}
}
