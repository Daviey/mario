package render

import (
	"github.com/Daviey/mario/engine"
)

// Pixel-band heights for the canvas (browser) renderer. The terminal build
// draws HUD/status as text cell rows; the canvas build rasterizes them with
// the pixel font so every pixel on screen comes from the game.
const (
	HudBandPx    = 8 // 1px pad + 5px font + 2px pad
	StatusBandPx = 7 // 1px pad + 5px font + 1px pad
)

// RenderPixels renders a complete frame — HUD band, world, status band —
// as a raw square-pixel grid for direct canvas painting (browser build).
// The leaderboard UI is NOT rasterized: the browser renders it as DOM
// text (see wasm.go's marioBoard bridge).
// Width is ViewW*Pix; height HudBandPx + ViewH*Pix + StatusBandPx.
func RenderPixels(g *engine.Game, p *Palette) *Frame {
	world := worldFrame(g, p)
	f := NewFrame(world.W, world.H+HudBandPx+StatusBandPx, p.Sky)
	f.DrawFrame(world, 0, HudBandPx)
	drawHudPx(f, g, p)
	drawStatusPx(f, g, p)
	return f
}

// DrawFrame stamps another frame's pixels at (dx, dy), copying whole
// rows at a time (the composite paths move full frames; a per-pixel
// loop would walk every pixel twice).
func (f *Frame) DrawFrame(src *Frame, dx, dy int) {
	for y := range src.H {
		ty := dy + y
		if ty < 0 || ty >= f.H {
			continue
		}
		w := min(src.W, f.W-dx)
		copy(f.px[ty*f.W+dx:ty*f.W+dx+w], src.px[y*src.W:y*src.W+w])
	}
}

// RGBBytes packs the frame as tight RGB triplets (canvas ImageData input).
func (f *Frame) RGBBytes() []byte {
	out := make([]byte, f.W*f.H*3)
	i := 0
	for _, c := range f.px {
		out[i] = byte(c.RGB >> 16)
		out[i+1] = byte(c.RGB >> 8)
		out[i+2] = byte(c.RGB)
		i += 3
	}
	return out
}

// drawHudPx rasterizes the HUD band from the same content ladder as the
// terminal HUD (hudLadder): segments are stamped individually so the
// HURRY flash and the CHEATS tag keep their ink on this surface too.
func drawHudPx(f *Frame, g *engine.Game, p *Palette) {
	f.Fill(0, 0, f.W, HudBandPx, p.HUDBG)
	x := 2
	for _, seg := range hudPickPx(hudLadder(g), f.W-4) {
		drawTextPx(f, x, 1, seg.s, hudSegColor(seg, g, p), 1)
		x += textWidthPx(seg.s, 1) + 8
	}
}

// drawStatusPx rasterizes the status band from the same content ladder
// as the terminal status line (statusLadder). The 3×5 font upper-cases
// the shared lowercase text.
func drawStatusPx(f *Frame, g *engine.Game, p *Palette) {
	y := f.H - StatusBandPx
	f.Fill(0, y, f.W, StatusBandPx, p.StatusBG)
	if text := pickTextPx(statusLadder(g), f.W-2); text != "" {
		drawCenterPx(f, y+1, text, p.TextDim, 1)
	}
}
