package engine

import "math"

// updateMushrooms advances power-up mushrooms (emerge, walk, collect).
func (g *Game) updateMushrooms() {
	p := g.Player
	for _, m := range g.Mushrooms {
		if m.Gone {
			continue
		}
		if m.Emerge > 0 {
			m.Emerge--
			m.Pos.Y -= MushroomEmergeStep
		} else {
			walk := MushroomWalk
			if m.Kind == MushStar {
				walk = StarWalk
			}
			m.Vel.Y = applyGravity(m.Vel.Y, Gravity)
			if g.moveX(&m.Pos, MushroomW, MushroomH, float64(m.Dir)*walk) {
				m.Dir = -m.Dir
			}
			landed, _, _ := g.moveY(&m.Pos, MushroomW, MushroomH, m.Vel.Y)
			if landed {
				m.Vel.Y = 0
				if m.Kind == MushStar {
					m.Vel.Y = StarBounce // the star never rests
				}
			}
			if m.Pos.Y > float64(g.Level.Height)+2 {
				m.Gone = true
				continue
			}
		}
		if overlap(p.Pos.X, p.Pos.Y, p.W, p.H, m.Pos.X, m.Pos.Y, MushroomW, MushroomH) {
			m.Gone = true
			switch m.Kind {
			case MushLife:
				g.oneUp()
				g.spawnScorePop(m.Pos.X, m.Pos.Y, 0, true)
			case MushStar:
				g.Score += StarScore
				p.Star = StarTicks
				g.spawnSparkle(m.Pos.X+0.2, m.Pos.Y)
				g.emit("star")
			default:
				g.Score += MushroomScore
				p.grow()
				g.spawnSparkle(m.Pos.X+0.2, m.Pos.Y)
				g.emit("powerup")
			}
		}
	}
}

// updateFlowers advances fire flowers: emerge, then wait to be collected.
func (g *Game) updateFlowers() {
	p := g.Player
	for _, f := range g.FireFlowers {
		if f.Gone {
			continue
		}
		if f.Emerge > 0 {
			f.Emerge--
			f.Pos.Y -= FlowerEmergeStep
		}
		if overlap(p.Pos.X, p.Pos.Y, p.W, p.H, f.Pos.X, f.Pos.Y, FlowerW, FlowerH) {
			f.Gone = true
			g.Score += FlowerScore
			if p.Power == PowerSmall {
				p.grow() // small players just become super, like SMB
			} else {
				p.fireUp()
			}
			g.spawnSparkle(f.Pos.X+0.2, f.Pos.Y)
			g.emit("powerup")
		}
	}
}

// aliveFireballs counts live fireballs.
func (g *Game) aliveFireballs() int {
	n := 0
	for _, fb := range g.Fireballs {
		if !fb.Gone {
			n++
		}
	}
	return n
}

// throwFireball launches a fireball in the player's facing direction.
func (g *Game) throwFireball() {
	p := g.Player
	// Fireballs work underwater, at reduced speed (contract S6).
	speed := FireballSpeed
	if g.Level.Underwater {
		speed *= 0.6
	}
	g.Fireballs = append(g.Fireballs, &Fireball{
		Pos:  Vec{p.Pos.X + p.W/2 + float64(p.Facing)*0.4, p.Pos.Y + p.H*0.3},
		Vel:  Vec{float64(p.Facing) * speed, -0.05},
		Life: FireballLife,
	})
	g.emit("fire")
}

// updateFireballs advances fireballs: gravity, ground bounces, wall,
// enemy and boss hits.
func (g *Game) updateFireballs() {
	for _, fb := range g.Fireballs {
		if fb.Gone {
			continue
		}
		fb.Life--
		if fb.Life <= 0 {
			fb.Gone = true
			continue
		}
		fb.Vel.Y = applyGravity(fb.Vel.Y, FireballGravity)
		if g.moveX(&fb.Pos, FireballW, FireballH, fb.Vel.X) {
			fb.Gone = true
			g.spawnSparkle(fb.Pos.X, fb.Pos.Y)
			continue
		}
		landed, _, _ := g.moveY(&fb.Pos, FireballW, FireballH, fb.Vel.Y)
		if landed {
			fb.Vel.Y = FireballBounce
		}
		if fb.Pos.Y > float64(g.Level.Height)+2 {
			fb.Gone = true
			continue
		}
		for _, e := range g.Enemies {
			if e.Gone || e.State == EnemySquashed || e.State == EnemyFlipped {
				continue
			}
			if overlap(fb.Pos.X, fb.Pos.Y, FireballW, FireballH, e.Pos.X, e.Pos.Y, e.W, e.H) {
				g.flipEnemy(e)
				g.Score += StompScore
				g.spawnScorePop(e.Pos.X, e.Pos.Y, StompScore, false)
				fb.Gone = true
				g.emit("stomp")
				break
			}
		}
		if fb.Gone {
			continue
		}
		for _, pl := range g.Plants {
			if pl.Gone {
				continue
			}
			if overlap(fb.Pos.X, fb.Pos.Y, FireballW, FireballH, pl.Pos.X, pl.Pos.Y, PlantW, PlantH) {
				pl.Gone = true
				g.Score += StompScore
				g.spawnScorePop(pl.Pos.X, pl.Pos.Y, StompScore, false)
				g.spawnSparkle(pl.Pos.X, pl.Pos.Y)
				fb.Gone = true
				break
			}
		}
		if fb.Gone {
			continue
		}
		if g.fireballVsBowsers(fb) {
			continue
		}
	}
}

