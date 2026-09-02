package engine

// Cheep cheep — the fish (S7). Two regimes:
//
//   - swim (2-2): the level is Underwater; fish spawn off the right
//     edge on a hash cadence, cross left in a straight line (red at
//     CheepRedSpeed, gray slower — the manual wins) and pass through
//     everything. Underwater there is no stomping (S6), so contact
//     hurts and only fireballs or star power clear them.
//   - leaping (2-3): red fish launch from below the screen bottom on a
//     55-tick cadence (hash offset), arc under gravity and fall away
//     again. These ARE stompable mid-air — 200 flat plus the bounce.
//
// Every timing rolls off bowserHash windows of the tick, so the shoal
// is a pure function of the tick stream.

// Cheep tuning (per 60 Hz tick).
const (
	CheepW         = 0.9
	CheepH         = 0.55
	CheepRedSpeed  = 0.10  // red: the moderate crosser
	CheepGraySpeed = 0.065 // gray: slower (SMB1 manual, pinned)
	CheepLeapVelY  = -0.62 // launch from below the screen bottom
	CheepLeapVelX  = 0.02  // slight drift either way
	CheepSwimEvery = 90    // swim spawn cadence base; +hash%S window
	CheepSwimSpan  = 45
	CheepSwimCap   = 4 // alive swim cheeps at once
	CheepLeapEvery = 55
	CheepLeapCap   = 3 // airborne leapers at once
)

// Cheep is one fish, swimmer or leaper.
type Cheep struct {
	Pos, Vel Vec
	W, H     float64
	Red      bool
	Leaping  bool
	Gone     bool
}

// updateCheeps spawns and advances the shoal, then resolves fireball
// and player contact per fish. The spawners are level-regime gates:
// swim while Underwater (and not a leap level), leap while CheepLeaping
// and the player has yet to reach the stopping marker (the stone steps).
func (g *Game) updateCheeps() {
	if g.Level.Underwater && !g.Level.CheepLeaping {
		g.spawnSwimCheep()
	}
	if g.Level.CheepLeaping && g.Player.Pos.X < float64(g.Level.CheepStopX) {
		g.spawnLeapingCheep()
	}
	p := g.Player
	for _, c := range g.Cheeps {
		if c.Gone {
			continue
		}
		if c.Leaping {
			c.Vel.Y = applyGravity(c.Vel.Y, Gravity)
			c.Pos.X += c.Vel.X
			c.Pos.Y += c.Vel.Y
			if c.Pos.Y > float64(g.Level.Height)+2 {
				c.Gone = true
				continue
			}
		} else {
			// Swimmers cross in a straight line, through everything.
			c.Pos.X += c.Vel.X
			c.Pos.Y += c.Vel.Y
			if c.Pos.X < g.CameraX-3 {
				c.Gone = true // passed offscreen
				continue
			}
		}
		// Fireballs clear both kinds for the flat 200.
		if fb := g.fireballHit(c.Pos.X, c.Pos.Y, c.W, c.H); fb != nil {
			fb.Gone = true
			c.Gone = true
			g.Score += CheepScore
			g.spawnScorePop(c.Pos.X, c.Pos.Y, CheepScore, false)
			g.emit("stomp")
			continue
		}
		if !overlap(p.Pos.X, p.Pos.Y, p.W, p.H, c.Pos.X, c.Pos.Y, c.W, c.H) {
			continue
		}
		if p.Star > 0 {
			c.Gone = true
			g.Score += CheepScore
			g.spawnScorePop(c.Pos.X, c.Pos.Y, CheepScore, false)
			g.emit("stomp")
			continue
		}
		// A leaping cheep in the air (never underwater) is stompable.
		stomping := c.Leaping && !g.Level.Underwater &&
			p.Vel.Y > 0.02 && (p.Pos.Y+p.H) < (c.Pos.Y+c.H*StompBodyFrac)
		if stomping {
			c.Gone = true
			g.Score += CheepScore
			g.spawnScorePop(c.Pos.X, c.Pos.Y, CheepScore, false)
			g.bouncePlayer()
			g.emit("stomp")
			continue
		}
		g.hurtPlayer()
	}
}

// spawnSwimCheep launches one swimmer off the right edge when the
// cadence window opens and the cap allows: position, colour and cadence
// all derive from the level's hash salt and the spawn index (the tick
// divided by the cadence) — never from RNG state.
func (g *Game) spawnSwimCheep() {
	off := int(bowserHash(g.Level.Width, 11))
	every := CheepSwimEvery + int(bowserHash(g.Level.Width, 7)%CheepSwimSpan)
	if (g.Tick+off)%every != 0 {
		return
	}
	if g.aliveCheeps(false) >= CheepSwimCap {
		return
	}
	idx := (g.Tick + off) / every
	red := bowserHash(2, idx)&1 == 0
	speed := CheepGraySpeed
	if red {
		speed = CheepRedSpeed
	}
	g.Cheeps = append(g.Cheeps, &Cheep{
		Pos: Vec{g.CameraX + float64(g.ViewW) + 2, float64(3 + int(bowserHash(1, idx))%9)},
		Vel: Vec{-speed, 0},
		W:   CheepW, H: CheepH,
		Red: red,
	})
}

// spawnLeapingCheep launches one red leaper from below the screen
// bottom, under or near the player's column ± a hash offset, at most
// CheepLeapCap airborne at once.
func (g *Game) spawnLeapingCheep() {
	off := int(bowserHash(g.Level.Width, 13))
	if (g.Tick+off)%CheepLeapEvery != 0 {
		return
	}
	if g.aliveCheeps(true) >= CheepLeapCap {
		return
	}
	h := bowserHash(3, (g.Tick+off)/CheepLeapEvery)
	vx := CheepLeapVelX
	if h&1 != 0 {
		vx = -CheepLeapVelX
	}
	g.Cheeps = append(g.Cheeps, &Cheep{
		Pos:     Vec{g.Player.Pos.X + float64(h%5) - 2, float64(g.Level.Height) + 1},
		Vel:     Vec{vx, CheepLeapVelY},
		W:       CheepW,
		H:       CheepH,
		Red:     true,
		Leaping: true,
	})
}

// aliveCheeps counts live cheeps of one regime.
func (g *Game) aliveCheeps(leaping bool) int {
	n := 0
	for _, c := range g.Cheeps {
		if !c.Gone && c.Leaping == leaping {
			n++
		}
	}
	return n
}
