package engine

// The SMB1 beanstalk (contract: 1-1's vine brick and Coin Heaven).
// Bumping a vine brick (BrickVine, 'J') spends it like a question block
// and sprouts a stalk that climbs the sky column above the brick, a
// tile every VineGrowEvery ticks. The player grabs the stalk from the
// ground with a rising Up press — the jump key doubles as the grab,
// exactly SMB's ladder rule — and rides it: Up climbs, Down descends
// (stepping off at the spent brick), the run key hops off mid-air.
//
// Reaching the crown once the stalk is fully grown trades the world for
// the level's vine room (Level.VineRoom — 1-1's Coin Heaven), and the
// heaven's open right edge drops the player back into the main level at
// Level.DropExitX, falling from above the sky: the original's
// skip-ahead exit, straight to the final staircase.
//
// The stalk itself is scenery: not solid, no collision, enemies pass
// through it — only the player's Up press interacts with it. One stalk
// per world side; a fresh bump while one lives re-seeds it (the
// original never fields two).

func newVine(tx, ty int, dest *Level) *Vine {
	return &Vine{X: tx, BaseY: ty, GrowTop: ty, Dest: dest}
}

// updateVine grows a live stalk toward the sky, one tile per interval.
func (g *Game) updateVine() {
	v := g.vine
	if v == nil || v.GrowTop <= VineTopRow {
		return
	}
	if g.Tick%VineGrowEvery == 0 {
		v.GrowTop--
	}
}

// Vine returns the live beanstalk, if one has sprouted (render-side).
func (g *Game) Vine() *Vine { return g.vine }

// tryGrabVine consumes a rising Up press into a climb: the player must
// stand overlapping the stalk's column, SMB ladder-style. The grab
// snaps him onto the stalk at the spent brick's crown — the original's
// instant pole-snatch.
func (g *Game) tryGrabVine(in Input) bool {
	v := g.vine
	p := g.Player
	if v == nil || p.Climbing || !p.Grounded || v.GrowTop >= v.BaseY {
		return false
	}
	if !(in.Up && !g.prevIn.Up) {
		return false
	}
	if horizontalOverlap(p.Pos.X, p.W, float64(v.X)) < 0.25 {
		return false
	}
	p.Climbing = true
	p.Pos.X = float64(v.X) + 0.5 - p.W/2
	p.Pos.Y = float64(v.BaseY) - p.H // feet on the spent brick
	p.Vel = Vec{}
	p.Grounded = false
	g.emit("bump")
	return true
}

// updateClimb applies one tick on the stalk, replacing the ordinary
// player physics: gravity is off, Up and Down ride the column, the run
// key hops off, and the crown of a fully grown stalk trades worlds.
func (g *Game) updateClimb(in Input) {
	p := g.Player
	v := g.vine
	p.Vel = Vec{}
	if in.Up {
		p.Pos.Y -= VineClimbSpeed
	}
	if in.Down {
		p.Pos.Y += VineClimbSpeed
	}
	if p.Pos.Y+p.H >= float64(v.BaseY) { // back at the brick: step off
		p.Pos.Y = float64(v.BaseY) - p.H
		p.Climbing = false
		p.Grounded = true
		return
	}
	if in.Run && !g.prevIn.Run { // hop off, SMB's vine leap
		p.Climbing = false
		p.Vel = Vec{X: float64(p.Facing) * MaxWalk, Y: JumpVel * 0.8}
		p.jumping = true
		g.emit("jump")
		return
	}
	if p.Pos.Y <= float64(v.GrowTop) {
		if v.Dest != nil && v.GrowTop <= VineTopRow {
			g.performVineWarp(v)
			return
		}
		p.Pos.Y = float64(v.GrowTop) // bare crown: sit at the top
	}
}

// performVineWarp trades the world for the vine's room, mirroring the
// pipe warp's stash discipline (warp.go): the main world is stashed,
// the room's cached live world applied, and play resumes standing at
// the room's start. The original arrives on the vine's crown and lets
// the player climb off; ours steps off already standing — the heaven's
// floor starts one step from the left wall, nothing is lost.
func (g *Game) performVineWarp(v *Vine) {
	if g.inRoom {
		g.roomWorlds[g.roomTemplate] = g.stashWorld()
	} else {
		g.savedWorld = g.stashWorld()
	}
	g.roomTemplate = v.Dest
	g.applyWorld(g.roomFor(v.Dest))
	g.inRoom = true
	p := g.Player
	p.Climbing = false
	p.Pos = Vec{X: 1.2, Y: float64(GroundTop) - p.H}
	p.Vel = Vec{}
	p.Grounded = true
	p.Facing = 1
	g.State = StatePlaying
	g.updateCamera()
	g.emit("pipe")
}

// exitRoomFall is the Coin Heaven's exit: past the floor's end the sky
// is open, and falling out below the room drops the player back into
// the main level at DropExitX, from above the sky.
func (g *Game) exitRoomFall() {
	x := g.Level.DropExitX
	g.roomWorlds[g.roomTemplate] = g.stashWorld()
	g.applyWorld(g.savedWorld)
	g.savedWorld = nil
	g.roomTemplate = nil
	g.inRoom = false
	p := g.Player
	p.Climbing = false
	p.Pos = Vec{X: float64(x) + 0.1, Y: -p.H - 1}
	p.Vel = Vec{X: 0.05, Y: 0}
	p.Grounded = false
	g.updateCamera()
	g.emit("pipe")
}
