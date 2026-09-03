package engine

// The castle maze (SMB1 4-4, 7-4, 8-4): corridors that repeat until
// the right route is taken. Our shape: each maze zone is a corridor
// blocked mid-way by a brick wall with two ways over it — the low tier
// (row 9) and the high tier (row 6, one jump up from the low). Cross
// the zone's far edge on the WRONG tier and the corridor loops: the
// player snaps back to the zone's entry, the original's scroll-back,
// and tries again. The rule is authored per zone (UpperOK) and
// invisible in play — exactly the original's cruel discoverability.
//
// The geometry is baked by Builder.Maze (wall + both tiers) so a zone
// can never be authored out of reach: the low tier is landable from
// the ground, the high tier from the low, and both run past the far
// edge before ending.

// MazeZone is one looping corridor: the span [X0, X1] on the level's
// ground row, and which tier carries the correct route out.
type MazeZone struct {
	X0, X1  int
	UpperOK bool // true: the high (row-6) tier is the way; false: the low
}

// updateMaze checks the crossing of each zone's far edge: a player
// whose feet pass X1 while riding the wrong tier (or mid-air below the
// high-tier band, which only the low route can be) loops the corridor.
func (g *Game) updateMaze() {
	p := g.Player
	for i := range g.Level.MazeZones {
		z := &g.Level.MazeZones[i]
		// The straddle window: the player's centre crosses X1 this
		// tick — past it the check goes quiet until a new crossing.
		if p.Pos.X+p.W/2 < float64(z.X1) || p.Pos.X >= float64(z.X1) {
			continue
		}
		upper := p.Pos.Y+p.H <= 7.5
		if upper == z.UpperOK {
			continue
		}
		// Wrong tier: the corridor loops.
		p.Pos.X = float64(z.X0) + 0.5
		p.Vel.X = 0
		g.emit("pipe")
		return
	}
}
