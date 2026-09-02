package engine

import "math"

// Hammer bro (S7) — the two-tall elite of 3-1. Patrols a ~2.5-tile
// window at a lighter pace than a walker, hops occasionally, and
// throws spinning hammers in arcs toward the player on a hash cadence.
// Every kill pays the flat 1000: stomp (with bounce), fireball, a
// sliding shell, or a block bumped under his feet. His hammers arc
// through terrain, hurt on touch, and die only to star power (100).
// All timing is bowserHash of (spawn column, clock) — no RNG, no
// chasing: patrol only, per the pinned deviation.

// Hammer bro tuning (per 60 Hz tick).
const (
	HammerBroW        = 0.9
	HammerBroH        = 1.7
	HammerBroWalk     = 0.02  // patrol pace (S7; lighter than EnemyWalk)
	HammerBroPatrol   = 1.25  // patrol half-window around HomeX
	HammerBroHopVel   = -0.34 // occasional hop impulse
	HammerBroThrowMin = 50    // throw cadence base; +hash%S window
	HammerBroThrowSpc = 70
	HammerW           = 0.5
	HammerH           = 0.5
	HammerThrowVelX   = 0.10  // toward the player
	HammerThrowVelY   = -0.34 // lobbed up out of the hand
	HammerLife        = 240   // ticks before the hammer spins out
	HammerScore       = 100   // star-power clear of a hammer
	HammerBroScore    = 1000  // any kill of the bro himself
)

// Hammer bro lifecycle: patrolling alive, or dead (knocked on his
// back by any of the four kills, falling out of the world).
const (
	broPatrol = iota
	broDead
)

// HammerBro is one bro. Timer is the throw countdown; Clock is his
// lifetime tick count (the hash input); State is his lifecycle.
type HammerBro struct {
	Pos, Vel Vec
	W, H     float64
	Dir      int
	Timer    int
	Clock    int
	State    int
	HomeX    float64 // patrol window centre (spawn X)
	Gone     bool
}

// Hammer is one spinning projectile. Rot doubles as the age counter:
// the renderer spins off Rot, and Rot reaching HammerLife retires it.
type Hammer struct {
	Pos, Vel Vec
	Rot      int
	Gone     bool
}

// newHammerBro starts a bro patrolling left with his first throw armed
// off the spawn column's hash.
func newHammerBro(p Vec) *HammerBro {
	b := &HammerBro{Pos: p, W: HammerBroW, H: HammerBroH, Dir: -1, HomeX: p.X}
	b.Timer = HammerBroThrowMin + int(bowserHash(int(p.X), 0)%HammerBroThrowSpc)
	return b
}

// broHash is the bro's mood oracle, Bowser's with the bro's own salt.
func broHash(b *HammerBro) uint32 { return bowserHash(int(b.HomeX), b.Clock) }

// updateHammerBros advances every bro: the dead fall out of the world,
// the living throw on cadence, patrol their window, hop and ride
// gravity — and resolve shell, fireball and player contact.
func (g *Game) updateHammerBros() {
	p := g.Player
	bottom := float64(g.Level.Height) + 2
	for _, b := range g.HammerBros {
		if b.Gone {
			continue
		}
		b.Clock++
		if b.State == broDead {
			// Already dead: falls at flipped-enemy pace, unclamped.
			b.Vel.Y += Gravity * 1.4
			b.Pos.X += b.Vel.X
			b.Pos.Y += b.Vel.Y
			if b.Pos.Y > bottom {
				b.Gone = true
			}
			continue
		}

		b.Timer--
		if b.Timer <= 0 {
			g.throwHammer(b)
			b.Timer = HammerBroThrowMin + int(broHash(b)%HammerBroThrowSpc)
		}

		b.Vel.Y = applyGravity(b.Vel.Y, Gravity)
		if g.moveX(&b.Pos, b.W, b.H, float64(b.Dir)*HammerBroWalk) {
			b.Dir = -b.Dir
		}
		// Patrol clamp: turn at the window edges.
		if b.Pos.X <= b.HomeX-HammerBroPatrol {
			b.Pos.X = b.HomeX - HammerBroPatrol
			b.Dir = 1
		}
		if b.Pos.X >= b.HomeX+HammerBroPatrol {
			b.Pos.X = b.HomeX + HammerBroPatrol
			b.Dir = -1
		}
		landed, _, _ := g.moveY(&b.Pos, b.W, b.H, b.Vel.Y)
		if landed {
			b.Vel.Y = 0
			if broHash(b)%13 == 0 { // the occasional hop
				b.Vel.Y = HammerBroHopVel
			}
			// A block bumped under his feet flips him like any walker.
			if g.broOnBumpedBlock(b) {
				g.killHammerBro(b)
				continue
			}
		}

		// Sliding shells mow bros down like any enemy — flat 1000.
		shellHit := false
		for _, e := range g.Enemies {
			if e.Gone || e.State != EnemyShellMoving {
				continue
			}
			if overlap(e.Pos.X, e.Pos.Y, e.W, e.H, b.Pos.X, b.Pos.Y, b.W, b.H) {
				shellHit = true
				break
			}
		}
		if shellHit {
			g.killHammerBro(b)
			continue
		}

		// Fireballs: the fourth 1000.
		if fb := g.fireballHit(b.Pos.X, b.Pos.Y, b.W, b.H); fb != nil {
			fb.Gone = true
			g.killHammerBro(b)
			continue
		}

		if !overlap(p.Pos.X, p.Pos.Y, p.W, p.H, b.Pos.X, b.Pos.Y, b.W, b.H) {
			continue
		}
		if p.Star > 0 {
			g.killHammerBro(b)
			continue
		}
		stomping := p.Vel.Y > 0.02 && (p.Pos.Y+p.H) < (b.Pos.Y+b.H*StompBodyFrac)
		if stomping {
			g.killHammerBro(b)
			g.bouncePlayer()
			continue
		}
		g.hurtPlayer()
	}
}

