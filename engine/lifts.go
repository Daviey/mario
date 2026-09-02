package engine

import "math"

// Rideable platforms, SMB1's lifts (contract S8): mushroom-style
// platforms that are solid from the top only — a body may jump up
// through them from below, but falling feet land and ride. Enemies and
// projectiles ignore them entirely. Every behaviour is a pure function
// of the game tick (a sine for the oscillators, counters for the rest),
// never of stored randomness.

// LiftKind discriminates the platform behaviours.
type LiftKind int

// Platform kinds. LiftVert and LiftHoriz oscillate deterministically;
// LiftFlimsy is static until stood on and then falls; LiftPulley is one
// half of a balance scale — standing on one platform lowers it and
// raises its pair until the coupling breaks and both fall.
const (
	LiftVert   LiftKind = iota // vertical oscillation, ±Range tiles around Y
	LiftHoriz                  // horizontal oscillation, ±Range tiles around X
	LiftFlimsy                 // static until stood on, then falls
	LiftPulley                 // pairs: standing lowers this one, raises the other
)

// Lift pacing and feel.
const (
	LiftOscSpeed     = 0.02 // radians per tick: ~5.2s per full oscillation
	LiftFallSpeed    = 0.05 // tiles per tick once a flimsy/pulley lift detaches
	LiftFlimsyDelay  = 45   // stood-on ticks before a flimsy platform gives way
	PulleySpeed      = 0.03 // tiles per tick the stood-on pulley side sinks
	PulleyBreakTicks = 210  // stood-on ticks before the pulley coupling breaks

	// liftOscPeriod is the sine's period in ticks; a paired vertical
	// lift runs half a period out of phase so one falls as the other
	// rises (1-2's and 2-4's mirrored lift pairs).
	liftOscPeriod = 314
	liftOscHalf   = 157

	// Stand tolerances for the solid-from-top rule: a body whose feet
	// pass within [top-0.15, top+0.05] (plus the fall step itself, so a
	// fast fall cannot tunnel through the thin surface) lands; rising
	// bodies pass through from below.
	standTolUp   = 0.15
	standTolDown = 0.05
)

// LiftSpawn is one authored platform (Builder.Lift); the live entity is
// rebuilt from it on every level load (see buildLifts).
type LiftSpawn struct {
	X, Y  float64 // X = left edge, Y = the platform's top surface
	W     float64 // platform width in tiles
	Kind  LiftKind
	Range float64 // travel amplitude in tiles (± around the base point)
}

// Lift is a live platform. X/Y is the top-left corner, Y being the
// riding surface; BaseX/BaseY anchor the oscillation; Phase offsets the
// sine so same-spawn platforms don't move in lockstep (bowserHash keeps
// it deterministic).
type Lift struct {
	X, Y         float64
	W            float64
	Kind         LiftKind
	BaseX, BaseY float64
	Phase        int
	Range        float64
	Dir          int // pulley side: -1 left, 1 right (cosmetic pairing info)
	Fell         bool
	Pair         *Lift // the other half of a LiftPulley scale (nil otherwise)
	StandTicks   int
	Gone         bool
}

// Lift records a platform for the level under construction: w tiles
// wide with its top surface at row y, travelling ±rng tiles. Consecutive
// LiftPulley spawns on the same row pair up into one balance scale
// (left-right); consecutive LiftVert spawns on the same row run in
// opposite phase (one rises as the other sinks). mustLevel (levels.go)
// carries the record onto the parsed level.
func (b *Builder) Lift(x, y, w int, kind LiftKind, rng int) {
	b.lifts = append(b.lifts, LiftSpawn{
		X: float64(x), Y: float64(y), W: float64(w),
		Kind: kind, Range: float64(rng),
	})
}

// buildLifts turns spawn records into live platforms, wiring the
// same-row pulley pairs and phase-opposed vertical pairs.
func buildLifts(lvl *Level) []*Lift {
	var lifts []*Lift
	var prev *Lift
	for _, s := range lvl.LiftSpawns {
		l := &Lift{
			X: s.X, Y: s.Y, W: s.W, Kind: s.Kind,
			BaseX: s.X, BaseY: s.Y, Range: s.Range,
			Phase: int(bowserHash(int(s.X), int(s.Y)) % uint32(liftOscPeriod)),
		}
		pairable := s.Kind == LiftPulley || s.Kind == LiftVert
		if prev != nil && pairable && prev.Kind == s.Kind && prev.BaseY == s.Y && prev.Pair == nil {
			if s.Kind == LiftPulley {
				prev.Pair, l.Pair = l, prev
				prev.Dir, l.Dir = -1, 1
			}
			l.Phase = (prev.Phase + liftOscHalf) % liftOscPeriod
			prev = nil // pairs are couples: a third spawn starts a new pair
		} else {
			prev = l
		}
		lifts = append(lifts, l)
	}
	return lifts
}

