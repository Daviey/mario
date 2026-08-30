// Package render converts game state into a colored terminal screen.
//
// The world is drawn on a square-pixel grid (Pix×Pix pixels per tile) and
// packed into terminal cells two pixels at a time with the half block '▀'
// (fg = upper pixel, bg = lower pixel) — the finest full-color grid a
// terminal can express. Rendering is a pure function of engine state: no
// I/O, no clocks, no randomness.
package render

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/Daviey/mario/engine"
)

// RGB is a 24-bit color (0xRRGGBB).
type RGB uint32

// Color pairs a truecolor value with its fallback indexes for lesser
// terminals: Idx256 points into the fixed xterm cube (16-255, rendered
// exactly by every -256color terminal), ANSI into the base-16 palette
// (0-15, rendered as whatever the user's profile says).
type Color struct {
	RGB    RGB
	Idx256 int
	ANSI   int
}

func color(hex uint32, ansi int) Color {
	return Color{RGB: RGB(hex), Idx256: nearest256(RGB(hex)), ANSI: ansi}
}

// Color depths: what a Screen serializes (see NewPalette).
const (
	Colors16  = 16  // base ANSI palette, SGR 30-37/90-97
	Colors256 = 256 // fixed xterm cube, SGR 38;5 — exact on every -256color terminal
	Colors24  = 24  // truecolor, SGR 38;2
)

// Palette holds every color the renderer uses.
type Palette struct {
	Colors int // Colors24, Colors256 or Colors16 — the SGR encoding this palette emits

	Sky          Color // classic SMB sky blue
	GroundLight  Color // sunlit top edge of terrain
	GroundMid    Color
	GroundDark   Color // deep earth / seams
	BrickLight   Color
	BrickDark    Color // mortar shadow
	QuestionBG   Color
	QuestionDim  Color // flash partner of QuestionBG; the mark never dims
	QuestionHi   Color
	QuestionFG   Color
	QuestionMark Color
	UsedBG       Color
	PipeLight    Color // lit side of pipes
	PipeMid      Color
	PipeDark     Color // shaded side / interior
	Pole         Color
	FlagRed      Color
	Coin         Color
	GoldLight    Color
	Player       Color
	Skin         Color
	Overall      Color
	Dark         Color // eyes, shoes, outlines
	Goomba       Color
	GoombaFlat   Color
	Green        Color
	GreenLight   Color
	GreenDark    Color
	KoopaSkin    Color
	Mushroom     Color
	Cloud        Color
	Door         Color
	Window       Color
	Text         Color
	TextDim      Color
	HUDBG        Color
	StatusBG     Color
	OverlayBG    Color
	OverlayFG    Color
	White        Color
}

// TrueColorTerm reports whether a TERM value names a terminal that
// renders 24-bit color sequences without any COLORTERM hint. Hosts that
// only ever see TERM (mosh overwrites the child's TERM to xterm-256color
// and drops COLORTERM entirely) use this on the TERM the client sent at
// ssh time to decide whether the game may run truecolor.
func TrueColorTerm(term string) bool {
	// kitty and Alacritty name themselves in TERM and are always
	// 24-bit; like ghostty/wezterm they need no COLORTERM hint.
	return strings.Contains(term, "truecolor") ||
		strings.Contains(term, "direct") ||
		term == "ghostty" || term == "xterm-ghostty" || term == "wezterm" ||
		term == "kitty" || term == "xterm-kitty" || term == "alacritty"
}

// ColorDepthFor picks the SGR color mode for a terminal described only
// by its TERM (and a COLORTERM the environment carried, if any):
// 24-bit when the terminal is known to render it (TrueColorTerm
// families or a COLORTERM hint), otherwise the fixed xterm cube
// whenever TERM advertises 256 colors — the convention every
// -256color terminal and mosh's cell model honors — and base-16 only
// for terminals that claim neither. Shared decision rule for the
// native runner, the SSH host's sessions and the mosh child alike.
func ColorDepthFor(term, colorterm string) int {
	if colorterm != "" || TrueColorTerm(term) {
		return Colors24
	}
	if strings.Contains(term, "256") {
		return Colors256
	}
	return Colors16
}

// NewPalette returns the game palette. colors picks the SGR encoding:
// Colors24 emits 24-bit sequences, Colors256 the fixed xterm cube
// (38;5), Colors16 the base ANSI palette.
func NewPalette(colors int) *Palette {
	return &Palette{
		Colors:       colors,
		Sky:          color(0x5C94FC, 12),
		GroundLight:  color(0xF8B060, 11),
		GroundMid:    color(0xC84C0C, 1),
		GroundDark:   color(0x7C2800, 1),
		BrickLight:   color(0xD86818, 3), // 208%16 == 0 once slipped through here: black bricks in 16-color mode
		BrickDark:    color(0x6B2B00, 1),
		QuestionBG:   color(0xFC9838, 11),
		QuestionDim:  color(0xE08018, 3),
		QuestionHi:   color(0xFFD9A0, 11),
		QuestionFG:   color(0x4A1C00, 0),
		QuestionMark: color(0xFFF8E0, 15),
		PipeLight:    color(0x80D010, 10),
		PipeMid:      color(0x00A800, 2),
		PipeDark:     color(0x004400, 2),
		Pole:         color(0x98E858, 10),
		FlagRed:      color(0xE4221B, 9),
		Coin:         color(0xFCD000, 11),
		GoldLight:    color(0xFFF3B0, 11),
		Player:       color(0xFF3B30, 9),
		Skin:         color(0xFFC89E, 6),
		UsedBG:       color(0x9C5A24, 3),
		Overall:      color(0x2B5DD7, 12),
		Dark:         color(0x1A0E04, 0),
		Goomba:       color(0xC86428, 3),
		GoombaFlat:   color(0x8B4513, 3),
		Green:        color(0x00A800, 2),
		GreenLight:   color(0x80D010, 10),
		GreenDark:    color(0x004000, 2),
		KoopaSkin:    color(0xF8D878, 3),
		Mushroom:     color(0xFF9F0A, 11),
		Cloud:        color(0xFFFFFF, 15),
		Door:         color(0x2B1A0A, 0),
		Window:       color(0x101820, 0),
		Text:         color(0xFFFFFF, 15),
		TextDim:      color(0x9FB6D9, 7),
		HUDBG:        color(0x0A1C6E, 4),
		StatusBG:     color(0x071330, 4),
		OverlayBG:    color(0xFFF8E7, 15),
		OverlayFG:    color(0x101010, 0),
		White:        color(0xFFFFFF, 15),
	}
}

