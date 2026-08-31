package render

import (
	"fmt"
	"sync"
	"unicode/utf8"

	"github.com/Daviey/mario/engine"
)

// drawStatus paints the bottom real-text line: the fan-game about banner
// while the title screen is up (the one surface every viewport height
// shows — the in-frame arcade banner needs ~12 visible tiles), the
// controls everywhere else.
func drawStatus(s *Screen, g *engine.Game, p *Palette) {
	y := s.H - 1
	for x := range s.W {
		s.SetStyled(x, y, ' ', p.TextDim, p.StatusBG, false)
	}
	s.Center(y, statusText(s.W-2, g), p.TextDim, p.StatusBG, false)
}

// statusLadder is the single source of status-line content: the
// fan-game about banner while the title screen is up (the one surface
// every viewport height shows — the in-frame arcade banner needs ~12
// visible tiles), the controls everywhere else. Candidates run richest
// → shortest; the empty tail lets narrow surfaces drop the line.
//
// Both surfaces draw this ladder — the terminal status line (drawStatus,
// picked by terminal columns) and the canvas status band (drawStatusPx,
// picked by pixels) — so their content can never drift apart. The 3×5
// font cuts only uppercase glyphs, so the pixel path rides
// drawTextPx's upper-case fallback; separators are '-' because the font
// has no '·' glyph.
func statusLadder(g *engine.Game) []string {
	if g.State == engine.StateTitle {
		return []string{
			"unofficial fan game - not affiliated with nintendo - runs in your terminal",
			"unofficial fan game - not affiliated with nintendo",
			"unofficial fan game - not nintendo",
			"fan game",
		}
	}
	return []string{
		"a/d move - w/space jump - s duck - x run - p pause - k die - r restart - q quit",
		"a/d move - w/space jump - x run - p pause - k die - q quit",
		"a/d move - w/space jump - x run - p pause - q quit",
		"a/d move - w/space jump - x run",
		"a/d move - w jump - x run",
		"q quit",
		"",
	}
}

// statusText picks the status line for a screen maxCols wide: about
// banner at the title (never dropped — "fan game" always fits the
// narrowest viewport), controls in play.
func statusText(maxCols int, g *engine.Game) string {
	if g.State == engine.StateTitle {
		if t := pickText(statusLadder(g), maxCols); t != "" {
			return t
		}
		return "fan game"
	}
	return pickText(statusLadder(g), maxCols)
}

// pickText returns the first candidate that fits maxCols terminal columns,
// preferring earlier (richer) entries; "" when nothing fits (callers
// substitute their own floor or drop the line — pickTextPx, by
// contrast, never returns empty).
func pickText(candidates []string, maxCols int) string {
	for _, c := range candidates {
		if utf8.RuneCountInString(c) <= maxCols {
			return c
		}
	}
	return ""
}

// hudInk selects a HUD segment's color on either surface.
type hudInk uint8

const (
	hudPlain hudInk = iota // the HUD text color
	hudRed                 // the CHEATS tag
	hudFlash               // TIME: steady text until HURRY, then flashing red
)

// hudSeg is one segment of the HUD line.
type hudSeg struct {
	s   string
	ink hudInk
}

// hudLadder builds every HUD content variant, richest first — the test
// and parity surface. The hot paths use hudPick/hudPickPx, which walk
// the same variants richest-first and stop at the first that fits.
func hudLadder(g *engine.Game) [][]hudSeg {
	return [][]hudSeg{hudSegs(g, 0), hudSegs(g, 1), hudSegs(g, 2), hudSegs(g, 3)}
}

// hudKey is everything a HUD variant's formatted strings depend on;
// the memo below rebuilds a variant only when one of these changes.
type hudKey struct {
	score int
	coins int
	world string
	time  int
	lives int
	cheat bool
}

// hudMemo caches one built variant per slot: the HUD renders on both
// surfaces every frame, and without this every frame re-formats the
// same handful of strings. The slices are shared — callers must treat
// them as read-only.
var (
	hudMemoMu sync.Mutex
	hudMemo   [4]struct {
		key  hudKey
		segs []hudSeg // nil until first built for this key
	}
)

// hudSegs builds one HUD content variant: 0 richest (SCORE, COINS,
// WORLD, TIME, LIVES, CHEATS), 1 drops WORLD, 2 drops the labels, 3 is
// the bare score (always drawn). The terminal HUD (drawHUD, widths in
// columns) and the canvas HUD band (drawHudPx, widths in pixels) render
// the same variants, so the two surfaces cannot drift apart.
func hudSegs(g *engine.Game, variant int) []hudSeg {
	k := hudKey{score: g.Score, coins: g.CoinCount, world: g.LevelName(),
		time: g.Time, lives: g.Lives, cheat: g.Cheats}
	hudMemoMu.Lock()
	e := &hudMemo[variant]
	if e.segs == nil || e.key != k {
		e.key = k
		e.segs = formatHudSegs(g, variant)
	}
	segs := e.segs
	hudMemoMu.Unlock()
	return segs
}

