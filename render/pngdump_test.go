package render

import (
	"image"
	"image/png"
	"os"
	"testing"

	ic "image/color"
)

// TestDumpPng renders a close-up strip of world tiles at 24x nearest
// neighbor (guarded by DUMP_PNG env: it is a visual debugging aid).
func TestDumpPng(t *testing.T) {
	if os.Getenv("DUMP_PNG") == "" {
		t.Skip("set DUMP_PNG=1")
	}
	rc := runeColors(testPal)
	f := NewFrame(10*Pix, 3*Pix, testPal.Sky)
	// A run of world blocks exactly as the game paints them.
	drawQuestion(f, testPal, 0*Pix, Pix, true)
	drawQuestion(f, testPal, 1*Pix, Pix, true)
	drawBrick(f, testPal, 2*Pix, Pix, 0)
	drawQuestion(f, testPal, 3*Pix, Pix, true)
	drawUsed(f, testPal, 4*Pix, Pix)
	drawQuestion(f, testPal, 5*Pix, Pix, true)
	drawBrick(f, testPal, 6*Pix, Pix, 1)
	drawQuestion(f, testPal, 7*Pix, Pix, true)
	f.DrawSprite(sprCoin, rc, 8*Pix+1, 0, false, 1)
	f.DrawSprite(sprCoin, rc, 8*Pix+1, 2*Pix, false, 1)
	f.DrawSprite(sprMarioSmall, rc, 9*Pix-3, 2*Pix-sprH(sprMarioSmall), false, 1)

	const zoom = 24
	img := image.NewNRGBA(image.Rect(0, 0, f.W*zoom, f.H*zoom))
	for y := range f.H {
		for x := range f.W {
			c := f.At(x, y)
			col := ic.NRGBA{R: byte(c.RGB >> 16), G: byte(c.RGB >> 8), B: byte(c.RGB), A: 255}
			for dy := range zoom {
				for dx := range zoom {
					img.SetNRGBA(x*zoom+dx, y*zoom+dy, col)
				}
			}
		}
	}
	w, err := os.Create(os.Getenv("DUMP_PNG"))
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := png.Encode(w, img); err != nil {
		t.Fatal(err)
	}
}
