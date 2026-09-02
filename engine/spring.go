package engine

// The springboard (contract S8): 2-1's and 3-1's jumping board. It is a
// solid-from-top box one tile wide and half a tile tall standing on the
// ground; standing on it compresses it tick by tick, and a jump pressed
// at full compression launches at twice the normal apex — enough to
// clear the tall brick walls the original parks behind it. A jump at
// low compression is an ordinary hop.

// Spring pacing.
const (
	SpringMaxTicks  = 20    // compression ceiling while stood on
	SpringFullTicks = 18    // compression needed for the big bounce
	SpringJumpVel   = -0.62 // ~2× the normal jump apex
)

// Spring is a live springboard. X is the box's left edge, Y its top
// surface (the box spans [X, X+1] × [Y, Y+0.5]); Compress counts the
// stood-on ticks, decaying again once the player leaves it.
type Spring struct {
	X, Y     float64
	Compress int
}

// Springboard records a jumping board for the level under construction:
// (x, y) is the cell it occupies, and the board stands on that cell's
// floor (its box is the cell's lower half, top surface at y+0.5).
// mustLevel (levels.go) carries the record onto the parsed level.
func (b *Builder) Springboard(x, y int) {
	b.springs = append(b.springs, Vec{float64(x), float64(y) + 0.5})
}

// springUnder returns the springboard the player is standing on or
// coming down onto, if any (same solid-from-top law as the lifts,
// crossing-proof: the band's depth includes the fall step).
func (g *Game) springUnder(p *Player) *Spring {
	if p.Vel.Y < 0 {
		return nil
	}
	feet := p.Pos.Y + p.H
	for _, s := range g.Springs {
		if p.Pos.X+p.W <= s.X || p.Pos.X >= s.X+1 {
			continue
		}
		if feet >= s.Y-standTolUp && feet <= s.Y+standTolDown+p.Vel.Y {
			return s
		}
	}
	return nil
}

// updateSprings advances the compression: the board compresses while
// stood on (capped) and relaxes once the player leaves it.
func (g *Game) updateSprings() {
	p := g.Player
	for _, s := range g.Springs {
		if g.springStood(p, s) {
			if s.Compress < SpringMaxTicks {
				s.Compress++
			}
		} else if s.Compress > 0 {
			s.Compress--
		}
	}
}

// springStood reports whether the player is resting on this spring.
func (g *Game) springStood(p *Player, s *Spring) bool {
	return p.Vel.Y >= 0 && p.Pos.X+p.W > s.X && p.Pos.X < s.X+1 &&
		p.Pos.Y+p.H >= s.Y-standTolUp && p.Pos.Y+p.H <= s.Y+standTolDown
}