// formatHudSegs is the uncached builder behind hudSegs.
func formatHudSegs(g *engine.Game, variant int) []hudSeg {
	seg := func(s string, ink hudInk) hudSeg { return hudSeg{s: s, ink: ink} }
	switch variant {
	case 1:
		segs := []hudSeg{
			seg(fmt.Sprintf("SCORE %06d", g.Score), hudPlain),
			seg(fmt.Sprintf("COINS x%02d", g.CoinCount), hudPlain),
			seg(fmt.Sprintf("TIME %03d", g.Time), hudFlash),
			seg(fmt.Sprintf("LIVES x%d", g.Lives), hudPlain),
		}
		if g.Cheats {
			segs = append(segs, seg("CHEATS", hudRed))
		}
		return segs
	case 2:
		return []hudSeg{
			seg(fmt.Sprintf("%06d", g.Score), hudPlain),
			seg(fmt.Sprintf("x%02d", g.CoinCount), hudPlain),
			seg(fmt.Sprintf("%03d", g.Time), hudFlash),
			seg(fmt.Sprintf("x%d", g.Lives), hudPlain),
		}
	case 3:
		return []hudSeg{seg(fmt.Sprintf("%06d", g.Score), hudPlain)}
	}
	segs := []hudSeg{
		seg(fmt.Sprintf("SCORE %06d", g.Score), hudPlain),
		seg(fmt.Sprintf("COINS x%02d", g.CoinCount), hudPlain),
		seg(fmt.Sprintf("WORLD %s", g.LevelName()), hudPlain),
		seg(fmt.Sprintf("TIME %03d", g.Time), hudFlash),
		seg(fmt.Sprintf("LIVES x%d", g.Lives), hudPlain),
	}
	if g.Cheats {
		segs = append(segs, seg("CHEATS", hudRed))
	}
	return segs
}

// hurryFlash reports whether the TIME readout is in its red phase this
// tick: flashing while the HURRY! banner cycles, steady red once it has
// come and gone.
func hurryFlash(g *engine.Game) bool {
	return g.Hurry && (g.HurryT <= 0 || g.Tick%30 < 18)
}

// hudSegColor resolves a segment's ink for the current tick.
func hudSegColor(seg hudSeg, g *engine.Game, p *Palette) Color {
	switch seg.ink {
	case hudRed:
		return p.FlagRed
	case hudFlash:
		if hurryFlash(g) {
			return p.FlagRed
		}
	}
	return p.Text
}

// hudWidth is a variant's width in terminal columns (segments joined by
// two spaces).
func hudWidth(segs []hudSeg) int {
	w := 0
	for i, seg := range segs {
		w += utf8.RuneCountInString(seg.s)
		if i < len(segs)-1 {
			w += 2
		}
	}
	return w
}

// hudWidthPx is a variant's width in 3×5-font pixels at scale 1.
func hudWidthPx(segs []hudSeg) int {
	w := 0
	for i, seg := range segs {
		w += textWidthPx(seg.s, 1)
		if i < len(segs)-1 {
			w += 8 // the two-space gap
		}
	}
	return w
}

// hudPick returns the richest variant that fits maxCols terminal
// columns, walking the variants richest-first and falling back to the
// bare score. The walk formats nothing on steady frames — hudSegs
// memoizes per game state.
func hudPick(g *engine.Game, maxCols int) []hudSeg {
	var last []hudSeg
	for i := range 4 {
		last = hudSegs(g, i)
		if hudWidth(last) <= maxCols {
			return last
		}
	}
	return last
}

// hudPickPx is hudPick for the pixel font, measuring in pixels.
func hudPickPx(g *engine.Game, maxPx int) []hudSeg {
	var last []hudSeg
	for i := range 4 {
		last = hudSegs(g, i)
		if hudWidthPx(last) <= maxPx {
			return last
		}
	}
	return last
}

func drawHUD(s *Screen, g *engine.Game, p *Palette) {
	for x := range s.W {
		s.SetStyled(x, 0, ' ', p.Text, p.HUDBG, false)
	}
	x := 1
	for _, seg := range hudPick(g, s.W-2) {
		s.TextStyled(x, 0, seg.s, hudSegColor(seg, g, p), p.HUDBG, true)
		x += utf8.RuneCountInString(seg.s) + 2
	}
}
