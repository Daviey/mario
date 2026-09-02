package engine

import "math"

// Bloober — the underwater squid (S7). A sink-then-lunge zigzag: it
// drifts down at BlooberSink, and when its timer expires it lunges up
// toward the player's column, the upward velocity decaying back into
// the sink. It passes through tiles (no collision, SMB1 law), cannot
// be stomped, and dies only to fireballs or star power — 200 either
// way. Timings come from bowserHash of the spawn column and clock.

// Bloober tuning (per 60 Hz tick).
const (
	BlooberW         = 0.9
	BlooberH         = 1.0
	BlooberSink      = 0.008 // drift-down pace between lunges
	BlooberLungeVel  = -0.28 // lunge impulse
	BlooberLungeWait = 90    // ticks between lunges; +hash%S window
	BlooberLungeSpan = 40
	BlooberXGain     = 0.01 // lunge homing gain toward the player's column
	BlooberXMax      = 0.05 // clamp of the homing drift
)

// Bloober is one squid, mid-cycle.
type Bloober struct {
	Pos, Vel Vec
	W, H     float64
	Timer    int
	Clock    int
	Gone     bool
}

// newBloober starts a squid sinking at its spawn point, its first
// lunge armed off the spawn column's hash.
func newBloober(p Vec) *Bloober {
	b := &Bloober{Pos: p, W: BlooberW, H: BlooberH, Vel: Vec{0, BlooberSink}}
	b.Timer = BlooberLungeWait + int(bowserHash(int(p.X), 0)%BlooberLungeSpan)
	return b
}

// updateBloopers advances every squid: sink, lunge on the timer, decay
// back to the sink — all through tiles — then resolve fireball and
// player contact. Contact is never a stomp; star power and fireballs
// are the only clears.
func (g *Game) updateBloopers() {
	p := g.Player
	for _, b := range g.Bloopers {
		if b.Gone {
			continue
		}
		b.Clock++
		b.Timer--
		if b.Timer <= 0 {
			b.Timer = BlooberLungeWait + int(bowserHash(int(b.Pos.X), b.Clock)%BlooberLungeSpan)
			b.Vel.Y = BlooberLungeVel
			dx := (p.Pos.X - b.Pos.X) * BlooberXGain
			b.Vel.X = math.Max(-BlooberXMax, math.Min(BlooberXMax, dx))
		} else {
			// The lunge decays back into the sink.
			b.Vel.Y = min(b.Vel.Y+BlooberSink, BlooberSink)
			b.Vel.X *= 0.98
		}
		b.Pos.X += b.Vel.X
		b.Pos.Y += b.Vel.Y
		if b.Pos.Y > float64(g.Level.Height)+2 {
			b.Gone = true
			continue
		}
		if fb := g.fireballHit(b.Pos.X, b.Pos.Y, b.W, b.H); fb != nil {
			fb.Gone = true
			b.Gone = true
			g.Score += BlooberScore
			g.spawnScorePop(b.Pos.X, b.Pos.Y, BlooberScore, false)
			g.emit("stomp")
			continue
		}
		if !overlap(p.Pos.X, p.Pos.Y, p.W, p.H, b.Pos.X, b.Pos.Y, b.W, b.H) {
			continue
		}
		if p.Star > 0 {
			b.Gone = true
			g.Score += BlooberScore
			g.spawnScorePop(b.Pos.X, b.Pos.Y, BlooberScore, false)
			g.emit("stomp")
			continue
		}
		g.hurtPlayer()
	}
}
