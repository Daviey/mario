package engine

import "math"

// The castle boss (2-4): Bowser hops and breathes fire on a bridge over
// lava, dies to five fireballs (or star power) or the axe behind him,
// and never counts as a stomp. His mood — fire vs hop, hop height,
// wave vs flat fire — is a pure hash of (spawn column, lifetime clock),
// patterned on render.hashX: no RNG state, so a fight replays
// identically from the recording alone.

// Bowser tuning (per 60 Hz tick).
const (
	BowserHopVel     = -0.30 // hop impulse (~2.1-tile apex)
	BowserHighHopVel = -0.42 // the big hop (~4.2-tile apex)
	BowserDrift      = -0.020
	BowserPatrol     = 3.5 // hop drift window: [HomeX-BowserPatrol, HomeX]
	BowserMouthTicks = 20  // mouth-open telegraph before the fireball
	BowserFireSpeed  = 0.16
	BowserFireHP     = 5 // fireballs the shell takes
	BowserScore      = 5000
	BowserFlashTicks = 8 // hit-flash countdown after a fireball
	BowserSinkSpeed  = 0.04
	BowserSinkTicks  = 48 // the sink out of sight (~1.9 tiles at BowserSinkSpeed)
	BridgeSweepTicks = 3  // one plank column per 3 ticks
	BowserBeatTicks  = 45 // victory beat before the castle walk
	BossFireLife     = 400
)

// bowserHash is Bowser's mood oracle: a pure hash of the spawn column
// and lifetime clock (knuth-style multiply and shift, like
// render.hashX). Every hop, breath and wave rolls off windows of it, so
// the fight is a pure function of the tick stream.
func bowserHash(spawnCol, clock int) uint32 {
	h := uint32(spawnCol)*2654435761 ^ uint32(clock)*0x9E3779B9
	h ^= h >> 13
	h *= 0x5BD1E995
	h ^= h >> 15
	return h
}

// spawnCol is the mood hash's column input: the spawn tile column.
func (b *Bowser) spawnCol() int { return int(b.HomeX) }

// updateBowsers advances every bowser one tick: the idle hop/fire cycle
// on the bridge, then the unclamped fall and lava sink once the support
// is gone (updateBridgeFall) or a kill lands.
func (g *Game) updateBowsers() {
	bottom := float64(g.Level.Height) + 2
	for _, b := range g.Bowsers {
		if b.Gone {
			continue
		}
		b.Clock++
		if b.Flash > 0 {
			b.Flash--
		}
		switch b.State {
		case BowserFalling:
			// No collision and deliberately unclamped: the corpse
			// accelerates out of the world — or into the lava below,
			// where the fight's last beat plays.
			b.Vel.Y += Gravity
			b.Pos.X += b.Vel.X
			b.Pos.Y += b.Vel.Y
			if g.bodyInLava(b.Pos, b.W, b.H) {
				g.bowserSinks(b)
				continue
			}
			if b.Pos.Y > bottom {
				b.Gone = true
			}
		case BowserSinking:
			b.Pos.Y += BowserSinkSpeed
			b.Timer--
			if b.Timer <= 0 {
				b.Gone = true
			}
		case BowserMouth:
			b.Vel.Y = applyGravity(b.Vel.Y, Gravity)
			if landed, _, _ := g.moveY(&b.Pos, b.W, b.H, b.Vel.Y); landed {
				b.Vel.Y = 0
			}
			b.Timer--
			if b.Timer == BowserMouthTicks/2 {
				g.breatheFire(b)
			}
			if b.Timer <= 0 {
				b.State = BowserIdle
				b.Timer = 36 + int(bowserHash(b.spawnCol(), b.Clock)>>16)%25
			}
		case BowserIdle:
			if b.Timer > 0 {
				b.Timer--
			}
			b.Vel.Y = applyGravity(b.Vel.Y, Gravity)
			landed, _, _ := g.moveY(&b.Pos, b.W, b.H, b.Vel.Y)
			if landed {
				b.Vel.Y = 0
			} else {
				// Airborne: drift left along the patrol window.
				b.Pos.X += BowserDrift
				if b.Pos.X < b.HomeX-BowserPatrol {
					b.Pos.X = b.HomeX - BowserPatrol
				}
				if b.Pos.X > b.HomeX {
					b.Pos.X = b.HomeX
				}
			}
			if b.Timer <= 0 && landed {
				h := bowserHash(b.spawnCol(), b.Clock)
				if h%10 < 4 { // ~40%: open the mouth and breathe fire
					b.State = BowserMouth
					b.Timer = BowserMouthTicks
				} else {
					if (h>>8)%10 < 3 { // ~30% of hops are the big one
						b.Vel.Y = BowserHighHopVel
					} else {
						b.Vel.Y = BowserHopVel
					}
					b.Timer = 36 + int(h>>16)%25 // re-arm the idle charge
				}
			}
		}
	}
}

