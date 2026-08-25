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
	"strings"

	"mario/engine"
)

// RGB is a 24-bit color (0xRRGGBB).
type RGB uint32

// Color pairs a truecolor value with its nearest ANSI-16 fallback index
// (0-15, standard 8 + bright 8 ordering).
type Color struct {
	RGB  RGB
	ANSI int
}

func color(hex uint32, ansi int) Color { return Color{RGB: RGB(hex), ANSI: ansi} }

// Palette holds every color the renderer uses.
type Palette struct {
	TrueColor bool

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

// NewPalette returns the game palette, emitting 24-bit color sequences when
// trueColor is set and basic ANSI-16 sequences otherwise.
func NewPalette(trueColor bool) *Palette {
	return &Palette{
		TrueColor:    trueColor,
		Sky:          color(0x5C94FC, 12),
		GroundLight:  color(0xF8B060, 11),
		GroundMid:    color(0xC84C0C, 1),
		GroundDark:   color(0x7C2800, 1),
		BrickLight:   color(0xD86818, 208%16),
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

// runeColors maps sprite-art runes to palette swatches.
func runeColors(p *Palette) map[rune]Color {
	return map[rune]Color{
		'R': p.Player, 'S': p.Skin, 'D': p.Dark, 'B': p.Overall,
		'W': p.White, 'C': p.Cloud, 'Y': p.Coin, 'L': p.GoldLight,
		'O': p.GroundMid, 'o': p.GroundLight,
		'G': p.Green, 'E': p.GreenLight, 'g': p.GreenDark,
		'K': p.KoopaSkin, 'n': p.Goomba,
	}
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
	W, H      int
	cells     []Cell
	TrueColor bool
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

// Text writes a string in one color on black.
func (s *Screen) Text(x, y int, text string, fg Color) {
	for i, r := range text {
		s.SetStyled(x+i, y, r, fg, Color{}, false)
	}
}

// TextStyled writes a styled string.
func (s *Screen) TextStyled(x, y int, text string, fg, bg Color, bold bool) {
	for i, r := range text {
		s.SetStyled(x+i, y, r, fg, bg, bold)
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
// with style transitions, reset at the end.
func (s *Screen) String() string {
	var b strings.Builder
	b.WriteString("\x1b[H")
	have := false
	var pFg, pBg Color
	pBold := false
	for y := 0; y < s.H; y++ {
		for x := 0; x < s.W; x++ {
			c := s.cells[y*s.W+x]
			if !have || c.Fg != pFg || c.Bg != pBg || c.Bold != pBold {
				b.WriteString(s.styleSeq(c))
				pFg, pBg, pBold, have = c.Fg, c.Bg, c.Bold, true
			}
			b.WriteRune(c.Ch)
		}
		if y < s.H-1 {
			b.WriteString("\x1b[0m\r\n")
			have = false
		}
	}
	b.WriteString("\x1b[0m")
	return b.String()
}

func (s *Screen) styleSeq(c Cell) string {
	parts := []string{"0"}
	if c.Bold {
		parts = append(parts, "1")
	}
	if s.TrueColor {
		parts = append(parts, fmt.Sprintf("38;2;%d;%d;%d", c.Fg.RGB>>16, (c.Fg.RGB>>8)&0xFF, c.Fg.RGB&0xFF))
		if c.Bg.RGB != 0 || c.Bg.ANSI != 0 {
			parts = append(parts, fmt.Sprintf("48;2;%d;%d;%d", c.Bg.RGB>>16, (c.Bg.RGB>>8)&0xFF, c.Bg.RGB&0xFF))
		}
	} else {
		parts = append(parts, ansiCode(c.Fg.ANSI, false))
		if c.Bg.ANSI != 0 {
			parts = append(parts, ansiCode(c.Bg.ANSI, true))
		}
	}
	return "\x1b[" + strings.Join(parts, ";") + "m"
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

// cloudBlocked reports whether a cloud anchored at tx on sky row row would
// touch solid tiles or the goal castle. Blocked clouds are skipped so they
// never slice behind level geometry — clouds only ever draw on open sky.
func cloudBlocked(g *engine.Game, tx, row int) bool {
	for x := tx; x < tx+3; x++ { // sprCloud spans 12px = 3 tiles
		if g.Level.At(x, row) != engine.Empty {
			return true
		}
	}
	// The goal castle occupies tiles FlagX+3..+7 on rows 9..12.
	c0 := g.Level.FlagX + 3
	if row >= 9 && tx+3 > c0 && tx < c0+5 {
		return true
	}
	return false
}

// HillAt reports whether a hill sits at world column tx (every ~13).
func HillAt(tx int) bool { return hashX(tx)%13 == 5 }

// BushAt reports a bush anchored at tx: its width. Bushes ~ every 7 columns.
func BushAt(tx int) (width int, ok bool) {
	h := hashX(tx)
	if h%7 != 3 || HillAt(tx) {
		return 0, false
	}
	return 2 + int(h>>10)%2, true
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
	vh := viewTilesOf(g)
	f := NewFrame(g.ViewW*Pix, vh*Pix, p.Sky)
	rc := runeColors(p)
	camX := g.CameraX
	camY := CameraY(g)
	ox := int(math.Round(camX * Pix))
	oy := int(math.Round(camY * Pix))
	txOf := func(px int) int { return px - ox }
	tyOf := func(py int) int { return py - oy }

	if g.State == engine.StateTitle {
		// Title screen: clean sky, decorations and the ground strip only,
		// so the logo and cast stay unobstructed.
		drawDecorations(f, g, p, rc, txOf, tyOf)
		drawGroundOnly(f, g, p, camX, camY, ox, oy)
		drawOverlayPx(f, g, p)
		return f
	}
	drawDecorations(f, g, p, rc, txOf, tyOf)
	drawCastleAt(f, g, p, txOf, tyOf)
	drawMushrooms(f, g, p, rc, camX, camY)
	drawTilesPx(f, g, p, camX, camY, ox, oy)
	drawCoinItems(f, g, p, rc, camX, camY, ox, oy)
	drawParticlesPx(f, g, p, rc, ox, oy)
	drawEnemiesPx(f, g, p, rc, camX, camY, ox, oy)
	drawPlayerPx(f, g, p, rc, camX, camY)
	drawOverlayPx(f, g, p)
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
	s.TrueColor = p.TrueColor
	drawHUD(s, g, p)
	drawStatus(s, p)
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

// blit packs two pixel rows per screen cell using the half block.
func drawStatus(s *Screen, p *Palette) {
	y := s.H - 1
	for x := 0; x < s.W; x++ {
		s.SetStyled(x, y, ' ', p.TextDim, p.StatusBG, false)
	}
	s.Center(y, "a/d move · w/space jump · x run · p pause · q quit", p.TextDim, p.StatusBG, false)
}
func blit(s *Screen, f *Frame) {
	worldRows := f.H / 2
	for cy := 0; cy < worldRows; cy++ {
		for x := 0; x < f.W && x < s.W; x++ {
			upper := f.At(x, cy*2)
			lower := f.At(x, cy*2+1)
			s.SetStyled(x, 1+cy, '▀', upper, lower, false)
		}
	}
}

func drawHUD(s *Screen, g *engine.Game, p *Palette) {
	for x := 0; x < s.W; x++ {
		s.SetStyled(x, 0, ' ', p.Text, p.HUDBG, false)
	}
	hud := fmt.Sprintf("SCORE %06d  COINS x%02d  WORLD %s  TIME %03d  LIVES x%d",
		g.Score, g.CoinCount, g.LevelName(), g.Time, g.Lives)
	s.TextStyled(1, 0, hud, p.Text, p.HUDBG, true)
}

func drawDecorations(f *Frame, g *engine.Game, p *Palette, rc map[rune]Color,
	txOf, tyOf func(int) int) {
	// On the title screen clouds also keep clear of the stacked text —
	// white cloud art would dissolve the white SUPER CLI subtitle.
	var bands [][4]int
	if g.State == engine.StateTitle {
		bands = titleTextBands(f)
	}
	for tx := range g.Level.Width {
		if row, _, ok := CloudAt(tx); ok && !cloudBlocked(g, tx, row) {
			x, y := txOf(tx*Pix), tyOf(row*Pix)
			if !cloudHitsBand(x, y, x+sprW(sprCloud), y+sprH(sprCloud), bands) {
				f.DrawSprite(sprCloud, rc, x, y, false, 1)
			}
		}
		if HillAt(tx) && g.Level.At(tx, engine.GroundTop).Solid() {
			f.DrawSprite(sprHill, rc, txOf(tx*Pix), tyOf(engine.GroundTop*Pix-sprH(sprHill)), false, 1)
		}
		if w, ok := BushAt(tx); ok && g.Level.At(tx, engine.GroundTop).Solid() {
			_ = w
			f.DrawSprite(sprBush, rc, txOf(tx*Pix), tyOf(engine.GroundTop*Pix-sprH(sprBush)), false, 1)
		}
	}
}

func drawCastleAt(f *Frame, g *engine.Game, p *Palette, txOf, tyOf func(int) int) {
	c0 := g.Level.FlagX + 3
	drawCastle(f, p, txOf(c0*Pix), tyOf(9*Pix))
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
			if t == engine.Empty {
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
			case engine.Question, engine.QuestionMush:
				drawQuestion(f, p, x, y, g.Tick%48 < 24)
			case engine.Used:
				drawUsed(f, p, x, y)
			case engine.Pipe:
				_, col := pipeCol(g, tx, ty)
				drawPipe(f, p, x, y, col, g.Level.At(tx, ty-1) != engine.Pipe)
			case engine.FlagPole:
				drawFlagPole(f, p, x, y)
			case engine.FlagTop:
				drawFlagTop(f, p, x, y)
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
		f.DrawSprite(sprMushroom, rc, cx-sprW(sprMushroom)/2, bottom-sprH(sprMushroom), false, 1)
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
			if e.Kind == engine.KindKoopa {
				art = sprKoopa
			}
			f.DrawSprite(art, rc, cx-sprW(art)/2, bottom-sprH(art), e.Dir < 0, 1)
		}
	}
}

func drawPlayerPx(f *Frame, g *engine.Game, p *Palette, rc map[rune]Color, camX, camY float64) {
	pl := g.Player
	if pl.Invincible > 0 && (g.Tick/3)%2 == 0 {
		return // damage flicker
	}
	art := marioArt(pl)
	cx := int(math.Round((pl.Pos.X + pl.W/2 - camX) * Pix))
	bottom := int(math.Round((pl.Pos.Y + pl.H - camY) * Pix))
	f.DrawSprite(art, rc, cx-sprW(art)/2, bottom-sprH(art), pl.Facing < 0, 1)
}

// marioArt picks the pose: airborne jump, skid while turning against
// motion, otherwise the distance-driven walk cycle (stand frame when idle).
func marioArt(pl *engine.Player) []string {
	switch {
	case !pl.Grounded:
		if pl.Super {
			return sprMarioSuperJump
		}
		return sprMarioSmallJump
	case pl.Skidding:
		if pl.Super {
			return sprMarioSuperSkid
		}
		return sprMarioSmallSkid
	case pl.Vel.X != 0:
		frames := marioSmallWalk
		if pl.Super {
			frames = marioSuperWalk
		}
		return frames[int(pl.WalkDist/engine.WalkFrameLen)%len(frames)]
	default:
		if pl.Super {
			return sprMarioSuper
		}
		return sprMarioSmall
	}
}

func drawOverlayPx(f *Frame, g *engine.Game, p *Palette) {
	mid := f.H / 2
	switch {
	case g.Paused:
		drawBannerPx(f, mid-2, "PAUSED", p.OverlayFG, p.OverlayBG, p)
	case g.State == engine.StateTitle:
		drawTitlePx(f, g, p, true)
	case g.State == engine.StateLevelClear:
		drawBannerPx(f, mid-2, "LEVEL CLEAR!", p.OverlayFG, p.OverlayBG, p)
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
func drawTitlePx(f *Frame, g *engine.Game, p *Palette, full bool) {
	rc2 := runeColors(p)
	// Bottom-anchored cascade: the cast stands ON the ground line and
	// every text element stacks above it, so no viewport height can
	// overlap or bury anything. Ground occupies the last 2 tile rows.
	castY := titleCastY(f) // mario sprite top; feet on the last sky row
	if full {
		for _, e := range titleTextEls(f) {
			if e.blink && g.Tick%40 >= 28 {
				continue
			}
			drawCenterShadowPx(f, e.y, e.s, e.ink.color(p), e.scale, p.Dark)
		}
	}
	// Mario and the goomba flank the centre, facing each other.
	cx := f.W / 2
	f.DrawSprite(sprMarioSmall, rc2, cx-2*sprW(sprMarioSmall)-6, castY, false, 2)
	f.DrawSprite(sprGoomba, rc2, cx+7, castY+2*(sprH(sprMarioSmall)-sprH(sprGoomba)), true, 2)

}

// titleInk picks a title text color from the palette.
type titleInk uint8

const (
	inkFlag titleInk = iota // MARIO logo red
	inkWhite
	inkGold
)

func (k titleInk) color(p *Palette) Color {
	switch k {
	case inkFlag:
		return p.FlagRed
	case inkGold:
		return p.GoldLight
	default:
		return p.White
	}
}

// titleText is one full-mode title screen line.
type titleText struct {
	s     string
	y     int
	scale int
	ink   titleInk
	blink bool // on-phase of the blink cycle only
}

// titleTextEls is the single source of truth for full-mode title text:
// every line with its pixel row, scale and ink. drawTitlePx stamps the
// elements; titleTextBands turns them into the keep-clear rects for the
// title cloud filter in drawDecorations.
func titleTextEls(f *Frame) []titleText {
	castY := titleCastY(f)
	castH := 2 * sprH(sprMarioSmall)
	var els []titleText
	add := func(s string, y, scale int, ink titleInk, blink bool) {
		els = append(els, titleText{s: s, y: y, scale: scale, ink: ink, blink: blink})
	}
	if castY >= 13 { // room for the ×2 logo (10px) above the cast?
		logoY := max(2, min(f.H/12, castY-10))
		add("MARIO", logoY, 2, inkFlag, false)
		if subY := logoY + 15; subY+5 <= castY { // clear gap below logo + its shadow
			add(pickTextPx([]string{"SUPER CLI EDITION", "SUPER CLI"}, f.W-2), subY, 1, inkWhite, false)
		}
	}
	add(pickTextPx([]string{"PRESS ANY KEY", "ANY KEY"}, f.W-2), min(castY+castH+1, f.H-5), 1, inkGold, true)
	if hintY := castY + castH + 7; hintY+5 <= f.H-5 {
		add(pickTextPx([]string{"L LEADERBOARD", "L BOARD"}, f.W-2), hintY, 1, inkWhite, false)
	}
	return els
}

// titleCastY is the top of the ×2-scaled title cast; its feet stand on the
// ground line (last 2 tile rows are ground).
func titleCastY(f *Frame) int {
	return f.H - 2*Pix - 2*sprH(sprMarioSmall)
}

// titleTextBands returns the pixel rects covered by full-mode title text.
// Blinking lines are always included so the cloud layout stays steady
// instead of strobing with the blink cycle.
func titleTextBands(f *Frame) [][4]int {
	var bands [][4]int
	for _, e := range titleTextEls(f) {
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
