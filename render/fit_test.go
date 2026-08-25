package render

import (
	"testing"

	"mario/engine"
)

// newSizedGame builds a playing game at an explicit viewport size.
func newSizedGame(t *testing.T, viewW, viewH int) *engine.Game {
	t.Helper()
	b := engine.NewBuilder(80, engine.LevelHeight)
	b.Ground(0, 79)
	b.Set(2, 12, 'M')
	b.Flag(70)
	l, err := engine.ParseLevel("t", b.Rows())
	if err != nil {
		t.Fatalf("ParseLevel: %v", err)
	}
	g := engine.NewGame([]*engine.Level{l}, viewW, viewH)
	g.State = engine.StatePlaying
	return g
}

func TestPickTextPx(t *testing.T) {
	got := pickTextPx([]string{"AAAAAA", "AAA", "A"}, 13)
	if got != "AAA" { // 6*4-1=23 > 13, 3*4-1=11 <= 13
		t.Errorf("pickTextPx = %q, want %q", got, "AAA")
	}
	if got := pickTextPx([]string{"AAAAAA"}, 4); got != "AAAAAA" {
		t.Errorf("last candidate must be the fallback, got %q", got)
	}
}

func TestHudFitsEveryViewportWidth(t *testing.T) {
	// At every supported width the HUD text must end inside the frame.
	for _, viewW := range []int{16, 20, 24, 30, 40, 60} {
		g := newSizedGame(t, viewW, 9)
		g.Score, g.CoinCount, g.Time, g.Lives = 123456, 7, 299, 3
		f := RenderPixels(g, testPal)
		lastText := -1
		for x := f.W - 1; x >= 0; x-- {
			if f.At(x, 2) != testPal.HUDBG { // text row inside the HUD band
				lastText = x
				break
			}
		}
		if lastText >= f.W-1 {
			t.Errorf("viewW=%d: HUD text runs to column %d of %d (overflow)", viewW, lastText, f.W)
		}
		// The score must always be visible, even at minimum width.
		if lastText < 0 {
			t.Errorf("viewW=%d: HUD text vanished entirely", viewW)
		}
	}
}

func TestStatusFitsEveryViewportWidth(t *testing.T) {
	for _, viewW := range []int{16, 20, 24, 30, 60} {
		g := newSizedGame(t, viewW, 9)
		f := RenderPixels(g, testPal)
		y := f.H - StatusBandPx + 1
		last := -1
		for x := f.W - 1; x >= 0; x-- {
			if f.At(x, y) != testPal.StatusBG {
				last = x
				break
			}
		}
		if last > f.W-1 {
			t.Errorf("viewW=%d: status text overflows (%d >= %d)", viewW, last, f.W)
		}
	}
}

func TestTitleCastStandsOnGroundLine(t *testing.T) {
	for _, viewH := range []int{5, 7, 9, 12, 15} {
		b := engine.NewBuilder(80, engine.LevelHeight)
		b.Ground(0, 79)
		b.Set(2, 12, 'M')
		b.Flag(70)
		l, err := engine.ParseLevel("t", b.Rows())
		if err != nil {
			t.Fatalf("ParseLevel: %v", err)
		}
		g := engine.NewGame([]*engine.Level{l}, 30, viewH)
		f := RenderPixels(g, testPal)

		// Ground = last two tile rows of the VISIBLE world area (camera
		// bottom-clamped), below the HUD band.
		groundTop := HudBandPx + viewTilesOf(g)*Pix - 2*Pix
		cx := f.W / 2
		// Find the mario sprite's lowest pixel (shoes are Dark, not red).
		isMario := func(c Color) bool {
			return c == testPal.Player || c == testPal.Overall ||
				c == testPal.Skin || c == testPal.Dark
		}
		marioBottom := -1
		for y := groundTop - 1; y >= 0 && marioBottom < 0; y-- {
			for x := cx - 20; x < cx; x++ {
				if isMario(f.At(x, y)) {
					marioBottom = y
					break
				}
			}
		}
		if marioBottom < 0 {
			t.Fatalf("viewH=%d: mario sprite not found", viewH)
		}
		if marioBottom != groundTop-1 {
			t.Errorf("viewH=%d: mario feet at row %d, want %d (on the ground line)",
				viewH, marioBottom, groundTop-1)
		}
		// Goomba feet on the same line.
		isGoomba := func(c Color) bool {
			return c == testPal.Goomba || c == testPal.Dark || c == testPal.White
		}
		goombaBottom := -1
		for y := groundTop - 1; y >= 0 && goombaBottom < 0; y-- {
			for x := cx; x < cx+20; x++ {
				if isGoomba(f.At(x, y)) {
					goombaBottom = y
					break
				}
			}
		}
		if goombaBottom != groundTop-1 {
			t.Errorf("viewH=%d: goomba feet at row %d, want %d", viewH, goombaBottom, groundTop-1)
		}
		// No sprite pixels inside the ground rows.
		for y := groundTop; y < f.H; y++ {
			for x := 0; x < f.W; x++ {
				c := f.At(x, y)
				if c == testPal.Player || c == testPal.Overall || c == testPal.Skin ||
					c == testPal.Goomba || c == testPal.White {
					t.Errorf("viewH=%d: sprite pixel at (%d,%d) inside ground", viewH, x, y)
				}
			}
		}
	}
}

func TestTitleTextNeverOverlapsCast(t *testing.T) {
	for _, viewH := range []int{9, 12, 15} {
		b := engine.NewBuilder(80, engine.LevelHeight)
		b.Ground(0, 79)
		b.Flag(70)
		l, _ := engine.ParseLevel("t", b.Rows())
		g := engine.NewGame([]*engine.Level{l}, 30, viewH)
		f := RenderPixels(g, testPal)
		groundTop := f.H - 2*Pix
		// Cast band: the rows the ×2 title cast sprites occupy.
		castTop := groundTop - 2*sprH(sprMarioSmall)
		// The logo (FlagRed, scale 2) must end above the cast band.
		logoBottom := -1
		for y := castTop - 1; y >= 0; y-- {
			for x := 0; x < f.W; x++ {
				if f.At(x, y) == testPal.FlagRed {
					logoBottom = y
					break
				}
			}
			if logoBottom >= 0 {
				break
			}
		}
		if logoBottom >= 0 && logoBottom >= castTop-1 {
			t.Errorf("viewH=%d: logo bottom row %d overlaps cast band top %d",
				viewH, logoBottom, castTop)
		}
	}
}
