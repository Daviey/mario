package render

import (
	"math"
	"strconv"
	"strings"
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
	// Retainer cutscene (castle levels with a Toad/Princess): the world
	// gives way to the castle-interior room for the exchange, then play
	// resumes through the score tick. Checked before the sky dressing —
	// the room repaints the whole frame anyway.
	if g.State == engine.StateRetainer {
		drawRetainerScene(f, g, p, rc)
		drawOverlayPx(f, g, p, nil)
		return f
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
	drawLifts(f, g, p, rc, camX, camY) // platforms under the actors
	drawSprings(f, g, p, rc, camX, camY)
	drawVine(f, g, p, rc, camX, camY)
	drawBullets(f, g, p, rc, camX, camY)
	drawAxe(f, g, p, rc, ox, oy)
	drawCoinItems(f, g, p, rc, camX, camY, ox, oy)
	drawParticlesPx(f, g, p, rc, ox, oy)
	drawEnemiesPx(f, g, p, rc, camX, camY, ox, oy)
	drawBowsers(f, g, p, rc, camX, camY)
	drawBossFires(f, g, p, rc, camX, camY)
	drawFireBars(f, g, p, rc, camX, camY)
	drawFireballs(f, g, p, rc, camX, camY)
	drawPodoboos(f, g, p, rc, camX, camY)
	drawCheeps(f, g, p, rc, camX, camY)
	drawBloopers(f, g, p, rc, camX, camY)
	drawHammerBros(f, g, p, rc, camX, camY)
	drawHammers(f, g, p, rc, camX, camY)
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
	case engine.ThemeUnderground, engine.ThemeCastle, engine.ThemeUnderwater:
		return // no sky dressing underground, inside the castle, or under water
	}
	if g.Level.Underwater {
		return // the swim regime is all water — clouds would read as foam
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
	if g.Level.GoalX() < 0 {
		return // goalless levels (warp rooms) have no goal castle
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
			case engine.Brick, engine.BrickCoin, engine.BrickVine: // coin and vine bricks are disguised as plain bricks
				drawBrick(f, p, x, y, tx)
			case engine.BlasterTile: // bullet-bill cannon: dark iron, muzzle rim on the run's outer ends
				drawBlaster(f, p, x, y, g.Level.At(tx-1, ty) != engine.BlasterTile)
			case engine.Question, engine.QuestionMush, engine.QuestionStar:
				drawQuestion(f, p, x, y, g.Tick%48 < 24)
			case engine.Used:
				drawUsed(f, p, x, y)
			case engine.Pipe:
				_, col := pipeCol(g, tx, ty)
				drawPipe(f, p, x, y, col, g.Level.At(tx, ty-1) != engine.Pipe)
			case engine.Lava:
				drawLava(f, p, x, y, tx, g.Level.At(tx, ty-1) != engine.Lava, g.Tick)
			case engine.TileBridge:
				drawBridge(f, p, x, y, tx)
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

// drawAxe paints the bridge-room axe — the goal marker when a level has
// no flag. The blade blinks between white and pale gold on the question
// block cadence so it reads as interactive against the black castle
// sky, and it leaves the world once grabbed (every state past the
// bridge-fall follows the grab).
func drawAxe(f *Frame, g *engine.Game, p *Palette, rc map[rune]Color, ox, oy int) {
	if g.Level.AxeX < 0 {
		return
	}
	switch g.State {
	case engine.StateBridgeFall, engine.StateWalkCastle, engine.StateScoreTick, engine.StateWin:
		return // axe in hand: past the bridge
	}
	if g.Tick%48 >= 24 {
		rc = axeDimColors(p)
	}
	f.DrawSprite(sprAxe, rc, g.Level.AxeX*Pix-ox, g.Level.AxeY*Pix-oy, false, 1)
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
			art, cols := sprShell, rc
			switch {
			case e.Kind == engine.KindBuzzy:
				cols = buzzyShellColors(p) // the indigo shell
			case e.Red:
				cols = koopaRedColors(p)
			}
			if e.State == engine.EnemyShellMoving {
				// motion streaks behind the shell
				dx := -e.Dir * 4
				f.Set(cx+dx, bottom-3, p.White)
				f.Set(cx+dx+e.Dir, bottom-3, p.White)
			}
			f.DrawSprite(art, cols, cx-sprW(art)/2, bottom-sprH(art), e.Dir < 0, 1)
		default:
			art, walk, cols := sprGoomba, sprGoombaWalk, rc
			switch e.Kind {
			case engine.KindKoopa:
				art, walk = sprKoopa, sprKoopaWalk
				if e.Red {
					art, walk, cols = sprKoopaRed, sprKoopaRedWalk, koopaRedColors(p)
				}
			case engine.KindPara:
				art, walk = sprPara, sprParaWalk
				if e.Red {
					art, walk, cols = sprParaRed, sprParaRedWalk, koopaRedColors(p)
				}
			case engine.KindBuzzy:
				art, walk = sprBuzzy, sprBuzzyWalk
			}
			if int(e.WalkDist/engine.EnemyFrameLen)%2 == 1 {
				art = walk
			}
			f.DrawSprite(art, cols, cx-sprW(art)/2, bottom-sprH(art), e.Dir < 0, 1)
		}
	}
}

// drawBowsers paints the bridge boss, bottom-centre anchored like the
// enemies: mouth open while telegraphing fire, hit sparkles while the
// damage flash counts down, upside down once flipped dead. Sinking
// corpses still draw; Gone ones do not.
func drawBowsers(f *Frame, g *engine.Game, p *Palette, rc map[rune]Color, camX, camY float64) {
	for _, b := range g.Bowsers {
		if b.Gone {
			continue
		}
		cx := int(math.Round((b.Pos.X + b.W/2 - camX) * Pix))
		bottom := int(math.Round((b.Pos.Y + b.H - camY) * Pix))
		art := sprBowser
		if b.State == engine.BowserMouth {
			art = sprBowserFire
		}
		x, y := cx-sprW(art)/2, bottom-sprH(art)
		if b.Flipped {
			f.drawSpriteVFlip(art, rc, x, y, b.Dir < 0)
		} else {
			f.DrawSprite(art, rc, x, y, b.Dir < 0, 1)
		}
		if b.Flash > 0 { // hit sparkles on the hide
			f.Set(x+2, y+3, p.White)
			f.Set(x+sprW(art)/2, y+6, p.White)
			f.Set(x+sprW(art)-4, y+8, p.White)
		}
	}
}

// drawBossFires paints Bowser's breath on the same two-frame spin as
// the player's fireballs, centre-anchored on the fire's box.
func drawBossFires(f *Frame, g *engine.Game, p *Palette, rc map[rune]Color, camX, camY float64) {
	for _, bf := range g.BossFires {
		if bf.Gone {
			continue
		}
		art := sprBossFire
		if (g.Tick/8)%2 == 1 {
			art = sprBossFireSpin
		}
		cx := int(math.Round((bf.Pos.X + engine.BossFireW/2 - camX) * Pix))
		cy := int(math.Round((bf.Pos.Y + engine.BossFireH/2 - camY) * Pix))
		f.DrawSprite(art, rc, cx-sprW(art)/2, cy-sprH(art)/2, false, 1)
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

// drawPodoboos paints the lava fireballs (contract S7/S9): bright orbs
// with flame tails, bottom-centre anchored like the enemies. A pod
// resting below its pool surface (Pos past BaseY+1) draws nothing —
// the lava already is the resting state it would sit inside.
func drawPodoboos(f *Frame, g *engine.Game, p *Palette, rc map[rune]Color, camX, camY float64) {
	for _, pd := range g.Podoboos {
		if pd.Gone || pd.Pos.Y > pd.BaseY+1 {
			continue
		}
		cx := int(math.Round((pd.Pos.X + pd.W/2 - camX) * Pix))
		bottom := int(math.Round((pd.Pos.Y + pd.H - camY) * Pix))
		f.DrawSprite(sprPodoboo, rc, cx-sprW(sprPodoboo)/2, bottom-sprH(sprPodoboo), false, 1)
	}
}

// drawCheeps paints cheep cheeps, centre-anchored on the fish box: red
// or gray, swimming or leaping. They face their travel direction — the
// art looks right, a left-moving fish flips.
func drawCheeps(f *Frame, g *engine.Game, p *Palette, rc map[rune]Color, camX, camY float64) {
	for _, c := range g.Cheeps {
		if c.Gone {
			continue
		}
		art, cols := sprCheep, rc
		if !c.Red {
			art, cols = sprCheepGray, cheepGrayColors(p)
		}
		cx := int(math.Round((c.Pos.X + c.W/2 - camX) * Pix))
		cy := int(math.Round((c.Pos.Y + c.H/2 - camY) * Pix))
		f.DrawSprite(art, cols, cx-sprW(art)/2, cy-sprH(art)/2, c.Vel.X < 0, 1)
	}
}

// drawBloopers paints the underwater squid, bottom-anchored so the
// tentacles hang at the box's foot.
func drawBloopers(f *Frame, g *engine.Game, p *Palette, rc map[rune]Color, camX, camY float64) {
	for _, bl := range g.Bloopers {
		if bl.Gone {
			continue
		}
		cx := int(math.Round((bl.Pos.X + bl.W/2 - camX) * Pix))
		bottom := int(math.Round((bl.Pos.Y + bl.H - camY) * Pix))
		f.DrawSprite(sprBloober, rc, cx-sprW(sprBloober)/2, bottom-sprH(sprBloober), bl.Vel.X < 0, 1)
	}
}

// drawHammerBros paints the armoured koopas: the stride frame follows
// the bro's own clock, so the waddle is a pure function of state like
// every other walk cycle.
func drawHammerBros(f *Frame, g *engine.Game, p *Palette, rc map[rune]Color, camX, camY float64) {
	for _, hb := range g.HammerBros {
		if hb.Gone {
			continue
		}
		art := sprHammerBro
		if (hb.Clock/8)%2 == 1 {
			art = sprHammerBroWalk
		}
		cx := int(math.Round((hb.Pos.X + hb.W/2 - camX) * Pix))
		bottom := int(math.Round((hb.Pos.Y + hb.H - camY) * Pix))
		f.DrawSprite(art, rc, cx-sprW(art)/2, bottom-sprH(art), hb.Dir < 0, 1)
	}
}

// drawHammers paints thrown hammers as a spinning read: the art flips
// on the hammer's rotation counter, the same trick the fireball spin
// pulls off the world tick. A 0.5×0.5 box centres the 5×5 art.
func drawHammers(f *Frame, g *engine.Game, p *Palette, rc map[rune]Color, camX, camY float64) {
	for _, hm := range g.Hammers {
		if hm.Gone {
			continue
		}
		cx := int(math.Round((hm.Pos.X + 0.25 - camX) * Pix)) // 0.25 = W/2 of the 0.5 box
		cy := int(math.Round((hm.Pos.Y + 0.25 - camY) * Pix))
		f.DrawSprite(sprHammer, rc, cx-sprW(sprHammer)/2, cy-sprH(sprHammer)/2, (hm.Rot/4)%2 == 1, 1)
	}
}

// drawBullets paints the flying bills (bullet.go): level flight, no
// terrain interaction, flipped when bound left.
func drawBullets(f *Frame, g *engine.Game, p *Palette, rc map[rune]Color, camX, camY float64) {
	for _, b := range g.Bullets {
		cx := int(math.Round((b.Pos.X + engine.BulletW/2 - camX) * Pix))
		cy := int(math.Round((b.Pos.Y + engine.BulletH/2 - camY) * Pix))
		f.DrawSprite(sprBullet, rc, cx-sprW(sprBullet)/2, cy-sprH(sprBullet)/2, b.Vel.X < 0, 1)
	}
}

// drawVine paints the beanstalk: one stalk tile per row from the spent
// brick's crown up to the grown top, leaf side alternating via the
// draw mirror. The brick itself renders as a plain brick until spent
// (the tile switch) and as Used after — the stalk is the only tell.
func drawVine(f *Frame, g *engine.Game, p *Palette, rc map[rune]Color, camX, camY float64) {
	v := g.Vine()
	if v == nil {
		return
	}
	x := int(math.Round((float64(v.X)+0.5-camX)*Pix)) - sprW(sprVine)/2
	for ty := v.GrowTop; ty < v.BaseY; ty++ {
		y := int(math.Round((float64(ty) - camY) * Pix))
		f.DrawSprite(sprVine, rc, x, y, (v.BaseY-ty)%2 == 1, 1)
	}
}

// drawLifts paints the rideable platforms (contract S8/S9): the fixed
// 4×2 platform chunk, stamped side by side until the lift's pixel
// width is covered and centred on it, so any W reads as one mushroom
// platform. Flimsy lifts get the pale plank art.
func drawLifts(f *Frame, g *engine.Game, p *Palette, rc map[rune]Color, camX, camY float64) {
	for _, l := range g.Lifts {
		if l.Gone {
			continue
		}
		art := sprLift
		if l.Kind == engine.LiftFlimsy {
			art = sprLiftFlimsy
		}
		px := int(math.Round((l.X - camX) * Pix))
		py := int(math.Round((l.Y - camY) * Pix))
		wpx := int(math.Round(l.W * Pix))
		aw := sprW(art)
		n := (wpx + aw - 1) / aw
		x0 := px + (wpx-n*aw)/2
		for i := range n {
			f.DrawSprite(art, rc, x0+i*aw, py, false, 1)
		}
	}
}

// drawSprings paints springboards with a progressive, bottom-anchored
// squash: the instant a rider's weight lands the board shortens a pixel
// onto its closed coil (sprSpringHalf), and at full compression — the
// big-bounce threshold — the plate goes gold (sprSpringArmed), the same
// interactive tell as the bridge axe's blink. The 2026-09-02 report:
// the old open/closed coil swap differed by four dark pixels and was
// invisible in play.
func drawSprings(f *Frame, g *engine.Game, p *Palette, rc map[rune]Color, camX, camY float64) {
	for _, s := range g.Springs {
		art := sprSpring
		switch {
		case s.Compress >= engine.SpringFullTicks:
			art = sprSpringArmed
		case s.Compress > 0:
			art = sprSpringHalf
		}
		x := int(math.Round((s.X - camX) * Pix))
		y := int(math.Round((s.Y - camY) * Pix))
		f.DrawSprite(art, rc, x+(Pix-sprW(art))/2, y+sprH(sprSpring)-sprH(art), false, 1)
	}
}

// drawWorldCard paints the "WORLD 1-2  x3" interstitial over black.
func drawWorldCard(f *Frame, g *engine.Game, p *Palette) {
	f.Fill(0, 0, f.W, f.H, Color{})
	if g.SecondQuest {
		drawCenterPx(f, f.H/2-14, "2ND QUEST", p.White, 1)
	}
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

// variantColors builds a copy of the rune palette with per-rune
// overrides — the shared body of the re-skin caches (fire mario, the
// axe blink, red koopas, gray cheeps, buzzy shells, the princess
// gown). Cached maps are read-only by convention (see runeColors).
func variantColors(p *Palette, cache *sync.Map, over map[rune]Color) map[rune]Color {
	if v, ok := cache.Load(*p); ok {
		return v.(map[rune]Color)
	}
	rc := map[rune]Color{}
	for k, v := range runeColors(p) {
		rc[k] = v
	}
	for k, v := range over {
		rc[k] = v
	}
	cache.Store(*p, rc)
	return rc
}

// fireRuneColors re-skins mario art as fire mario: white cap and shirt,
// red overalls. Cached per palette contents, like runeColorsCache.
var fireRuneCache sync.Map // Palette -> map[rune]Color

func fireRuneColors(p *Palette) map[rune]Color {
	return variantColors(p, &fireRuneCache, map[rune]Color{
		'R': p.White,
		'B': p.FlagRed,
	})
}

// axeDimColors swaps the axe blade to pale gold — the blink partner of
// white. Cached per palette contents, like fireRuneColors.
var axeDimCache sync.Map // Palette -> map[rune]Color
func axeDimColors(p *Palette) map[rune]Color {
	return variantColors(p, &axeDimCache, map[rune]Color{
		'W': p.GoldLight,
	})
}

// koopaRedColors re-skins koopa/para/shell art as the red variant
// (contract S2 'R'/'r'): shell greens go to the red family.
var koopaRedCache sync.Map // Palette -> map[rune]Color

func koopaRedColors(p *Palette) map[rune]Color {
	return variantColors(p, &koopaRedCache, map[rune]Color{
		'G': p.FlagRed,
		'E': color(0xFF8878, 9),
		'g': color(0x8C1410, 1),
	})
}

// cheepGrayColors re-skins the cheep art gray for the slower gray
// variant (manual wins on the speed call).
var cheepGrayCache sync.Map // Palette -> map[rune]Color

func cheepGrayColors(p *Palette) map[rune]Color {
	return variantColors(p, &cheepGrayCache, map[rune]Color{
		'R': color(0xA8B0C0, 7),
	})
}

// buzzyShellColors re-skins the shared shell art as the buzzy's indigo
// shell (contract S9: shell reuse of sprShell with dark cols).
var buzzyShellCache sync.Map // Palette -> map[rune]Color

func buzzyShellColors(p *Palette) map[rune]Color {
	return variantColors(p, &buzzyShellCache, map[rune]Color{
		'G': p.Overall, // the buzzy art's indigo
		'g': p.Dark,
	})
}

// princessColors re-skins the princess art: the 'R' gown rune becomes
// pink (no pink swatch exists in the base palette — this is the one
// place a costume color is introduced).
var princessCache sync.Map // Palette -> map[rune]Color

func princessColors(p *Palette) map[rune]Color {
	return variantColors(p, &princessCache, map[rune]Color{
		'R': color(0xF070A8, 13),
	})
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

//
// The retainer cutscene (contract S5/S9): the castle interior where the
// saved Mushroom Retainer (or, after the last castle, the Princess)
// delivers the arcade-text message. Rendered purely from engine state —
// the player's walk-in position, the NPC's level-fixed spot and the
// world tick drive everything.
//

// Retainer cutscene lines. The arcade charset has no comma (pinned
// deviation), so the canonical punctuation lives only in the manual.
const (
	retainerTextToad     = "THANK YOU MARIO BUT OUR PRINCESS IS IN ANOTHER CASTLE!"
	retainerTextPrincess = "THANK YOU MARIO YOUR QUEST IS OVER"
	retainerTextPress    = "PRESS ANY KEY"
)

// drawRetainerScene paints the castle-interior room: black air, a
// barred window, a stone floor, the retainer at RetainerAt and the
// player walking in — plus the word-wrapped message. The camera is
// recomputed from the cast's positions (the level camera may sit
// anywhere after the bridge), keeping the scene a pure function of
// state.
func drawRetainerScene(f *Frame, g *engine.Game, p *Palette, rc map[rune]Color) {
	feetRow := g.Level.RetainerAt.Y      // the NPC's feet cell
	floorTop := float64(feetRow+1) * Pix // world px of the floor surface
	camY := floorTop/Pix - (float64(f.H)-Pix)/Pix
	mid := (g.Player.Pos.X + g.Player.W/2 + float64(g.Level.RetainerAt.X)) / 2
	camX := mid - float64(f.W)/(2*Pix)
	if camX < 0 {
		camX = 0
	}
	if m := float64(g.Level.Width) - float64(f.W)/Pix; camX > m {
		camX = m
	}
	floorY := int(math.Round(floorTop - camY*Pix))

	// Layout first: the message word-wrapped to the room's width, so the
	// window can hang below it on whatever wall is left. The princess
	// adds the blinking prompt — her castle is the end of the quest.
	msg := retainerTextToad
	if g.Level.Retainer == 2 {
		msg = retainerTextPrincess
	}
	lines := wrapTextPx(msg, f.W-8, 1)
	pressRow := 3 + 7*len(lines) + 2

	// The room: themed castle air and stone (the level is a castle, so p
	// is already castle-skinned), with a barred window lighting the wall
	// below the message.
	f.Fill(0, 0, f.W, f.H, p.Sky)
	textY := 3
	for _, ln := range lines {
		drawCenterPx(f, textY, ln, p.White, 1)
		textY += 7
	}
	if g.Level.Retainer == 2 {
		// The prompt blinks, but its row is always RESERVED — the window
		// below must not bounce with the blink phase.
		if g.Tick%40 < 28 {
			drawCenterPx(f, pressRow, retainerTextPress, p.GoldLight, 1)
		}
		textY = pressRow + 7
	}
	ww, wh := Pix*2, Pix+2
	wx, wy := f.W/2-ww/2, textY+1
	if floorY-sprH(sprPrincess)-2 > wy+wh { // only when the wall has room
		f.Fill(wx, wy, ww, wh, p.GroundDark)
		f.Fill(wx+ww/2, wy, 1, wh, p.GroundMid)
		f.Fill(wx, wy+wh/2, ww, 1, p.GroundMid)
	}
	f.Fill(0, floorY, f.W, 1, p.GroundLight)
	f.Fill(0, floorY+1, f.W, f.H-floorY-1, p.GroundMid)
	for x := range f.W { // staggered stone seams below the surface
		f.Set(x, floorY+3+(x/6%2)*2, p.GroundDark)
	}

	// The retainer: Toad for the impostor castles, the Princess after
	// the real final one. RetainerAt is the feet cell, so the sprite
	// bottom sits on the floor surface.
	npc, cols := sprToad, rc
	if g.Level.Retainer == 2 {
		npc, cols = sprPrincess, princessColors(p)
	}
	nx := int(math.Round((float64(g.Level.RetainerAt.X) + 0.5 - camX) * Pix))
	f.DrawSprite(npc, cols, nx-sprW(npc)/2, floorY-sprH(npc), false, 1)

	// The player walks in from the door — drawn directly (not via
	// drawPlayerPx) because InCastle is set and this room IS where he
	// reappears.
	pl := g.Player
	art := marioArt(pl)
	pcx := int(math.Round((pl.Pos.X + pl.W/2 - camX) * Pix))
	pbottom := int(math.Round((pl.Pos.Y + pl.H - camY) * Pix))
	f.DrawSprite(art, playerRuneColors(p, pl.Power == engine.PowerFire, pl.Star > 0, g.Tick),
		pcx-sprW(art)/2, pbottom-sprH(art), pl.Facing < 0, 1)

	// (text drawn above, before the window was placed under it)
}

// wrapTextPx greedily wraps s on spaces into lines that each fit maxW
// pixels at the given scale; a single word wider than maxW keeps its
// own line (the font's '?' fallback covers anything unprintable).
func wrapTextPx(s string, maxW, scale int) []string {
	words := strings.Split(s, " ")
	var lines []string
	cur := ""
	for _, w := range words {
		cand := w
		if cur != "" {
			cand = cur + " " + w
		}
		if cur != "" && textWidthPx(cand, scale) > maxW {
			lines = append(lines, cur)
			cur = w
			continue
		}
		cur = cand
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}
