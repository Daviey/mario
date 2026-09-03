package engine

import "math"

// Bullet Bill — world 5's signature (SMB1): the cannon tiles ('N',
// merged into one blaster per horizontal run at parse) draw fire while
// the player is in range, lobbing black shells toward his side on a
// hashed cadence — pure functions of the spawn column and the tick,
// no randomness anywhere. Shells fly level and straight: no gravity,
// no terrain interaction (the original's bills cross pits, pipes and
// platforms alike). A stomp, a fireball, star power or a sliding shell
// kills one; any other contact hurts.

const (
	BulletSpeed     = 0.15 // tiles per tick — SMB1's measured bill speed
	BulletFireEvery = 170  // ticks between shots per blaster, phase-hashed
	MaxBullets      = 4    // live-shell cap across the level
	BulletScore     = 200
	BulletW         = 0.75
	BulletH         = 0.7
	BlasterRange    = 26 // the player must be inside this span to draw fire

	// BulletCardSpan is how far one card-window of flight reaches —
	// the berth a respawn column must keep from a ground-row cannon
	// (spawnThreatNear mirrors it; the respawn grace silences the
	// shots themselves, like 2-3's leaper spawner).
	BulletCardSpan = BulletSpeed * WorldCardTicks
)

// Bullet is one flying shell.
type Bullet struct {
	Pos, Vel Vec
	Gone     bool
}

// updateBlasters fires each cannon on its hashed cadence while the
// player is in range; every shot leaves toward the player's side.
func (g *Game) updateBlasters() {
	if g.respawnGrace > 0 {
		return // the respawn card-window: no shots at a fresh respawner
	}
	if len(g.Level.BlasterSpawns) == 0 || len(g.Bullets) >= MaxBullets {
		return
	}
	p := g.Player
	for _, b := range g.Level.BlasterSpawns {
		if math.Abs(p.Pos.X+p.W/2-b.X) > BlasterRange {
			continue
		}
		if (g.Tick+int(bowserHash(int(b.X), 3)))%BulletFireEvery != 0 {
			continue
		}
		dir := 1.0
		if p.Pos.X+p.W/2 < b.X {
			dir = -1.0
		}
		g.Bullets = append(g.Bullets, &Bullet{
			Pos: Vec{b.X + dir*0.9, b.Y + (1-BulletH)/2},
			Vel: Vec{dir * BulletSpeed, 0},
		})
		g.emit("kick")
		if len(g.Bullets) >= MaxBullets {
			return
		}
	}
}

// updateBullets flies every live shell, retires the ones that leave
// the world, and resolves every interaction: the player's stomp, star
// power and side contact, plus fireball and sliding-shell kills.
func (g *Game) updateBullets() {
	p := g.Player
	for _, b := range g.Bullets {
		if b.Gone {
			continue
		}
		b.Pos.X += b.Vel.X
		if b.Pos.X < -1 || b.Pos.X > float64(g.Level.Width)+1 {
			b.Gone = true
			continue
		}
		if !overlap(b.Pos.X, b.Pos.Y, BulletW, BulletH, p.Pos.X, p.Pos.Y, p.W, p.H) {
			continue
		}
		stomping := p.Vel.Y > 0.02 && (p.Pos.Y+p.H) < (b.Pos.Y+BulletH*StompBodyFrac)
		switch {
		case p.Star > 0:
			g.killBullet(b)
		case stomping:
			g.killBullet(b)
			g.Player.stompChain++
			g.bouncePlayer()
			g.emit("stomp")
		default:
			g.hurtPlayer()
		}
	}
	// Fireballs burn a bill out of the air (a buzzy's shell is immune;
	// a bullet's is not).
	for _, f := range g.Fireballs {
		if f.Gone {
			continue
		}
		for _, b := range g.Bullets {
			if b.Gone {
				continue
			}
			if overlap(f.Pos.X, f.Pos.Y, FireballW, FireballH, b.Pos.X, b.Pos.Y, BulletW, BulletH) {
				f.Gone = true
				g.killBullet(b)
				break
			}
		}
	}
	// A sliding shell mows bills like any other enemy.
	for _, e := range g.Enemies {
		if e.Gone || e.State != EnemyShellMoving {
			continue
		}
		for _, b := range g.Bullets {
			if !b.Gone && overlap(e.Pos.X, e.Pos.Y, e.W, e.H, b.Pos.X, b.Pos.Y, BulletW, BulletH) {
				g.awardLadder(e.Chain, b.Pos.X, b.Pos.Y)
				e.Chain++
				g.killBullet(b)
			}
		}
	}
}

func (g *Game) killBullet(b *Bullet) {
	if b.Gone {
		return
	}
	b.Gone = true
	g.Score += BulletScore
	g.spawnScorePop(b.Pos.X, b.Pos.Y, BulletScore, false)
}