// killHammerBro pays any kill of a bro: knocked on his back (the
// renderer flips State broDead corpses), flat HammerBroScore.
func (g *Game) killHammerBro(b *HammerBro) {
	b.State = broDead
	b.Vel = Vec{0, FlipVel}
	g.Score += HammerBroScore
	g.spawnScorePop(b.Pos.X, b.Pos.Y, HammerBroScore, false)
	g.emit("stomp")
}

// broOnBumpedBlock reports whether a bro is standing on a block that
// was bumped this very tick (bumps carry BumpAnimTicks from hitBlock
// and decay at the end of the tick, so == BumpAnimTicks means fresh).
func (g *Game) broOnBumpedBlock(b *HammerBro) bool {
	ty := int(math.Floor(b.Pos.Y + b.H + skin))
	x0 := int(math.Floor(b.Pos.X + skin))
	x1 := int(math.Floor(b.Pos.X + b.W - skin))
	for tx := x0; tx <= x1; tx++ {
		if g.bumps[ty*g.Level.Width+tx] == BumpAnimTicks {
			return true
		}
	}
	return false
}

// throwHammer lobs one hammer from the bro's hand toward the player.
func (g *Game) throwHammer(b *HammerBro) {
	dir := 1
	if p := g.Player; p.Pos.X+p.W/2 < b.Pos.X+b.W/2 {
		dir = -1
	}
	g.Hammers = append(g.Hammers, &Hammer{
		Pos: Vec{b.Pos.X + b.W/2 - HammerW/2, b.Pos.Y + 0.2},
		Vel: Vec{float64(dir) * HammerThrowVelX, HammerThrowVelY},
	})
}

// updateHammers advances every hammer: lobbed under fireball gravity,
// spinning (Rot), passing through terrain, until its life ends or it
// leaves the level. Player contact hurts; star power swats it for 100.
func (g *Game) updateHammers() {
	p := g.Player
	for _, h := range g.Hammers {
		if h.Gone {
			continue
		}
		h.Rot++
		h.Vel.Y = applyGravity(h.Vel.Y, FireballGravity)
		h.Pos.X += h.Vel.X
		h.Pos.Y += h.Vel.Y
		if h.Rot >= HammerLife || h.Pos.Y > float64(g.Level.Height)+2 ||
			h.Pos.X < -2 || h.Pos.X > float64(g.Level.Width)+2 {
			h.Gone = true
			continue
		}
		if !overlap(p.Pos.X, p.Pos.Y, p.W, p.H, h.Pos.X, h.Pos.Y, HammerW, HammerH) {
			continue
		}
		if p.Star > 0 {
			h.Gone = true
			g.Score += HammerScore
			g.spawnScorePop(h.Pos.X, h.Pos.Y, HammerScore, false)
			g.emit("stomp")
			continue
		}
		g.hurtPlayer()
	}
}