// underground re-skins the palette for the underground world: black sky,
// blue ground and bricks — the classic 1-2 look.
func underground(p *Palette) *Palette {
	q := *p
	q.Sky = color(0x000000, 0)
	q.GroundLight = color(0x8888D8, 12)
	q.GroundMid = color(0x3850C8, 4)
	q.GroundDark = color(0x1A2A70, 4)
	q.BrickLight = color(0x5878E8, 12)
	q.BrickDark = color(0x2030A0, 4)
	q.Cloud = color(0x2A2A4A, 4)
	return &q
}

// skyTheme re-skins the athletic world: pale sky, sandstone terrain.
func skyTheme(p *Palette) *Palette {
	q := *p
	q.Sky = color(0x88C8F8, 12)
	q.GroundLight = color(0xF8F0D0, 15)
	q.GroundMid = color(0xD8B070, 11)
	q.GroundDark = color(0x8A6230, 3)
	q.BrickLight = color(0xE8C890, 11)
	q.BrickDark = color(0x8A6230, 3)
	return &q
}

// castleTheme re-skins the finale: black sky, grey stone, dead air.
func castleTheme(p *Palette) *Palette {
	q := *p
	q.Sky = color(0x000000, 0)
	q.GroundLight = color(0xC8C8C8, 7)
	q.GroundMid = color(0x909098, 7)
	q.GroundDark = color(0x484850, 8)
	q.BrickLight = color(0xA8A8B0, 7)
	q.BrickDark = color(0x404048, 8)
	q.Cloud = color(0x2A2A2A, 8)
	return &q
}

// themedCache memoizes themed palettes per (base palette, theme): the
// theme used to be rebuilt — a full Palette copy — every frame. Values
// are deterministic; a process draws with a handful of palettes.
var themedCache sync.Map // themeKey -> *Palette

type themeKey struct {
	p     *Palette
	theme engine.Theme
}

// paletteFor returns the palette for the current level's theme, built
// once per (base palette, theme).
func paletteFor(g *engine.Game, p *Palette) *Palette {
	if g.Level == nil {
		return p
	}
	var build func(*Palette) *Palette
	switch g.Level.Theme {
	case engine.ThemeUnderground:
		build = underground
	case engine.ThemeSky:
		build = skyTheme
	case engine.ThemeCastle:
		build = castleTheme
	default:
		return p
	}
	k := themeKey{p, g.Level.Theme}
	if v, ok := themedCache.Load(k); ok {
		return v.(*Palette)
	}
	q := build(p)
	themedCache.Store(k, q)
	return q
}

// runeColors maps sprite-art runes to palette swatches. Cached per
// palette — callers must treat the map as read-only (fire/star variants
// are separately cached, never built by mutating this one).
var runeColorsCache sync.Map // *Palette -> map[rune]Color

func runeColors(p *Palette) map[rune]Color {
	if v, ok := runeColorsCache.Load(p); ok {
		return v.(map[rune]Color)
	}
	rc := map[rune]Color{
		'R': p.Player, 'S': p.Skin, 'D': p.Dark, 'B': p.Overall,
		'W': p.White, 'C': p.Cloud, 'Y': p.Coin, 'L': p.GoldLight,
		'O': p.GroundMid, 'o': p.GroundLight,
		'G': p.Green, 'E': p.GreenLight, 'g': p.GreenDark,
		'K': p.KoopaSkin, 'n': p.Goomba,
	}
	runeColorsCache.Store(p, rc)
	return rc
}

// Cell is one character position with styling.
type Cell struct {
	Ch   rune
	Fg   Color
	Bg   Color
	Bold bool
}

// Screen is a rectangular cell buffer plus ANSI serialization.
type Screen struct {
	W, H   int
	cells  []Cell
	Colors int // Colors24, Colors256 or Colors16
}

// NewScreen returns a screen with every cell blank black.
func NewScreen(w, h int) *Screen {
	s := &Screen{W: w, H: h, cells: make([]Cell, w*h)}
	blank := Cell{Ch: ' '}
	for i := range s.cells {
		s.cells[i] = blank
	}
	return s
}

// Set writes a foreground-colored cell, clipping out-of-bounds writes.
func (s *Screen) Set(x, y int, ch rune, fg Color) {
	s.SetStyled(x, y, ch, fg, Color{}, false)
}

