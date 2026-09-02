package render

import (
	"math"
	"strconv"
	"sync"

	"github.com/Daviey/mario/engine"
)

// worldFrame renders the world (sky, decorations, tiles, HUD
// overlays) into a pixel frame of ViewW*Pix x ViewH*Pix pixels. dst is
// refilled in place when already the right size (nil or a mismatched
// size allocates), keeping the per-tick pipeline allocation-free; the
// fill is total, so no state leaks between ticks.
func worldFrame(dst *Frame, g *engine.Game, p *Palette) *Frame {
	p = paletteFor(g, p)
	vh := viewTilesOf(g)
	f := refillFrame(dst, g.ViewW*Pix, vh*Pix, p.Sky)
	rc := runeColors(p)
	camX := g.CameraX
	camY := CameraY(g)
	ox := int(math.Round(camX * Pix))
	oy := int(math.Round(camY * Pix))

	// Title text is laid out once per frame: the cloud keep-clear bands
	// and the title painter consume the same elements. The demo draws
	// the same full title overlay over live play, so it needs the same
	// bands — without them its clouds slice through the demo title.
	var titleEls []titleText
	title := g.State == engine.StateTitle && !g.Demo
	if title || g.Demo {
		titleEls = titleTextEls(f, g)
	}
	drawDecorations(f, g, p, rc, ox, oy, bandsFromEls(f, titleEls))
	if title {
		// Title screen: clean sky, decorations and the ground strip only,
		// so the logo and cast stay unobstructed.
		drawGroundOnly(f, g, p, camX, camY, ox, oy)
		drawOverlayPx(f, g, p, titleEls)
		return f
	}
	// During a pipe warp the player sinks into / rises out of the pipe:
	// drawing him under the tiles lets the pipe mouth occlude him, the
	// same trick the plants use.
	pipeWarp := g.State == engine.StatePipeIn || g.State == engine.StatePipeOut
	if pipeWarp {
		drawPlayerPx(f, g, p, rc, camX, camY)
	}
	drawPlants(f, g, p, rc, camX, camY) // under the pipes: the pipe occludes
	drawCastleAt(f, g, p, ox, oy)
	drawMushrooms(f, g, p, rc, camX, camY)
	drawFlowers(f, g, p, rc, camX, camY)
	drawTilesPx(f, g, p, camX, camY, ox, oy)
	drawCoinItems(f, g, p, rc, camX, camY, ox, oy)
	drawParticlesPx(f, g, p, rc, ox, oy)
	drawEnemiesPx(f, g, p, rc, camX, camY, ox, oy)
	drawFireBars(f, g, p, rc, camX, camY)
	drawFireballs(f, g, p, rc, camX, camY)
	if !pipeWarp {
		drawPlayerPx(f, g, p, rc, camX, camY)
	}
	drawOverlayPx(f, g, p, titleEls)
	return f
}

// refillFrame returns a w×h frame filled with bg, allocating only when
// f is nil or the wrong size.
func refillFrame(f *Frame, w, h int, bg Color) *Frame {
	if f == nil || f.W != w || f.H != h {
		return NewFrame(w, h, bg)
	}
	f.Fill(0, 0, w, h, bg)
	return f
}

// Render draws one complete frame: HUD row, world pixel grid through the
// camera (with vertical follow), entities, particles and overlays. Screen
// size is ViewW*Pix columns wide and (2+ViewH*Pix/2) rows tall: a fuller
// window shows more world, never bigger sprites. An active ScoreUI
// replaces the world with the leaderboard text screens. One-shot callers
// only: per-tick callers should go through a Stream (which recycles the
// buffers) or renderInto directly.
func Render(g *engine.Game, p *Palette, ui ...*ScoreUI) *Screen {
	s, _ := renderInto(nil, nil, g, p, ui...)
	return s
}

// renderInto is Render with destination reuse: screen and world raster
// are refilled in place when already the right size, so steady-state
// rendering allocates nothing. It returns the (possibly freshly
// allocated) buffers so callers can pass them back on the next call.
func renderInto(s *Screen, world *Frame, g *engine.Game, p *Palette, ui ...*ScoreUI) (*Screen, *Frame) {
	vh := viewTilesOf(g)
	s = refillScreen(s, g.ViewW*Pix, 2+vh*Pix/2)
	s.Colors = p.Colors
	drawHUD(s, g, p)
	drawStatus(s, g, p)
	if u := firstUI(ui); u != nil && u.Mode != UIOff {
		drawScoreUIText(s, u, p, g.Tick)
	} else {
		world = worldFrame(world, g, p)
		blit(s, world)
	}
	return s, world
}

// FrameANSI renders and serializes in one call.
func FrameANSI(g *engine.Game, p *Palette, ui ...*ScoreUI) string {
	return Render(g, p, ui...).String()
}

// drawDecorations paints the sky dressing. ox, oy are the camera offset
// in pixels — world tiles draw at tile*Pix-offset, like every other
// world painter. bands are the title text's keep-clear rects (nil when
// no title overlay draws) — the caller computes them once per frame
// from the same title elements the title painter stamps.
func drawDecorations(f *Frame, g *engine.Game, p *Palette, rc map[rune]Color,
	ox, oy int, bands [][4]int) {
	switch g.Level.Theme {
	case engine.ThemeUnderground, engine.ThemeCastle:
		return // no sky dressing underground or inside the castle
	}
	// Only the overworld grows hills and bushes; the sky world keeps its
	// clouds but floats over open air.
	overworld := g.Level.Theme == engine.ThemeOverworld
	for tx := range g.Level.Width {
		if row, ok := CloudAt(tx); ok && !cloudBlocked(g, tx, row) {
			x, y := tx*Pix-ox, row*Pix-oy
			if !cloudHitsBand(x, y, x+sprW(sprCloud), y+sprH(sprCloud), bands) {
				f.DrawSprite(sprCloud, rc, x, y, false, 1)
			}
		}
		if overworld && HillAt(tx) && spanGrounded(g, tx, sprW(sprHill)) {
			f.DrawSprite(sprHill, rc, tx*Pix-ox, engine.GroundTop*Pix-oy-sprH(sprHill), false, 1)
		}
		if overworld && BushAt(tx) && spanGrounded(g, tx, sprW(sprBush)) {
			f.DrawSprite(sprBush, rc, tx*Pix-ox, engine.GroundTop*Pix-oy-sprH(sprBush), false, 1)
		}
	}
}

func drawCastleAt(f *Frame, g *engine.Game, p *Palette, ox, oy int) {
	if g.Level.FlagX < 0 {
		return // flagless levels (warp rooms) have no goal castle
	}
	c0, cy, _, _ := castleRect(g)
	x, y := c0*Pix-ox, cy*Pix-oy
	drawCastle(f, p, x, y)
	drawCastleFlag(f, p, x, y, g.CastleFlag)
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
// red overalls. Cached per palette contents, like runeColorsCache.
var fireRuneCache sync.Map // Palette -> map[rune]Color

func fireRuneColors(p *Palette) map[rune]Color {
	if v, ok := fireRuneCache.Load(*p); ok {
		return v.(map[rune]Color)
	}
	rc := map[rune]Color{}
	for k, v := range runeColors(p) {
		rc[k] = v
	}
	rc['R'] = p.White
	rc['B'] = p.FlagRed
	fireRuneCache.Store(*p, rc)
	return rc
}

// starPhaseKey identifies one flicker phase of the star re-skin.
type starPhaseKey struct {
	p     Palette
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
	k := starPhaseKey{*p, fire, phase}
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
