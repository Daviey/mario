package engine

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
			m.Vel.Y += Gravity
			if m.Vel.Y > MaxFall {
				m.Vel.Y = MaxFall
			}
			if g.moveX(&m.Pos, MushroomW, MushroomH, float64(m.Dir)*MushroomWalk) {
				m.Dir = -m.Dir
			}
			landed, _, _ := g.moveY(&m.Pos, MushroomW, MushroomH, m.Vel.Y)
			if landed {
				m.Vel.Y = 0
			}
			if m.Pos.Y > float64(g.Level.Height)+2 {
				m.Gone = true
				continue
			}
		}
		if overlap(p.Pos.X, p.Pos.Y, p.W, p.H, m.Pos.X, m.Pos.Y, MushroomW, MushroomH) {
			m.Gone = true
			g.Score += MushroomScore
			p.grow()
			g.spawnSparkle(m.Pos.X+0.2, m.Pos.Y)
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