// updatePlants advances the piranha plants: rise, wait, sink, hide — with
// the mercy rule that a plant never rises while the player stands near.
func (g *Game) updatePlants() {
	p := g.Player
	pcx := p.Pos.X + p.W/2
	for _, pl := range g.Plants {
		if pl.Gone {
			continue
		}
		switch pl.State {
		case PlantHidden:
			if math.Abs(pcx-(pl.Pos.X+PlantW/2)) < PlantMercyDist {
				pl.Timer = PlantHiddenTicks // mercy: hold while stood near
				continue
			}
			pl.Timer--
			if pl.Timer <= 0 {
				pl.State = PlantRising
			}
			continue
		case PlantRising:
			pl.Pos.Y -= PlantH / PlantRiseTicks
			if pl.Pos.Y <= pl.BaseY-PlantH {
				pl.Pos.Y = pl.BaseY - PlantH
				pl.State = PlantUp
				pl.Timer = PlantUpTicks
			}
		case PlantUp:
			pl.Timer--
			if pl.Timer <= 0 {
				pl.State = PlantSinking
			}
		case PlantSinking:
			pl.Pos.Y += PlantH / PlantRiseTicks
			if pl.Pos.Y >= pl.BaseY {
				pl.Pos.Y = pl.BaseY
				pl.State = PlantHidden
				pl.Timer = PlantHiddenTicks
			}
		}
		if pl.State != PlantHidden &&
			overlap(p.Pos.X, p.Pos.Y, p.W, p.H, pl.Pos.X, pl.Pos.Y, PlantW, PlantH) {
			if p.Star > 0 {
				pl.Gone = true
				g.Score += StompScore
				g.spawnScorePop(pl.Pos.X, pl.Pos.Y, StompScore, false)
				g.spawnSparkle(pl.Pos.X, pl.Pos.Y)
				g.emit("stomp")
			} else {
				g.hurtPlayer()
			}
		}
	}
}

// collectCoins picks up coin items the player touches.
func (g *Game) collectCoins() {
	p := g.Player
	for _, c := range g.CoinItems {
		if c.Gone {
			continue
		}
		if overlap(p.Pos.X, p.Pos.Y, p.W, p.H, c.Pos.X, c.Pos.Y, CoinSize, CoinSize) {
			c.Gone = true
			g.addCoin()
			g.spawnSparkle(c.Pos.X+0.2, c.Pos.Y)
		}
	}
}

// updateParticles advances visual particles and drops expired ones.
func (g *Game) updateParticles() {
	keep := g.Particles[:0]
	for _, pt := range g.Particles {
		pt.Life--
		if pt.Life <= 0 {
			continue
		}
		switch pt.Kind {
		case ParticleCoin:
			pt.Vel.Y += 0.02
		case ParticleDebris:
			pt.Vel.Y += 0.03
		}
		pt.Pos.X += pt.Vel.X
		pt.Pos.Y += pt.Vel.Y
		keep = append(keep, pt)
	}
	g.Particles = keep
}

func (g *Game) spawnCoinPop(x, y float64) {
	g.Particles = append(g.Particles, &Particle{Pos: Vec{x, y}, Vel: Vec{0, -0.30}, Life: 26, Kind: ParticleCoin})
}

func (g *Game) spawnDebris(tx, ty int) {
	x, y := float64(tx)+0.3, float64(ty)+0.2
	for _, v := range [][2]float64{{-0.06, -0.32}, {0.06, -0.32}, {-0.11, -0.22}, {0.11, -0.22}} {
		g.Particles = append(g.Particles, &Particle{Pos: Vec{x, y}, Vel: Vec{v[0], v[1]}, Life: 42, Kind: ParticleDebris})
	}
}

func (g *Game) spawnSparkle(x, y float64) {
	g.Particles = append(g.Particles, &Particle{Pos: Vec{x, y}, Vel: Vec{0, -0.05}, Life: 14, Kind: ParticleSparkle})
}

func (g *Game) spawnDustPuff(x, y float64) {
	g.Particles = append(g.Particles, &Particle{Pos: Vec{x, y}, Vel: Vec{0, -0.02}, Life: 12, Kind: ParticleDust})
}

// spawnScorePop floats a score value (0 = "1UP") up from a world point.
func (g *Game) spawnScorePop(x, y float64, val int, oneUp bool) {
	v := val
	if oneUp {
		v = 0
	}
	g.Particles = append(g.Particles, &Particle{
		Pos: Vec{x, y}, Vel: Vec{0, -0.018}, Life: 45, Kind: ParticleScore, Val: v,
	})
}
