package render

import (
	"fmt"
	"strings"

	"mario/engine"
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
// Width is ViewW*Pix; height HudBandPx + ViewH*Pix + StatusBandPx.
func RenderPixels(g *engine.Game, p *Palette) *Frame {
	world := worldFrame(g, p)
	f := NewFrame(world.W, world.H+HudBandPx+StatusBandPx, p.Sky)
	f.DrawFrame(world, 0, HudBandPx)
	drawHudPx(f, g, p)
	drawStatusPx(f, p)
	return f
}

// DrawFrame stamps another frame's pixels at (dx, dy).
func (f *Frame) DrawFrame(src *Frame, dx, dy int) {
	for y := 0; y < src.H; y++ {
		for x := 0; x < src.W; x++ {
			f.Set(dx+x, dy+y, src.At(x, y))
		}
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

func drawHudPx(f *Frame, g *engine.Game, p *Palette) {
	f.Fill(0, 0, f.W, HudBandPx, p.HUDBG)
	hud := fmt.Sprintf("SCORE %06d  COINS x%02d  %s  TIME %03d  LIVES x%d",
		g.Score, g.CoinCount, strings.ToUpper(g.LevelName()), g.Time, g.Lives)
	drawTextPx(f, 2, 1, hud, p.Text, 1)
}

func drawStatusPx(f *Frame, p *Palette) {
	y := f.H - StatusBandPx
	f.Fill(0, y, f.W, StatusBandPx, p.StatusBG)
	drawCenterPx(f, y+1, "A/D MOVE  W JUMP  X RUN  P PAUSE  Q QUIT", p.TextDim, 1)
}