// SetStyled writes a fully styled cell, clipping out-of-bounds writes.
func (s *Screen) SetStyled(x, y int, ch rune, fg, bg Color, bold bool) {
	if x < 0 || x >= s.W || y < 0 || y >= s.H {
		return
	}
	s.cells[y*s.W+x] = Cell{Ch: ch, Fg: fg, Bg: bg, Bold: bold}
}

// At returns the cell at a position (zero Cell when out of bounds).
func (s *Screen) At(x, y int) Cell {
	if x < 0 || x >= s.W || y < 0 || y >= s.H {
		return Cell{}
	}
	return s.cells[y*s.W+x]
}

// Text writes a string in one color on black. Columns advance one per
// rune: a multibyte rune must not shift every later glyph one column
// per byte.
func (s *Screen) Text(x, y int, text string, fg Color) {
	cx := x
	for _, r := range text {
		s.SetStyled(cx, y, r, fg, Color{}, false)
		cx++
	}
}

// TextStyled writes a styled string.
func (s *Screen) TextStyled(x, y int, text string, fg, bg Color, bold bool) {
	cx := x
	for _, r := range text {
		s.SetStyled(cx, y, r, fg, bg, bold)
		cx++
	}
}

// Center writes text centered on row y.
func (s *Screen) Center(y int, text string, fg, bg Color, bold bool) {
	x := (s.W - len([]rune(text))) / 2
	if x < 0 {
		x = 0
	}
	s.TextStyled(x, y, text, fg, bg, bold)
}

// RowString returns the runes of one row (test helper).
func (s *Screen) RowString(y int) string {
	if y < 0 || y >= s.H {
		return ""
	}
	row := make([]rune, s.W)
	for x := 0; x < s.W; x++ {
		row[x] = s.cells[y*s.W+x].Ch
	}
	return string(row)
}

// String serializes the screen as one ANSI frame: cursor home, every cell
// with only the SGR parameters that changed since the previous cell, reset
// at the end. Style state deliberately persists across row breaks — the
// terminal keeps SGR across line feeds, so starting a row in the style the
// previous row ended in costs nothing.
func (s *Screen) String() string {
	var b strings.Builder
	b.WriteString("\x1b[H")
	st := sgrState{mode: s.Colors}
	for y := range s.H {
		for x := range s.W {
			c := s.cells[y*s.W+x]
			st.transition(&b, c)
			b.WriteRune(c.Ch)
		}
		if y < s.H-1 {
			b.WriteString("\r\n")
		}
	}
	b.WriteString("\x1b[0m")
	return b.String()
}

// sgrState mirrors the terminal's current SGR state so successive cells,
// runs and rows emit only the SGR parameters that actually changed — style
// sequences dominate a frame's byte cost, so this is where the SSH
// bandwidth goes. A space paints nothing but its background, so its
// foreground is a don't-care and never costs bytes.
type sgrState struct {
	mode int  // Colors24/256/16 — which SGR encoding the terminal takes
	have bool // any style emitted yet (terminal starts at default)
	fg   Color
	bg   Color
	bold bool
}

func isDefaultColor(c Color) bool { return c.RGB == 0 && c.ANSI == 0 }

// colorParams returns the SGR parameters that set c as foreground
// (bg=false) or background in the given color mode. Every mode is
// table-driven so the per-changed-cell wire path never formats a string:
// basic mode reads basicSGR (every ANSI-16 code, computed once at init),
// 24-bit and 256-cube modes read sgrMem (memoized per Color the first
// time it is emitted — in practice at palette construction, once per
// process).
func colorParams(mode int, c Color, bg bool) string {
	switch mode {
	case Colors24, Colors256:
		if s, ok := sgrMem.Load(sgrKey{c: c, mode: mode, bg: bg}); ok {
			return s.(string)
		}
		base := 38
		if bg {
			base = 48
		}
		var str string
		if mode == Colors24 {
			str = fmt.Sprintf("%d;2;%d;%d;%d", base, c.RGB>>16, (c.RGB>>8)&0xFF, c.RGB&0xFF)
		} else {
			str = fmt.Sprintf("%d;5;%d", base, c.Idx256)
		}
		sgrMem.Store(sgrKey{c: c, mode: mode, bg: bg}, str)
		return str
	}
	i := c.ANSI
	if i < 0 || i > 15 {
		i = 0
	}
	return basicSGR[i][b2i(bg)]
}

// basicSGR holds every ANSI-16 SGR parameter string (fg and bg) so basic
// mode is a plain array read.
var basicSGR [16][2]string

func init() {
	for i := range 16 {
		basicSGR[i][0] = ansiCode(i, false)
		basicSGR[i][1] = ansiCode(i, true)
	}
}

// sgrMem memoizes 24-bit and 256-cube SGR parameter strings per
// (Color, mode, role) — deterministic values, written once per unique
// combination.
var sgrMem sync.Map // sgrKey -> string

