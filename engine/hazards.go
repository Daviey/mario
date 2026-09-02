package engine

import "math"

// updateFireBars damages the player on contact with any fire-bar ball.
// The bars themselves are stateless: their angle is a pure function of
// the game tick, so they never need per-tick integration.
func (g *Game) updateFireBars() {
	p := g.Player
	for _, fb := range g.FireBars {
		for i := range FireBarLen {
			b := fb.BallPos(i, g.Tick)
			if overlap(p.Pos.X, p.Pos.Y, p.W, p.H,
				b.X-FireBarBallSize/2, b.Y-FireBarBallSize/2,
				FireBarBallSize, FireBarBallSize) {
				g.hurtPlayer()
				return
			}
		}
	}
}

// touchingLava reports whether the player's body overlaps any lava tile.
func (g *Game) touchingLava() bool {
	p := g.Player
	return g.bodyInLava(p.Pos, p.W, p.H)
}

// bodyInLava reports whether a body's box overlaps any lava tile — the
// player's law above and the bowser's (bowser.go) share it. Probes use
// the skin inset, so a body whose edge merely kisses a tile boundary —
// zero overlap with the tile itself — does not burn; the hitbox matches
// the lava the player can see.
func (g *Game) bodyInLava(pos Vec, w, h float64) bool {
	x0 := int(math.Floor(pos.X + skin))
	x1 := int(math.Floor(pos.X + w - skin))
	y0 := int(math.Floor(pos.Y + skin))
	y1 := int(math.Floor(pos.Y + h - skin))
	for ty := y0; ty <= y1; ty++ {
		for tx := x0; tx <= x1; tx++ {
			if g.Level.At(tx, ty) == Lava {
				return true
			}
		}
	}
	return false
}