// breatheFire spits one boss fire from the open mouth. The collapse
// owns the arena: no new fire once the axe is grabbed.
func (g *Game) breatheFire(b *Bowser) {
	if g.State != StatePlaying {
		return
	}
	pos := Vec{b.Pos.X - 0.3, b.Pos.Y + b.H*0.35}
	h := bowserHash(b.spawnCol(), b.Clock)
	g.BossFires = append(g.BossFires, &BossFire{
		Pos:   pos,
		Vel:   Vec{float64(b.Dir) * BowserFireSpeed, 0},
		BaseY: pos.Y,
		Wave:  h&(1<<15) != 0,
		Life:  BossFireLife,
	})
	g.emit("bowser")
}

// updateBossFires advances Bowser's breath: it flies flat (or rides a
// sine wave off its spawn height) until a wall, its lifetime or the
// level's edge ends it. The player it touches burns; the fire itself
// persists through the hit.
func (g *Game) updateBossFires() {
	for _, f := range g.BossFires {
		if f.Gone {
			continue
		}
		f.Pos.X += f.Vel.X
		if f.Wave {
			f.Pos.Y = f.BaseY + 0.55*math.Sin(float64(BossFireLife-f.Life)/9)
		}
		f.Life--

		// Wall probe across the fire's height at its leading edge.
		lead := f.Pos.X + skin
		if f.Vel.X > 0 {
			lead = f.Pos.X + BossFireW - skin
		}
		tx := int(math.Floor(lead))
		ty0 := int(math.Floor(f.Pos.Y + skin))
		ty1 := int(math.Floor(f.Pos.Y + BossFireH - skin))
		blocked := false
		for ty := ty0; ty <= ty1 && !blocked; ty++ {
			blocked = g.solidAt(tx, ty)
		}
		if blocked {
			f.Gone = true
			g.spawnSparkle(f.Pos.X, f.Pos.Y)
			continue
		}
		if f.Life <= 0 || f.Pos.X < -2 {
			f.Gone = true
			continue
		}
		p := g.Player
		if overlap(p.Pos.X, p.Pos.Y, p.W, p.H, f.Pos.X, f.Pos.Y, BossFireW, BossFireH) {
			g.hurtPlayer()
		}
	}
}

// killBowser pays a combat kill (five fireballs, or star contact): the
// corpse flips upside down and drops out of the world. An impostor
// (Disguise != KindGoomba — the worlds 1-4/2-4/3-4 bosses) is replaced
// outright by the flipped corpse of his true form, SMB1's reveal; the
// score is the same BowserScore either way.
func (g *Game) killBowser(b *Bowser) {
	g.Score += BowserScore
	g.spawnScorePop(b.Pos.X, b.Pos.Y, BowserScore, false)
	g.emit("bowserdie")
	if b.Disguise != KindGoomba {
		g.spawnRevealCorpse(b)
		b.Gone = true
		return
	}
	b.Flipped = true
	b.State = BowserFalling
	b.Vel = Vec{0, FlipVel}
}

// bowserSinks pays the lava kill (once — a combat kill already paid at
// killBowser) and starts the sink out of sight.
func (g *Game) bowserSinks(b *Bowser) {
	if !b.Flipped {
		g.Score += BowserScore
		g.spawnScorePop(b.Pos.X, b.Pos.Y, BowserScore, false)
		g.emit("bowserdie")
	}
	b.State = BowserSinking
	b.Timer = BowserSinkTicks
}

// playerBowserInteractions resolves player-vs-boss contact. A bowser is
// never a stomp: every touch hurts, and only star power turns it into a
// kill. Corpses (dead, falling, sinking) deal no damage.
func (g *Game) playerBowserInteractions() {
	p := g.Player
	for _, b := range g.Bowsers {
		if b.dead() {
			continue
		}
		if !overlap(p.Pos.X, p.Pos.Y, p.W, p.H, b.Pos.X, b.Pos.Y, b.W, b.H) {
			continue
		}
		if p.Star > 0 {
			g.killBowser(b)
			continue
		}
		g.hurtPlayer()
	}
}

