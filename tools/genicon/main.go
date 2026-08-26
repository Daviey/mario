// genicon renders the PWA icons (web/icons/*.png) from the classic 5×5
// boot-screen Mario sprite — the same art as the loader in web/index.html
// and render/sprites.go, so the home-screen icon, the favicon and the boot
// screen all show the same face. Stdlib only; run from the repo root:
//
//	CGO_ENABLED=0 go run ./tools/genicon
//
// The PNGs are committed; regenerate only when the sprite art changes.
package main

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
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

// icon renders the sprite centered on a size×size sky canvas at cell
// sprite-pixels per art pixel (the same math as the loader favicon:
// size/6 ≈ 83% coverage; maskable icons keep the art in the ~60% safe
// zone so Android's circular mask cannot crop Mario).
func icon(size, cell int) *image.RGBA {
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

func write(path string, img *image.RGBA) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

func main() {
	dir := filepath.Join("web", "icons")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "genicon:", err)
		os.Exit(1)
	}
	for _, ic := range []struct {
		name string
		size int
		cell int
	}{
		{"icon-192.png", 192, 32},          // 160 px art, ~83%
		{"icon-512.png", 512, 85},          // 425 px art, ~83%
		{"icon-maskable-512.png", 512, 61}, // 305 px art, ~60% safe zone
	} {
		p := filepath.Join(dir, ic.name)
		if err := write(p, icon(ic.size, ic.cell)); err != nil {
			fmt.Fprintln(os.Stderr, "genicon:", err)
			os.Exit(1)
		}
		fmt.Println("wrote", p)
	}
}
