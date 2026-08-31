package art

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/Daviey/mario/render"
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

// The icon palette must stay in lockstep with the game's render palette:
// art exists so icon generators need not import the game (see mario.go's
// package doc), which makes its colors a copy — and this test the
// tripwire that keeps the copy honest. It reads render's exported
// palette values directly (NewPalette's fields are exported, so this is
// true parity, not a pinned copy). Test-only import: the package's
// runtime import graph stays game-free.
func TestPaletteMatchesRender(t *testing.T) {
	pal := render.NewPalette(render.Colors24)
	cases := []struct {
		name string
		got  color.RGBA
		want render.RGB
	}{
		{"cap/shirt 'R'", cols['R'], pal.Player.RGB},
		{"skin 'S'", cols['S'], pal.Skin.RGB},
		{"hair/boots 'D'", cols['D'], pal.Dark.RGB},
		{"overalls 'B'", cols['B'], pal.Overall.RGB},
		{"sky", sky, pal.Sky.RGB},
	}
	for _, c := range cases {
		rgb := uint32(c.got.R)<<16 | uint32(c.got.G)<<8 | uint32(c.got.B)
		if rgb != uint32(c.want) {
			t.Errorf("%s: art color %02X%02X%02X != render %06X", c.name, c.got.R, c.got.G, c.got.B, uint32(c.want))
		}
		if c.got.A != 0xFF {
			t.Errorf("%s: alpha = %02X, want opaque (FF)", c.name, c.got.A)
		}
	}
}

// The boot-face SPRITE has no render counterpart to read: render's
// in-game sprites are 7×7 (sprMarioSmall et al. in render/sprites.go)
// and export nothing, and the 5×5 face is a hand-drawn compact variant
// of that face — the only other copy is the web boot loader's favicon
// sprite (web/boot.js, `const sprMario = ...`). So this is a PINNED
// COPY, not parity: be honest that a render-side rename cannot trip it;
// what it does catch is the two hand copies (here and web/boot.js)
// drifting apart, and a rune losing its palette entry (Icon silently
// paints sky for unknown cols, erasing a feature).
// TODO: generate one copy from the other (or export the boot face from
// render) so this becomes real parity like the palette test above.
func TestBootFaceSpritePinned(t *testing.T) {
	pinned := []string{
		".RRR.",
		"RRRRR",
		"SSDSS",
		"RBBBR",
		".D.D.",
	}
	if len(sprMario) != len(pinned) {
		t.Fatalf("boot face has %d rows, want %d", len(sprMario), len(pinned))
	}
	for i, row := range pinned {
		if sprMario[i] != row {
			t.Errorf("boot face row %d = %q, want %q (pinned twin: web/boot.js)", i, sprMario[i], row)
		}
	}
	for _, row := range sprMario {
		for _, r := range row {
			if r == '.' {
				continue
			}
			if _, ok := cols[byte(r)]; !ok {
				t.Errorf("sprite rune %q has no palette entry — Icon would paint sky there", r)
			}
		}
	}
}
