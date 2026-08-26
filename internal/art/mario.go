// Package art renders the shared mario pixel art (the boot-screen face)
// as standalone images. It exists so icon generators (tools/genicon for
// the PWA icons, tools/mkdeb and the AUR build for package icons) all
// show the same face without importing the game itself — the art mirrors
// render/sprites.go verbatim, like the web loader's copy in index.html.
package art

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
)

// Palette hexes mirror render.go NewPalette / the web loader's PC map.
var cols = map[byte]color.RGBA{
	'R': {R: 0xFF, G: 0x3B, B: 0x30, A: 0xFF}, // cap/shirt
	'S': {R: 0xFF, G: 0xC8, B: 0x9E, A: 0xFF}, // skin
	'D': {R: 0x1A, G: 0x0E, B: 0x04, A: 0xFF}, // hair/boots
	'B': {R: 0x2B, G: 0x5D, B: 0xD7, A: 0xFF}, // overalls
}

var sky = color.RGBA{R: 0x5C, G: 0x94, B: 0xFC, A: 0xFF} // sky blue, full bleed

// sprMario — the boot-screen sprite, render/sprites.go verbatim.
var sprMario = []string{
	".RRR.",
	"RRRRR",
	"SSDSS",
	"RBBBR",
	".D.D.",
}

// Icon renders the sprite centered on a size×size sky canvas at cell
// sprite-pixels per art pixel (the same math as the loader favicon:
// size/6 ≈ 83% coverage; maskable icons keep the art in the ~60% safe
// zone so Android's circular mask cannot crop Mario).
func Icon(size, cell int) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := img.Rect.Min.Y; y < img.Rect.Max.Y; y++ {
		for x := img.Rect.Min.X; x < img.Rect.Max.X; x++ {
			img.SetRGBA(x, y, sky)
		}
	}
	ox := (size - len(sprMario[0])*cell) / 2
	oy := (size - len(sprMario)*cell) / 2
	for r, row := range sprMario {
		for c := range len(row) {
			col, ok := cols[row[c]]
			if !ok {
				continue // '.' transparent → sky shows through
			}
			for dy := range cell {
				for dx := range cell {
					img.SetRGBA(ox+c*cell+dx, oy+r*cell+dy, col)
				}
			}
		}
	}
	return img
}

// IconPNG renders Icon and returns it as encoded PNG bytes.
func IconPNG(size, cell int) []byte {
	var buf bytes.Buffer
	if err := png.Encode(&buf, Icon(size, cell)); err != nil {
		// Unreachable for an in-memory *image.RGBA.
		panic("art: png encode: " + err.Error())
	}
	return buf.Bytes()
}