// liftUnder returns the platform the player is coming down onto, if
// any: falling or standing still, horizontally overlapping, feet inside
// the landing band around the top surface — where the band's depth is
// the fall step itself, so a body whose feet cross the surface this
// tick cannot tunnel through it. Rising bodies (jumping up through the
// platform) never match — lifts are solid from the top only.
func (g *Game) liftUnder(p *Player) *Lift {
	if p.Vel.Y < 0 {
		return nil
	}
	feet := p.Pos.Y + p.H
	for _, l := range g.Lifts {
		if l.Gone {
			continue
		}
		if p.Pos.X+p.W <= l.X || p.Pos.X >= l.X+l.W {
			continue
		}
		if feet >= l.Y-standTolUp && feet <= l.Y+standTolDown+p.Vel.Y {
			return l
		}
	}
	return nil
}

// updateLifts advances every platform and carries its rider. Platforms
// move after the player each tick (updatePlaying order), so the ride is
// applied as the platform's own delta: a rider follows the horizontal
// shift (through walls, via moveX) and the vertical one, and a top that
// rose into the player from below pushes him up onto it instead.
func (g *Game) updateLifts() {
	p := g.Player
	for _, l := range g.Lifts {
		if l.Gone {
			continue
		}
		ox, oy := l.X, l.Y

		// The oscillators: a pure sine of the tick stream.
		if !l.Fell {
			switch l.Kind {
			case LiftVert:
				l.Y = l.BaseY + math.Sin(float64(g.Tick+l.Phase)*LiftOscSpeed)*l.Range
			case LiftHoriz:
				l.X = l.BaseX + math.Sin(float64(g.Tick+l.Phase)*LiftOscSpeed)*l.Range
			}
		}

		// The rider, judged against the pre-move geometry: feet on the
		// old top, overlapping horizontally, not jumping away.
		stood := p.Vel.Y >= 0 && p.Pos.X+p.W > ox && p.Pos.X < ox+l.W &&
			p.Pos.Y+p.H >= oy-standTolUp && p.Pos.Y+p.H <= oy+standTolDown

		switch l.Kind {
		case LiftFlimsy:
			if stood {
				l.StandTicks++
				if l.StandTicks > LiftFlimsyDelay {
					l.Fell = true // the platform gives way
				}
			}
		case LiftPulley:
			if stood && !l.Fell {
				l.StandTicks++
				l.Y = clampF(l.Y+PulleySpeed, l.BaseY-l.Range, l.BaseY+l.Range)
				if l.Pair != nil && !l.Pair.Fell {
					l.Pair.Y = clampF(l.Pair.Y-PulleySpeed, l.Pair.BaseY-l.Pair.Range, l.Pair.BaseY+l.Pair.Range)
				}
				if l.StandTicks > PulleyBreakTicks {
					l.Fell = true // overloaded: the scale breaks, both sides fall
					if l.Pair != nil {
						l.Pair.Fell = true
					}
				}
			}
		}

		// A detached platform falls on its own; the rider is no longer
		// carried and simply drops with it (the solid top keeps catching
		// him on the way down).
		if l.Fell && (l.Kind == LiftFlimsy || l.Kind == LiftPulley) {
			l.Y += LiftFallSpeed
		} else if stood {
			if dx := l.X - ox; dx != 0 {
				g.moveX(&p.Pos, p.W, p.H, dx)
			}
			p.Pos.Y += l.Y - oy
		} else if l.Y < oy && p.Vel.Y >= 0 && p.Pos.X+p.W > l.X && p.Pos.X < l.X+l.W {
			// The top rose this tick into a body that was not riding:
			// push it up onto the surface (at most the small depth a
			// fast fall or a rising platform can interpenetrate).
			if feet := p.Pos.Y + p.H; feet > l.Y && feet <= oy+0.6 {
				p.Pos.Y = l.Y - p.H
				p.Vel.Y = 0
			}
		}

		if l.Y > float64(g.Level.Height)+2 {
			l.Gone = true
		}
	}
}

// clampF clamps v into [lo, hi].
func clampF(v, lo, hi float64) float64 {
	return math.Max(lo, math.Min(v, hi))
}
