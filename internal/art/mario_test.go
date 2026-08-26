package art

import (
	"bytes"
	"image"
	"image/png"
	"testing"
)

func TestIconPNGDecodes(t *testing.T) {
	b := IconPNG(48, 8)
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("IconPNG not a valid PNG: %v", err)
	}
	if got := img.Bounds(); got != image.Rect(0, 0, 48, 48) {
		t.Fatalf("bounds = %v, want 48×48", got)
	}
	// The art is 5×8=40 px centered on a 48 px canvas: offsets 4..43.
	// A non-sky pixel inside that band proves the sprite was painted.
	rgba, ok := img.(*image.RGBA)
	if !ok {
		t.Fatalf("decoded image is %T, want *image.RGBA", img)
	}
	painted := false
	for y := 4; y < 44 && !painted; y++ {
		for x := 4; x < 44; x++ {
			o := rgba.PixOffset(x, y)
			r, g, b := rgba.Pix[o], rgba.Pix[o+1], rgba.Pix[o+2]
			if r != 0x5C || g != 0x94 || b != 0xFC {
				painted = true
				break
			}
		}
	}
	if !painted {
		t.Fatal("no sprite pixels inside the art band — canvas is all sky")
	}
}

func TestIconPNGDeterministic(t *testing.T) {
	a, b := IconPNG(192, 32), IconPNG(192, 32)
	if !bytes.Equal(a, b) {
		t.Fatal("IconPNG not byte-deterministic")
	}
}