type sgrKey struct {
	c    Color
	mode int
	bg   bool
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// transition appends the minimal SGR sequence that renders cell c and
// updates the tracked state.
func (st *sgrState) transition(b *strings.Builder, c Cell) {
	dBg, dBold := c.Bg, c.Bold
	dFg := c.Fg
	if c.Ch == ' ' {
		dFg = st.fg // invisible on a space: keep whatever is already set
	}
	if st.have && st.sameColor(dFg, st.fg) && st.sameColor(dBg, st.bg) && dBold == st.bold {
		return
	}
	// Attributes that must be cleared (bold off, a color returning to the
	// terminal default) have no cheap additive form: fall back to a full
	// "0;..." reset+set.
	needClear := st.have &&
		((!dBold && st.bold) ||
			(isDefaultColor(dFg) && !isDefaultColor(st.fg)) ||
			(isDefaultColor(dBg) && !isDefaultColor(st.bg)))
	if !st.have || needClear {
		parts := []string{"0"}
		if dBold {
			parts = append(parts, "1")
		}
		if c.Ch != ' ' && !isDefaultColor(c.Fg) {
			parts = append(parts, colorParams(st.mode, c.Fg, false))
		}
		if !isDefaultColor(dBg) {
			parts = append(parts, colorParams(st.mode, dBg, true))
		}
		b.WriteString("\x1b[" + strings.Join(parts, ";") + "m")
		if c.Ch == ' ' {
			dFg = Color{} // the full form omits fg: terminal is at default
		}
	} else {
		var parts []string
		if !st.sameColor(dFg, st.fg) {
			parts = append(parts, colorParams(st.mode, dFg, false))
		}
		if !st.sameColor(dBg, st.bg) {
			parts = append(parts, colorParams(st.mode, dBg, true))
		}
		if dBold && !st.bold {
			parts = append(parts, "1")
		}
		b.WriteString("\x1b[" + strings.Join(parts, ";") + "m")
	}
	st.fg, st.bg, st.bold, st.have = dFg, dBg, dBold, true
}

// sameColor reports whether two colors are indistinguishable on the wire
// in this color mode — the comparison must match what is actually
// emitted. Basic mode sends only the ANSI index, so same-index colors
// (GroundMid/GroundDark both 1, PipeDark/GreenDark both 2) are the same
// terminal color and must not re-emit SGR every frame; 256-cube mode
// sends only the cube index; truecolor compares the full pair. The
// terminal default is its own wire state (no SGR at all), so a default
// color never compares equal to a real one even when their indices match.
func (st *sgrState) sameColor(a, b Color) bool {
	da, db := isDefaultColor(a), isDefaultColor(b)
	if da || db {
		return da == db
	}
	switch st.mode {
	case Colors24:
		return a == b
	case Colors256:
		return a.Idx256 == b.Idx256
	}
	return a.ANSI == b.ANSI
}

func ansiCode(idx int, bg bool) string {
	base := 30
	if bg {
		base = 40
	}
	if idx >= 8 {
		base += 60
		idx -= 8
	}
	return fmt.Sprintf("%d", base+idx)
}

// determinstic column hash for sky decorations (no RNG state).
func hashX(x int) uint32 {
	h := uint32(x) * 2654435761
	h ^= h >> 13
	h *= 0x5BD1E995
	h ^= h >> 15
	return h
}

// CloudAt reports a cloud anchored at world column tx: its sky row and
// width in columns. Clouds appear roughly every 9 columns.
func CloudAt(tx int) (row, width int, ok bool) {
	h := hashX(tx)
	if h%9 != 0 {
		return 0, 0, false
	}
	return 4 + int(h>>8)%7, 2 + int(h>>12)%2, true
}

// castleRect is the goal castle's tile footprint: anchored 3 tiles right
// of the flag pole, 5 wide, on rows 9..12. Cloud avoidance and the
// castle painter both derive from this one geometry so they can never
// drift apart.
func castleRect(g *engine.Game) (x0, y0, w, h int) {
	return g.Level.FlagX + 3, 9, 5, 4
}

// cloudBlocked reports whether a cloud anchored at tx on sky row row would
// touch solid tiles or the goal castle. Blocked clouds are skipped so they
// never slice behind level geometry — clouds only ever draw on open sky.
func cloudBlocked(g *engine.Game, tx, row int) bool {
	for x := tx; x < tx+3; x++ { // sprCloud spans 12px = 3 tiles
		if g.Level.At(x, row) != engine.Empty {
			return true
		}
	}
	cx, cy, cw, ch := castleRect(g)
	if row >= cy && row < cy+ch && tx+3 > cx && tx < cx+cw {
		return true
	}
	return false
}

// HillAt reports whether a hill sits at world column tx (every ~13).
func HillAt(tx int) bool { return hashX(tx)%13 == 5 }

// BushAt reports whether a bush is anchored at tx (~ every 7 columns,
// never on a hill column). All bushes draw the same 9×2 sprite, so there
// is no width to report.
func BushAt(tx int) bool {
	h := hashX(tx)
	return h%7 == 3 && !HillAt(tx)
}

// viewTilesOf is the vertical viewport in tiles, derived from the game's
// ViewH so the world fills the available terminal area without changing
// the on-screen scale of any sprite or tile.
func viewTilesOf(g *engine.Game) int {
	vh := g.ViewH
	if vh < 4 {
		vh = 4
	}
	if vh > g.Level.Height {
		vh = g.Level.Height
	}
	return vh
}

// CameraY computes the vertical camera position in tiles: the player is
// kept slightly below center, clamped to the level.
func CameraY(g *engine.Game) float64 {
	vh := viewTilesOf(g)
	p := g.Player
	c := p.Pos.Y + p.H/2 - float64(vh)/2
	if c < 0 {
		return 0
	}
	if m := float64(g.Level.Height - vh); c > m {
		return m
	}
	return c
}

// worldFrame renders the world (sky, decorations, tiles, HUD
// overlays) into a pixel frame of ViewW*Pix x ViewH*Pix pixels.
func worldFrame(g *engine.Game, p *Palette) *Frame {
	p = paletteFor(g, p)
	vh := viewTilesOf(g)
	f := NewFrame(g.ViewW*Pix, vh*Pix, p.Sky)
	rc := runeColors(p)
	camX := g.CameraX
	camY := CameraY(g)
	ox := int(math.Round(camX * Pix))
	oy := int(math.Round(camY * Pix))
	txOf := func(px int) int { return px - ox }
	tyOf := func(py int) int { return py - oy }

	// Title text is laid out once per frame: the cloud keep-clear bands
	// and the title painter consume the same elements.
	var titleEls []titleText
	title := g.State == engine.StateTitle && !g.Demo
	if title {
		titleEls = titleTextEls(f, g)
	}
	drawDecorations(f, g, p, rc, txOf, tyOf, bandsFromEls(f, titleEls))
	if title {
		// Title screen: clean sky, decorations and the ground strip only,
		// so the logo and cast stay unobstructed.
		drawGroundOnly(f, g, p, camX, camY, ox, oy)
		drawOverlayPx(f, g, p, titleEls)
		return f
	}
	drawPlants(f, g, p, rc, camX, camY) // under the pipes: the pipe occludes
	drawCastleAt(f, g, p, txOf, tyOf)
	drawMushrooms(f, g, p, rc, camX, camY)
	drawFlowers(f, g, p, rc, camX, camY)
	drawTilesPx(f, g, p, camX, camY, ox, oy)
	drawCoinItems(f, g, p, rc, camX, camY, ox, oy)
	drawParticlesPx(f, g, p, rc, ox, oy)
	drawEnemiesPx(f, g, p, rc, camX, camY, ox, oy)
	drawFireBars(f, g, p, rc, camX, camY)
	drawFireballs(f, g, p, rc, camX, camY)
	drawPlayerPx(f, g, p, rc, camX, camY)
	drawOverlayPx(f, g, p, nil)
	return f
}

// Render draws one complete frame: HUD row, world pixel grid through the
// camera (with vertical follow), entities, particles and overlays. Screen
// size is ViewW*Pix columns wide and (2+ViewH*Pix/2) rows tall: a fuller
// window shows more world, never bigger sprites. An active ScoreUI
// replaces the world with the leaderboard text screens.
func Render(g *engine.Game, p *Palette, ui ...*ScoreUI) *Screen {
	vh := viewTilesOf(g)
	s := NewScreen(g.ViewW*Pix, 2+vh*Pix/2)
	s.Colors = p.Colors
	drawHUD(s, g, p)
	drawStatus(s, g, p)
	if u := firstUI(ui); u != nil && u.Mode != UIOff {
		drawScoreUIText(s, u, p, g.Tick)
	} else {
		blit(s, worldFrame(g, p))
	}
	return s
}

// FrameANSI renders and serializes in one call.
func FrameANSI(g *engine.Game, p *Palette, ui ...*ScoreUI) string {
	return Render(g, p, ui...).String()
}

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
// preferring earlier (richer) entries.
func pickText(candidates []string, maxCols int) string {
	for _, c := range candidates {
		if utf8.RuneCountInString(c) <= maxCols {
			return c
		}
	}
	return ""
}

// blit packs two pixel rows per screen cell: differing halves ride the
// half block (fg = upper, bg = lower); a solid pair is a plain space over
// the colour — one byte on the wire instead of the block's three, and sky
// and other large fills are almost entirely solid pairs.
func blit(s *Screen, f *Frame) {
	worldRows := f.H / 2
	for cy := range worldRows {
		for x := range min(f.W, s.W) {
			upper := f.At(x, cy*2)
			lower := f.At(x, cy*2+1)
			if upper == lower {
				s.SetStyled(x, 1+cy, ' ', upper, upper, false)
				continue
			}
			s.SetStyled(x, 1+cy, '▀', upper, lower, false)
		}
	}
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
// and parity surface. The hot paths use hudPick/hudPickPx, which build
// only the variant they land on.
func hudLadder(g *engine.Game) [][]hudSeg {
	return [][]hudSeg{hudSegs(g, 0), hudSegs(g, 1), hudSegs(g, 2), hudSegs(g, 3)}
}

// hudSegs builds one HUD content variant: 0 richest (SCORE, COINS,
// WORLD, TIME, LIVES, CHEATS), 1 drops WORLD, 2 drops the labels, 3 is
// the bare score (always drawn). The terminal HUD (drawHUD, widths in
// columns) and the canvas HUD band (drawHudPx, widths in pixels) render
// the same variants, so the two surfaces cannot drift apart.
func hudSegs(g *engine.Game, variant int) []hudSeg {
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
// columns, falling back to the bare score. It builds only the variant
// it lands on — the HUD runs every frame.
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

// drawDecorations paints the sky dressing. bands are the title text's
// keep-clear rects (nil outside the title screen) — the caller computes
// them once per frame from the same title elements the title painter
// stamps.
func drawDecorations(f *Frame, g *engine.Game, p *Palette, rc map[rune]Color,
	txOf, tyOf func(int) int, bands [][4]int) {
	switch g.Level.Theme {
	case engine.ThemeUnderground, engine.ThemeCastle:
		return // no sky dressing underground or inside the castle
	}
	// Only the overworld grows hills and bushes; the sky world keeps its
	// clouds but floats over open air.
	overworld := g.Level.Theme == engine.ThemeOverworld
	for tx := range g.Level.Width {
		if row, _, ok := CloudAt(tx); ok && !cloudBlocked(g, tx, row) {
			x, y := txOf(tx*Pix), tyOf(row*Pix)
			if !cloudHitsBand(x, y, x+sprW(sprCloud), y+sprH(sprCloud), bands) {
				f.DrawSprite(sprCloud, rc, x, y, false, 1)
			}
		}
		if overworld && HillAt(tx) && g.Level.At(tx, engine.GroundTop).Solid() {
			f.DrawSprite(sprHill, rc, txOf(tx*Pix), tyOf(engine.GroundTop*Pix-sprH(sprHill)), false, 1)
		}
		if overworld && BushAt(tx) && g.Level.At(tx, engine.GroundTop).Solid() {
			f.DrawSprite(sprBush, rc, txOf(tx*Pix), tyOf(engine.GroundTop*Pix-sprH(sprBush)), false, 1)
		}
	}
}

func drawCastleAt(f *Frame, g *engine.Game, p *Palette, txOf, tyOf func(int) int) {
	c0, cy, _, _ := castleRect(g)
	x, y := txOf(c0*Pix), tyOf(cy*Pix)
	drawCastle(f, p, x, y)
	drawCastleFlag(f, p, x, y-2, g.CastleFlag)
}

// drawGroundOnly paints just the terrain strip (title-screen backdrop).
func drawGroundOnly(f *Frame, g *engine.Game, p *Palette, camX, camY float64, ox, oy int) {
	vhTiles := viewTilesOf(g)
	for ty := int(math.Floor(camY)) - 1; ty < int(math.Ceil(camY))+vhTiles+1; ty++ {
		if ty < 0 || ty >= g.Level.Height {
			continue
		}
		for tx := int(math.Floor(camX)) - 1; tx <= int(math.Floor(camX))+g.ViewW+1; tx++ {
			if tx < 0 || tx >= g.Level.Width || g.Level.At(tx, ty) != engine.Ground {
				continue
			}
			drawGround(f, p, tx*Pix-ox, ty*Pix-oy, tx, ty,
				g.Level.At(tx, ty-1) != engine.Ground)
		}
	}
}

func drawTilesPx(f *Frame, g *engine.Game, p *Palette, camX, camY float64, ox, oy int) {
	ty0 := int(math.Floor(camY)) - 1
	ty1 := int(math.Ceil(camY)) + viewTilesOf(g) + 1
	for ty := ty0; ty <= ty1; ty++ {
		if ty < 0 || ty >= g.Level.Height {
			continue
		}
		tx0 := int(math.Floor(camX)) - 1
		tx1 := tx0 + g.ViewW + 2
		for tx := tx0; tx <= tx1; tx++ {
			if tx < 0 || tx >= g.Level.Width {
				continue
			}
			t := g.Level.At(tx, ty)
			if t == engine.Empty || t == engine.HiddenCoin || t == engine.HiddenLife {
				continue
			}
			x := tx*Pix - ox
			y := ty*Pix - oy
			if g.BumpActive(tx, ty) {
				y -= Pix / 2 // bump nudges the block up half a tile
			}
			switch t {
			case engine.Ground:
				drawGround(f, p, x, y, tx, ty, g.Level.At(tx, ty-1) != engine.Ground)
			case engine.Brick:
				drawBrick(f, p, x, y, tx)
			case engine.Question, engine.QuestionMush, engine.QuestionStar:
				drawQuestion(f, p, x, y, g.Tick%48 < 24)
			case engine.Used:
				drawUsed(f, p, x, y)
			case engine.Pipe:
				_, col := pipeCol(g, tx, ty)
				drawPipe(f, p, x, y, col, g.Level.At(tx, ty-1) != engine.Pipe)
			case engine.Lava:
				drawLava(f, p, x, y, tx, g.Level.At(tx, ty-1) != engine.Lava, g.Tick)
			case engine.FlagPole:
				drawFlagPole(f, p, x, y)
			case engine.FlagTop:
				drawFlagTop(f, p, x, y, g.FlagDrop)
			}
		}
	}
}

// pipeCol returns the pipe start column and this tile's column within it.
func pipeCol(g *engine.Game, tx, ty int) (start, col int) {
	start = tx
	for start > 0 && g.Level.At(start-1, ty) == engine.Pipe {
		start--
	}
	return start, tx - start
}

func drawCoinItems(f *Frame, g *engine.Game, p *Palette, rc map[rune]Color,
	camX, camY float64, ox, oy int) {
	art := sprCoin
	if (g.Tick/8)%2 == 1 {
		art = sprCoinEdge
	}
	for _, c := range g.CoinItems {
		if c.Gone {
			continue
		}
		cx := int(math.Round((c.Pos.X + engine.CoinSize/2 - camX) * Pix))
		cy := int(math.Round((c.Pos.Y + engine.CoinSize/2 - camY) * Pix))
		f.DrawSprite(art, rc, cx-sprW(art)/2, cy-sprH(art)/2, false, 1)
	}
}

func drawMushrooms(f *Frame, g *engine.Game, p *Palette, rc map[rune]Color, camX, camY float64) {
	for _, m := range g.Mushrooms {
		if m.Gone {
			continue
		}
		cx := int(math.Round((m.Pos.X + engine.MushroomW/2 - camX) * Pix))
		bottom := int(math.Round((m.Pos.Y + engine.MushroomH - camY) * Pix))
		art := sprMushroom
		switch m.Kind {
		case engine.MushLife:
			art = sprMushroom1UP
		case engine.MushStar:
			art = sprStar
		}
		f.DrawSprite(art, rc, cx-sprW(art)/2, bottom-sprH(art), false, 1)
	}
}

func drawParticlesPx(f *Frame, g *engine.Game, p *Palette, rc map[rune]Color, ox, oy int) {
	for _, pt := range g.Particles {
		if pt.Life <= 0 {
			continue
		}
		x := int(math.Round(pt.Pos.X*Pix)) - ox
		y := int(math.Round(pt.Pos.Y*Pix)) - oy
		switch pt.Kind {
		case engine.ParticleCoin:
			f.DrawSprite(sprCoinPop, rc, x, y, false, 1)
		case engine.ParticleDebris:
			f.Set(x, y, p.BrickDark)
			f.Set(x+1, y+1, p.BrickDark)
		case engine.ParticleSparkle:
			f.DrawSprite(sprSparkle, rc, x-1, y-1, false, 1)
		case engine.ParticleDust:
			f.Set(x, y, p.White)
			if pt.Life%2 == 0 {
				f.Set(x+1, y, p.White)
			}
		case engine.ParticleScore:
			txt := strconv.Itoa(pt.Val)
			if pt.Val == 0 {
				txt = "1UP"
			}
			drawTextPx(f, x-2*len(txt), y, txt, p.White, 1)
		}
	}
}

func drawEnemiesPx(f *Frame, g *engine.Game, p *Palette, rc map[rune]Color,
	camX, camY float64, ox, oy int) {
	for _, e := range g.Enemies {
		if e.Gone {
			continue
		}
		cx := int(math.Round((e.Pos.X + e.W/2 - camX) * Pix))
		bottom := int(math.Round((e.Pos.Y + e.H - camY) * Pix))
		switch e.State {
		case engine.EnemySquashed, engine.EnemyFlipped:
			f.Fill(cx-3, bottom-2, 6, 2, p.GoombaFlat)
		case engine.EnemyShell, engine.EnemyShellMoving:
			art := sprShell
			if e.State == engine.EnemyShellMoving {
				// motion streaks behind the shell
				dx := -e.Dir * 4
				f.Set(cx+dx, bottom-3, p.White)
				f.Set(cx+dx+e.Dir, bottom-3, p.White)
			}
			f.DrawSprite(art, rc, cx-sprW(art)/2, bottom-sprH(art), e.Dir < 0, 1)
		default:
			art := sprGoomba
			walk := sprGoombaWalk
			if e.Kind == engine.KindKoopa {
				art, walk = sprKoopa, sprKoopaWalk
			} else if e.Kind == engine.KindPara {
				art, walk = sprPara, sprParaWalk
			}
			if int(e.WalkDist/engine.EnemyFrameLen)%2 == 1 {
				art = walk
			}
			f.DrawSprite(art, rc, cx-sprW(art)/2, bottom-sprH(art), e.Dir < 0, 1)
		}
	}
}

// drawFireBars paints the rotating castle hazards: a chain of fireballs
// per bar, spinning frames alternating along the chain.
func drawFireBars(f *Frame, g *engine.Game, p *Palette, rc map[rune]Color, camX, camY float64) {
	for _, fb := range g.FireBars {
		for i := range engine.FireBarLen {
			b := fb.BallPos(i, g.Tick)
			cx := int(math.Round((b.X - camX) * Pix))
			cy := int(math.Round((b.Y - camY) * Pix))
			art := sprFireball
			if (i+g.Tick/6)%2 == 1 {
				art = sprFireballSpin
			}
			f.DrawSprite(art, rc, cx-sprW(art)/2, cy-sprH(art)/2, false, 1)
		}
	}
}

// drawPlants paints the piranha plants (before tiles, so the pipe mouth
// occludes whatever part of the plant is still inside).
func drawPlants(f *Frame, g *engine.Game, p *Palette, rc map[rune]Color, camX, camY float64) {
	for _, pl := range g.Plants {
		if pl.Gone || pl.State == engine.PlantHidden {
			continue
		}
		cx := int(math.Round((pl.Pos.X + engine.PlantW/2 - camX) * Pix))
		bottom := int(math.Round((pl.Pos.Y + engine.PlantH - camY) * Pix))
		f.DrawSprite(sprPlant, rc, cx-sprW(sprPlant)/2, bottom-sprH(sprPlant), false, 1)
	}
}

// drawFlowers paints fire flowers emerging from (or sitting on) their block.
func drawFlowers(f *Frame, g *engine.Game, p *Palette, rc map[rune]Color, camX, camY float64) {
	for _, fl := range g.FireFlowers {
		if fl.Gone {
			continue
		}
		cx := int(math.Round((fl.Pos.X + engine.FlowerW/2 - camX) * Pix))
		bottom := int(math.Round((fl.Pos.Y + engine.FlowerH - camY) * Pix))
		f.DrawSprite(sprFireFlower, rc, cx-sprW(sprFireFlower)/2, bottom-sprH(sprFireFlower), false, 1)
	}
}

// drawFireballs paints live fireballs with a two-frame spin.
func drawFireballs(f *Frame, g *engine.Game, p *Palette, rc map[rune]Color, camX, camY float64) {
	for _, fb := range g.Fireballs {
		if fb.Gone {
			continue
		}
		art := sprFireball
		if (g.Tick/6)%2 == 1 {
			art = sprFireballSpin
		}
		cx := int(math.Round((fb.Pos.X + engine.FireballW/2 - camX) * Pix))
		cy := int(math.Round((fb.Pos.Y + engine.FireballH/2 - camY) * Pix))
		f.DrawSprite(art, rc, cx-sprW(art)/2, cy-sprH(art)/2, false, 1)
	}
}

// drawWorldCard paints the "WORLD 1-2  x3" interstitial over black.
func drawWorldCard(f *Frame, g *engine.Game, p *Palette) {
	f.Fill(0, 0, f.W, f.H, Color{})
	drawCenterPx(f, f.H/2-6, g.CardName(), p.White, 1)
	rc := runeColors(p)
	cx := f.W/2 - 16
	y := f.H/2 + 2
	f.DrawSprite(sprMarioSmall, rc, cx, y, false, 1)
	drawTextPx(f, cx+12, y+1, "X "+strconv.Itoa(g.Lives), p.White, 1)
}

func drawPlayerPx(f *Frame, g *engine.Game, p *Palette, rc map[rune]Color, camX, camY float64) {
	pl := g.Player
	if g.InCastle {
		return // through the door
	}
	if pl.Invincible > 0 && (g.Tick/3)%2 == 0 {
		return // damage flicker
	}
	art := sprMarioDead
	if g.State == engine.StateFlagSlide {
		// gripping the pole: the jump pose reads best
		if pl.Power >= engine.PowerSuper {
			art = sprMarioSuperJump
		} else {
			art = sprMarioSmallJump
		}
	} else if g.State != engine.StateDying {
		art = marioArt(pl)
	}
	rc = playerRuneColors(p, pl.Power == engine.PowerFire, pl.Star > 0, g.Tick)
	cx := int(math.Round((pl.Pos.X + pl.W/2 - camX) * Pix))
	bottom := int(math.Round((pl.Pos.Y + pl.H - camY) * Pix))
	f.DrawSprite(art, rc, cx-sprW(art)/2, bottom-sprH(art), pl.Facing < 0, 1)
}

// fireRuneColors re-skins mario art as fire mario: white cap and shirt,
// red overalls. Cached per palette.
var fireRuneCache sync.Map // *Palette -> map[rune]Color

func fireRuneColors(p *Palette) map[rune]Color {
	if v, ok := fireRuneCache.Load(p); ok {
		return v.(map[rune]Color)
	}
	rc := map[rune]Color{}
	for k, v := range runeColors(p) {
		rc[k] = v
	}
	rc['R'] = p.White
	rc['B'] = p.FlagRed
	fireRuneCache.Store(p, rc)
	return rc
}

// starPhaseKey identifies one flicker phase of the star re-skin.
type starPhaseKey struct {
	p     *Palette
	fire  bool
	phase int // 1..3 (0 is the un-flickered base itself)
}

var starPhaseCache sync.Map // starPhaseKey -> map[rune]Color

// playerRuneColors resolves the sprite-color map for the player this
// tick — fire mario's re-skin, then the star-power flicker — entirely
// from caches, so the render hot path never builds a map. Four phases
// cycle off the world tick, deterministic, no RNG.
func playerRuneColors(p *Palette, fire, star bool, tick int) map[rune]Color {
	base := runeColors(p)
	if fire {
		base = fireRuneColors(p)
	}
	if !star {
		return base
	}
	phase := (tick / 3) % 4
	if phase == 0 {
		return base
	}
	k := starPhaseKey{p, fire, phase}
	if v, ok := starPhaseCache.Load(k); ok {
		return v.(map[rune]Color)
	}
	rc := map[rune]Color{}
	for k, v := range base {
		rc[k] = v
	}
	switch phase {
	case 1:
		rc['R'], rc['B'] = p.White, p.GoldLight
	case 2:
		rc['R'], rc['B'] = p.Coin, p.White
	case 3:
		rc['R'], rc['B'] = p.Green, p.Coin
	}
	starPhaseCache.Store(k, rc)
	return rc
}

// marioArt picks the pose: liftoff stretch or airborne jump, landing squash,
// skid while turning against motion, otherwise the distance-driven walk
// cycle (stand frame when idle). The death pose is chosen in drawPlayerPx.
func marioArt(pl *engine.Player) []string {
	super := pl.Power >= engine.PowerSuper
	switch {
	case !pl.Grounded && pl.StretchT > 0:
		if super {
			return sprMarioSuperStretch
		}
		return sprMarioSmallStretch
	case !pl.Grounded:
		if super {
			return sprMarioSuperJump
		}
		return sprMarioSmallJump
	case pl.SquashT > 0:
		if super {
			return sprMarioSuperSquash
		}
		return sprMarioSmallSquash
	case pl.Skidding:
		if super {
			return sprMarioSuperSkid
		}
		return sprMarioSmallSkid
	case pl.Vel.X != 0:
		frames := marioSmallWalk
		if super {
			frames = marioSuperWalk
		}
		return frames[int(pl.WalkDist/engine.WalkFrameLen)%len(frames)]
	default:
		if pl.Power >= engine.PowerSuper {
			return sprMarioSuper
		}
		return sprMarioSmall
	}
}

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
