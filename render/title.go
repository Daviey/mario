package render

import (
	"fmt"

	"github.com/Daviey/mario/engine"
)

// drawOverlayPx paints state overlays; titleEls carries the frame's
// title text elements when the caller already computed them (nil: the
// overlay computes them itself).
func drawOverlayPx(f *Frame, g *engine.Game, p *Palette, titleEls []titleText) {
	mid := f.H / 2
	switch {
	case g.Paused:
		drawBannerPx(f, mid-4, "PAUSED", p.OverlayFG, p.OverlayBG, p)
		drawCenterPx(f, mid+6, "P RESUME", p.White, 1)
		drawCenterPx(f, mid+12, "R RESTART  Q QUIT", p.White, 1)
	case g.HurryT > 0 && g.Tick%20 < 12:
		drawBannerPx(f, 4, "HURRY!", p.White, p.FlagRed, p)
	case g.State == engine.StateWorldCard:
		drawWorldCard(f, g, p)
	case g.State == engine.StateTitle, g.Demo:
		drawTitlePx(f, g, p, true, titleEls)
	case g.State == engine.StateGameOver:
		drawBannerPx(f, mid-4, "GAME OVER", p.OverlayFG, p.OverlayBG, p)
		drawCenterPx(f, mid+4, "PRESS R TO RESTART", p.White, 1)
	case g.State == engine.StateWin:
		drawBannerPx(f, mid-4, "YOU WIN!", p.OverlayFG, p.OverlayBG, p)
		drawCenterPx(f, mid+4, "PRESS R TO RESTART", p.White, 1)
	}
}

// drawTitlePx draws the title screen: mario vs goomba cast on the ground,
// and in full mode the logo, subtitle, "press any key" blink and the
// leaderboard hint above/below the cast. With full=false only the cast is
// drawn — the leaderboard board takes the rest of the screen.
func drawTitlePx(f *Frame, g *engine.Game, p *Palette, full bool, els []titleText) {
	rc2 := runeColors(p)
	// Bottom-anchored cascade: the cast stands ON the ground line and
	// every text element stacks above it, so no viewport height can
	// overlap or bury anything. Ground occupies the last 2 tile rows.
	castY := titleCastY(f) // mario sprite top; feet on the last sky row
	if full {
		if els == nil {
			els = titleTextEls(f, g)
		}
		for _, e := range els {
			if e.blink && g.Tick%40 >= 28 {
				continue
			}
			if e.shadow { // only the ground-level lines need it; over sky the
				// shadow fills the letters' counters and wrecks legibility
				drawCenterShadowPx(f, e.y, e.s, e.ink.color(p), e.scale, p.Dark)
			} else {
				drawCenterPx(f, e.y, e.s, e.ink.color(p), e.scale)
			}
		}
	}
	// Mario and the goomba flank the centre, facing each other.
	cx := f.W / 2
	// The cast walks in place: mario cycles his gait, the goomba waddles.
	mario := marioSmallWalk[(g.Tick/10)%len(marioSmallWalk)]
	goomba := sprGoomba
	if (g.Tick/10)%2 == 1 {
		goomba = sprGoombaWalk
	}
	f.DrawSprite(mario, rc2, cx-2*sprW(mario)-6, castY, false, 2)
	f.DrawSprite(goomba, rc2, cx+7, castY+2*(sprH(mario)-sprH(goomba)), true, 2)

}

// titleInk picks a title text color from the palette.
type titleInk uint8

const (
	inkFlag titleInk = iota // MARIO logo red
	inkWhite
	inkGold
	inkDim // build version tag under the subtitle
)

func (k titleInk) color(p *Palette) Color {
	switch k {
	case inkFlag:
		return p.FlagRed
	case inkGold:
		return p.GoldLight
	case inkDim:
		return p.TextDim
	default:
		return p.White
	}
}

// titleText is one full-mode title screen line.
type titleText struct {
	s      string
	y      int
	scale  int
	ink    titleInk
	blink  bool // on-phase of the blink cycle only
	shadow bool // 1px drop shadow: ground-level lines only
}