// fireballVsBowsers resolves one player fireball against every live
// bowser (items.go calls it from updateFireballs). Each hit costs a
// shell point and flashes the hide; the fifth tips him into the fall.
func (g *Game) fireballVsBowsers(fb *Fireball) bool {
	for _, b := range g.Bowsers {
		if b.dead() {
			continue
		}
		if !overlap(fb.Pos.X, fb.Pos.Y, FireballW, FireballH, b.Pos.X, b.Pos.Y, b.W, b.H) {
			continue
		}
		fb.Gone = true
		b.HP--
		b.Flash = BowserFlashTicks
		if b.HP <= 0 {
			g.killBowser(b)
		}
		return true
	}
	return false
}

// bowserSupported reports whether any solid tile sits under the
// bowser's feet span — the law updateBridgeFall uses to drop him the
// moment his plank is swept.
func (g *Game) bowserSupported(b *Bowser) bool {
	ty := int(math.Floor(b.Pos.Y + b.H + skin))
	x0 := int(math.Floor(b.Pos.X + skin))
	x1 := int(math.Floor(b.Pos.X + b.W - skin))
	for tx := x0; tx <= x1; tx++ {
		if g.solidAt(tx, ty) {
			return true
		}
	}
	return false
}

// grabAxe starts the boss arena's ending: the axe is 2-4's true goal.
// The cutscene owns the player (frozen, facing the fight), the fires
// die down, and the bridge is queued for its left-to-right collapse.
func (g *Game) grabAxe() {
	p := g.Player
	g.State = StateBridgeFall
	g.emit("axe")
	p.Vel = Vec{}
	p.Facing = 1
	g.BossFires = nil
	// Collect the bridge columns left-to-right: a column-major scan of
	// the grid yields them sorted and deduplicated by construction.
	g.bridgeCols = nil
	for tx := range g.Level.Width {
		for ty := range g.Level.Height {
			if g.Level.At(tx, ty) == TileBridge {
				g.bridgeCols = append(g.bridgeCols, tx)
				break
			}
		}
	}
}

// updateBridgeFall plays the collapse: one bridge column per
// BridgeSweepTicks vanishes left-to-right, every bowser standing on
// what is left keeps fighting until his footing is gone, then falls and
// sinks into the lava. Once bridge and boss are both spent, the victory
// beat holds BowserBeatTicks before the walk to the castle.
func (g *Game) updateBridgeFall() {
	p := g.Player

	// The axe can be grabbed mid-air: land first (walk-castle physics),
	// then hold the standing pose for the whole cutscene.
	if !p.Grounded {
		p.Vel.Y = applyGravity(p.Vel.Y, Gravity)
		if landed, _, _ := g.moveY(&p.Pos, p.W, p.H, p.Vel.Y); landed {
			p.Vel.Y = 0
			p.Grounded = true
		}
	}
	p.Vel.X = 0
	p.Facing = 1

	// The sweep: leftmost plank column first.
	if len(g.bridgeCols) > 0 && g.Tick%BridgeSweepTicks == 0 {
		x := g.bridgeCols[0]
		g.bridgeCols = g.bridgeCols[1:]
		for ty := range g.Level.Height {
			if g.Level.At(x, ty) == TileBridge {
				g.Level.Set(x, ty, Empty)
			}
		}
	}

	// A bowser with nothing solid under his feet span drops; the shared
	// update keeps his fall, sink and last hops running.
	for _, b := range g.Bowsers {
		if b.dead() {
			continue
		}
		if !g.bowserSupported(b) {
			b.State = BowserFalling
		}
	}
	g.updateBowsers()

	// Bridge gone, boss gone: hold the beat, then walk to the castle.
	if len(g.bridgeCols) == 0 && g.bowsersSettled() {
		if g.stateTimer <= 0 {
			g.stateTimer = BowserBeatTicks
		}
		g.stateTimer--
		if g.stateTimer <= 0 {
			g.State = StateWalkCastle
			p.Vel = Vec{X: CastleWalkSpeed, Y: 0}
			p.Facing = 1
			g.InCastle = false
		}
	}

	g.updateCamera()
}

// bowsersSettled reports whether every bowser is Gone — vacuously true
// for a bowserless arena.
func (g *Game) bowsersSettled() bool {
	for _, b := range g.Bowsers {
		if !b.Gone {
			return false
		}
	}
	return true
}