// titleTextEls is the single source of truth for full-mode title text:
// every line with its pixel row, scale and ink. drawTitlePx stamps the
// elements; titleTextBands turns them into the keep-clear rects for the
// title cloud filter in drawDecorations.
func titleTextEls(f *Frame, g *engine.Game) []titleText {
	castY := titleCastY(f)
	castH := 2 * sprH(sprMarioSmall)
	var els []titleText
	add := func(s string, y, scale int, ink titleInk, blink, shadow bool) {
		els = append(els, titleText{s: s, y: y, scale: scale, ink: ink, blink: blink, shadow: shadow})
	}
	if castY >= 13 { // room for the ×2 logo (10px) above the cast?
		logoY := max(2, min(f.H/12, castY-10))
		add("MARIO", logoY, 2, inkFlag, false, false)
		if subY := logoY + 15; subY+5 <= castY { // clear gap below the logo
			add(pickTextPx([]string{"SUPER CLI EDITION", "SUPER CLI"}, f.W-2), subY, 1, inkWhite, false, false)
			tailY := subY + 5 // bottom of the sky stack so far
			if vc := versionCandidates(Version); len(vc) > 0 {
				if v := pickTextPx(vc, f.W-2); v != "" {
					if verY := subY + 7; verY+5 <= castY { // build tag under the subtitle
						add(v, verY, 1, inkDim, false, false)
						tailY = verY + 5
					}
				}
			}
			// About banner: fan-game disclaimer under the version tag.
			// Needs ~12 visible tiles of height, so the terminal status
			// line carries the full disclaimer on every viewport instead
			// (drawStatus swaps it in at the title).
			if fanY := tailY + 2; fanY+5 <= castY {
				add(pickTextPx([]string{"UNOFFICIAL FAN GAME", "FAN GAME"}, f.W-2), fanY, 1, inkWhite, false, false)
				if nY := fanY + 6; nY+5 <= castY {
					add(pickTextPx([]string{"NOT AFFILIATED WITH NINTENDO", "NO NINTENDO AFFILIATION", "NOT NINTENDO"}, f.W-2), nY, 1, inkDim, false, false)
				}
			}
		}
	}
	pressY := min(castY+castH+1, f.H-5) // ground band: first line under the cast
	add(pickTextPx([]string{"PRESS ANY KEY", "ANY KEY"}, f.W-2), pressY, 1, inkGold, true, true)
	if hintY := pressY + 6; hintY+5 <= f.H { // second ground-band line, flush bottom
		add(pickTextPx([]string{"L BOARD D DAILY I ABOUT", "L BOARD I ABOUT", "L/D BOARD DAILY", "L DAILY"}, f.W-2), hintY, 1, inkWhite, false, true)
		if best := g.Best; best > 0 {
			if bestY := hintY + 6; bestY+5 <= f.H {
				add(fmt.Sprintf("BEST %06d", best), bestY, 1, inkGold, false, true)
			}
		}
	}
	return els
}

// titleCastY is the top of the ×2-scaled title cast; its feet stand on
// the ground line (last 2 tile rows are ground).
func titleCastY(f *Frame) int {
	return f.H - 2*Pix - 2*sprH(sprMarioSmall)
}

// titleTextBands returns the pixel rects covered by full-mode title text.
// Blinking lines are always included so the cloud layout stays steady
// instead of strobing with the blink cycle.
func titleTextBands(f *Frame, g *engine.Game) [][4]int {
	return bandsFromEls(f, titleTextEls(f, g))
}

// bandsFromEls is the pixel rects covered by the given title elements —
// the hot path passes the same elements the painter stamps so the layout
// is computed once per frame.
func bandsFromEls(f *Frame, els []titleText) [][4]int {
	var bands [][4]int
	for _, e := range els {
		w := textWidthPx(e.s, e.scale)
		x := (f.W - w) / 2
		if x < 0 {
			x = 0
		}
		h := 5 * e.scale
		bands = append(bands, [4]int{x, e.y, min(x+w, f.W), min(e.y+h, f.H)})
	}
	return bands
}

// cloudHitsBand reports whether the cloud rect (x0,y0)-(x1,y1) intersects
// any keep-clear band. An empty band list never matches.
func cloudHitsBand(x0, y0, x1, y1 int, bands [][4]int) bool {
	for _, b := range bands {
		if x0 < b[2] && x1 > b[0] && y0 < b[3] && y1 > b[1] {
			return true
		}
	}
	return false
}
