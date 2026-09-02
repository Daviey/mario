package engine

import (
	"math"
	"testing"
)

func TestGravityKeepsPlayerGrounded(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	run(g, 120, Input{})
	p := g.Player
	if !p.Grounded {
		t.Fatal("player should stay grounded on flat floor")
	}
	if !approx(p.Pos.Y, 13-SmallH) {
		t.Errorf("y = %f, want %f (feet on ground)", p.Pos.Y, 13-SmallH)
	}
	if p.Pos.X != 2 {
		t.Errorf("x drifted to %f with no input", p.Pos.X)
	}
}

func TestWalkRightMovesAndCaps(t *testing.T) {
	g := newGame(t, buildLevel(t, 80))
	run(g, 200, Input{Right: true})
	p := g.Player
	if p.Pos.X <= 2 {
		t.Fatalf("player did not move right: %f", p.Pos.X)
	}
	if p.Vel.X > MaxWalk+1e-9 {
		t.Errorf("vx = %f exceeds walk cap %f", p.Vel.X, MaxWalk)
	}
	if p.Facing != 1 {
		t.Errorf("facing = %d, want 1", p.Facing)
	}
}

func TestRunFasterThanWalk(t *testing.T) {
	gw := newGame(t, buildLevel(t, 200))
	gr := newGame(t, buildLevel(t, 200))
	for range 300 {
		gw.Update(Input{Right: true})
		gr.Update(Input{Right: true, Run: true})
	}
	if dw, dr := gw.Player.Pos.X, gr.Player.Pos.X; dr <= dw+5 {
		t.Errorf("running (%f) should outdistance walking (%f) clearly", dr, dw)
	}
	if gr.Player.Vel.X > MaxRun+1e-9 {
		t.Errorf("run speed %f exceeds cap", gr.Player.Vel.X)
	}
}

func TestFrictionStopsPlayer(t *testing.T) {
	g := newGame(t, buildLevel(t, 120))
	run(g, 100, Input{Right: true})
	if g.Player.Vel.X == 0 {
		t.Fatal("precondition: player should be moving")
	}
	run(g, 200, Input{})
	if g.Player.Vel.X != 0 {
		t.Errorf("vx = %f after friction, want 0", g.Player.Vel.X)
	}
}

func TestFacingFlips(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	run(g, 30, Input{Right: true})
	run(g, 5, Input{})
	run(g, 30, Input{Left: true})
	if g.Player.Facing != -1 {
		t.Errorf("facing = %d, want -1", g.Player.Facing)
	}
}

func TestJumpLeavesGroundAndLands(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	airborne := false
	for range 80 {
		g.Update(Input{Up: true})
		if !g.Player.Grounded {
			airborne = true
		}
	}
	if !airborne {
		t.Fatal("player never left the ground")
	}
	if !g.Player.Grounded {
		t.Errorf("player should have landed; y=%f vy=%f", g.Player.Pos.Y, g.Player.Vel.Y)
	}
	if !approx(g.Player.Pos.Y, 13-SmallH) {
		t.Errorf("landed at y=%f, want ground level", g.Player.Pos.Y)
	}
}

func TestJumpHeight(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	minY := g.Player.Pos.Y
	for range 60 {
		g.Update(Input{Up: true})
		minY = math.Min(minY, g.Player.Pos.Y)
	}
	rise := (13 - SmallH) - minY
	if rise < 3.5 {
		t.Errorf("full jump rose %.2f tiles, want >= 3.5", rise)
	}
	if rise > 5.0 {
		t.Errorf("full jump rose %.2f tiles, want <= 5", rise)
	}
}

func TestVariableJumpTapIsLower(t *testing.T) {
	tap := newGame(t, buildLevel(t, 60))
	hold := newGame(t, buildLevel(t, 60))
	minTap, minHold := 13.0, 13.0
	for i := range 80 {
		tap.Update(Input{Up: i == 0})
		hold.Update(Input{Up: true})
		minTap = math.Min(minTap, tap.Player.Pos.Y)
		minHold = math.Min(minHold, hold.Player.Pos.Y)
	}
	if minTap <= minHold+0.5 {
		t.Errorf("tapped jump (%.2f) should be clearly lower than held (%.2f)", minTap, minHold)
	}
}

func TestCoyoteTime(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	// White-box: the instant a walk-off-ledge jump must still fire.
	g.Player.Grounded = false
	g.Player.groundTimer = CoyoteTicks - 1 // last tick inside the window
	g.Player.jumpBuffer = 0
	g.Player.Vel = Vec{}
	g.Player.Pos = Vec{20, 10} // mid-air over solid ground
	g.Update(Input{Up: true})
	if got := g.Player.Vel.Y; got != JumpVel+Gravity {
		t.Errorf("coyote jump did not fire: vy=%f want %f", got, JumpVel+Gravity)
	}
}

func TestCoyoteExpiredNoJump(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	g.Player.Grounded = false
	g.Player.groundTimer = CoyoteTicks + 1
	g.Player.Pos.Y = 8 // mid-air
	g.Update(Input{Up: true})
	if g.Player.Vel.Y == JumpVel {
		t.Error("jump fired after coyote time expired")
	}
}

func TestJumpBuffer(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	// Press jump while falling just above the ground: it should fire on
	// landing, not be swallowed or double-jump.
	g.Player.Grounded = false
	g.Player.groundTimer = 99
	g.Player.Vel.Y = 0.1
	g.Player.Pos.Y = 12.0
	g.Update(Input{Up: true}) // buffered, still airborne
	if g.Player.Vel.Y == JumpVel {
		t.Fatal("jump fired mid-air (buffer should wait for landing)")
	}
	for range 5 {
		if g.Player.Grounded {
			break
		}
		g.Update(Input{})
	}
	if !g.Player.Grounded {
		t.Fatalf("player should have landed; y=%f", g.Player.Pos.Y)
	}
	// Landing this very tick consumes the buffered jump.
	for range 10 {
		g.Update(Input{})
		if g.Player.Vel.Y < -0.1 {
			return // buffered jump fired
		}
	}
	t.Error("buffered jump never fired after landing")
}

func TestNoDoubleJump(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	g.Update(Input{Up: true}) // first jump
	first := g.Player.Vel.Y
	if first > JumpVel+Gravity+1e-9 {
		t.Fatalf("first jump: vy=%f", first)
	}
	run(g, 5, Input{})
	g.Update(Input{Up: true}) // second press mid-air
	if second := g.Player.Vel.Y; second <= JumpVel {
		t.Errorf("double jump fired: vy=%f", second)
	}
}

func TestWallStopsPlayer(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Pipe(10, 3) })
	g := newGame(t, l)
	run(g, 400, Input{Right: true, Run: true})
	// Pipe occupies columns 10-11; player right edge must stay left of 10.
	if g.Player.Pos.X+g.Player.W >= 10+0.001 {
		t.Errorf("player penetrated wall: right edge %f", g.Player.Pos.X+g.Player.W)
	}
	if g.Player.Pos.X <= 2 {
		t.Errorf("player never moved toward the wall")
	}
}

func TestLeftEdgeIsSolid(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	run(g, 300, Input{Left: true})
	if g.Player.Pos.X < 0 {
		t.Errorf("player crossed left level edge: %f", g.Player.Pos.X)
	}
}

func TestPitFallKills(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Fill(12, GroundTop, 59, LevelHeight-1, ' ') })
	g := newGame(t, l)
	for range 400 {
		if g.State != StatePlaying {
			break
		}
		g.Update(Input{Right: true, Run: true})
	}
	if g.State != StateDying {
		t.Fatalf("state = %v, want dying after pit fall (y=%f)", g.State, g.Player.Pos.Y)
	}
	if g.Lives != StartLives-1 {
		t.Errorf("lives = %d, want %d", g.Lives, StartLives-1)
	}
}

func TestCeilingStopsAscent(t *testing.T) {
	// Brick directly above the player's head at row 11.
	l := buildLevel(t, 60, func(b *Builder) { b.Fill(0, 11, 20, 11, 'B') })
	g := newGame(t, l)
	for range 30 {
		g.Update(Input{Up: true})
	}
	if g.Player.Pos.Y < 11 {
		t.Errorf("player passed through ceiling: y=%f", g.Player.Pos.Y)
	}
}

// --- Underwater regime (contract S6) ---

func TestSwimSinksGentlyToTerminal(t *testing.T) {
	l := buildLevel(t, 60)
	l.Underwater = true
	g := newGame(t, l)
	g.Player.Pos = Vec{10, 5}
	g.Player.Grounded = false
	run(g, 60, Input{})
	// Sinking (never reaching the ground from y=5 in 60 water ticks)
	// and clamped to the water terminal velocity.
	if g.Player.Pos.Y+g.Player.H >= 13 {
		t.Fatalf("sank to the bed too fast: y=%f", g.Player.Pos.Y)
	}
	if !approx(g.Player.Vel.Y, WaterMaxFall) {
		t.Errorf("sink vel = %f, want %f", g.Player.Vel.Y, WaterMaxFall)
	}
}

func TestSwimStrokeImpulse(t *testing.T) {
	l := buildLevel(t, 60)
	l.Underwater = true
	g := newGame(t, l)
	g.Player.Pos = Vec{10, 8}
	g.Player.Grounded = false
	g.Update(Input{Up: true}) // rising edge: one stroke
	if v := g.Player.Vel.Y; !approx(v, SwimStrokeVel+WaterGravity) {
		t.Errorf("stroke vel = %f, want %f (impulse plus one water gravity)",
			v, SwimStrokeVel+WaterGravity)
	}
	if g.Player.StretchT != 0 {
		t.Error("stroke played the jump stretch pose")
	}
	// Repeatable mid-water: release, press again, stroke again.
	g.Update(Input{})
	g.Update(Input{Up: true})
	if v := g.Player.Vel.Y; !approx(v, SwimStrokeVel+WaterGravity) {
		t.Errorf("second stroke vel = %f, want %f", v, SwimStrokeVel+WaterGravity)
	}
}

func TestSwimSpeedCaps(t *testing.T) {
	for _, tc := range []struct {
		run  bool
		want float64
	}{
		{false, WaterMaxWalk},
		{true, WaterMaxRun},
	} {
		l := buildLevel(t, 60)
		l.Underwater = true
		g := newGame(t, l)
		run(g, 60, Input{Right: true, Run: tc.run})
		if v := g.Player.Vel.X; !approx(v, tc.want) {
			t.Errorf("run=%v swim speed = %f, want %f", tc.run, v, tc.want)
		}
	}
}

func TestCurrentDragsSwimmerDown(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Current(20, 30) })
	l.Underwater = true
	g := newGame(t, l)
	g.Player.Pos = Vec{25, 5}
	g.Player.Grounded = false
	run(g, 60, Input{})
	// Inside the zone the drag rides on top of the water terminal.
	if !approx(g.Player.Vel.Y, WaterMaxFall+CurrentDrag) {
		t.Errorf("current sink vel = %f, want %f", g.Player.Vel.Y, WaterMaxFall+CurrentDrag)
	}
}

func TestCurrentPitKillsSwimmer(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) {
		b.Current(20, 30)
		b.Fill(20, GroundTop, 31, LevelHeight-1, ' ') // pit under the current
	})
	l.Underwater = true
	g := newGame(t, l)
	g.Player.Pos = Vec{25, 5}
	g.Player.Grounded = false
	died := false
	for i := 0; i < 400 && !died; i++ {
		g.Update(Input{})
		died = g.State == StateDying
	}
	if !died {
		t.Errorf("state = %v after 400 ticks, want a drowning death in the pit", g.State)
	}
}
func TestUnderwaterFireballAtReducedSpeed(t *testing.T) {
	l := buildLevel(t, 60)
	l.Underwater = true
	g := newGame(t, l)
	g.Player.Power = PowerFire
	g.Update(Input{Run: true})
	if len(g.Fireballs) != 1 {
		t.Fatalf("fireballs = %d, want 1 (allowed underwater)", len(g.Fireballs))
	}
	if v := g.Fireballs[0].Vel.X; !approx(v, FireballSpeed*0.6) {
		t.Errorf("underwater fireball speed = %f, want %f", v, FireballSpeed*0.6)
	}
}

// --- Lifts and springboards (contract S8) ---

func TestLiftVertOscillatesAroundBase(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Lift(20, 8, 3, LiftVert, 2) })
	g := newGame(t, l)
	if len(g.Lifts) != 1 {
		t.Fatalf("lifts = %d, want 1", len(g.Lifts))
	}
	lf := g.Lifts[0]
	if lf.BaseY != 8 || lf.W != 3 || lf.Range != 2 {
		t.Fatalf("lift spawn = %+v, want base y=8 w=3 range=2", lf)
	}
	lo, hi := lf.Y, lf.Y
	for range 400 {
		g.Update(Input{})
		lo, hi = math.Min(lo, lf.Y), math.Max(hi, lf.Y)
	}
	if lo < 8-2-1e-9 || hi > 8+2+1e-9 {
		t.Errorf("oscillation left the ±range band: [%.3f,%.3f]", lo, hi)
	}
	if hi-lo < 3.5 {
		t.Errorf("oscillation amplitude too small: %.3f", hi-lo)
	}
}

func TestLiftRideCarriesPlayer(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Lift(20, 8, 3, LiftVert, 2) })
	g := newGame(t, l)
	lf := g.Lifts[0]
	g.Player.Pos = Vec{20.5, 6}
	run(g, 30, Input{})
	if !g.Player.Grounded {
		t.Fatalf("player never landed on the lift (y=%f top=%f)", g.Player.Pos.Y, lf.Y)
	}
	// The rider's feet stay glued to the surface through the motion.
	for range 200 {
		g.Update(Input{})
		if d := (g.Player.Pos.Y + g.Player.H) - lf.Y; d > 0.16 || d < -0.02 {
			t.Fatalf("rider left the platform: feet-top=%+.3f (y=%f top=%f)",
				d, g.Player.Pos.Y, lf.Y)
		}
	}
}
func TestLiftHorizCarriesRiderSideways(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Lift(20, 12, 3, LiftHoriz, 2) })
	g := newGame(t, l)
	lf := g.Lifts[0]
	g.Player.Pos = Vec{20.5, 10}
	run(g, 40, Input{})
	if !g.Player.Grounded {
		t.Fatal("player never landed on the platform")
	}
	// Wherever on the platform the rider lands (the platform is moving
	// while he falls), the ride then holds that offset rock steady.
	off := g.Player.Pos.X - lf.X
	for range 300 {
		g.Update(Input{})
		if d := (g.Player.Pos.X - lf.X) - off; !approx(d, 0) {
			t.Fatalf("rider slipped across the platform: offset drift %+.4f (px=%f lx=%f)",
				d, g.Player.Pos.X, lf.X)
		}
	}
}

func TestLiftJumpThroughFromBelow(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Lift(20, 8, 3, LiftHoriz, 0) })
	g := newGame(t, l)
	p := g.Player
	p.Pos = Vec{20.5, 8 - p.H + 0.02} // feet just under the surface
	p.Vel = Vec{Y: -0.2}
	if g.liftUnder(p) != nil {
		t.Error("rising body matched the platform top (no jump-through)")
	}
	p.Vel.Y = 0
	if g.liftUnder(p) == nil {
		t.Error("settling body did not match the platform top")
	}
}

func TestFlimsyLiftFallsAfterDelay(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Lift(20, 10, 3, LiftFlimsy, 0) })
	g := newGame(t, l)
	lf := g.Lifts[0]
	g.Player.Pos = Vec{20.5, 8}
	run(g, 30, Input{})
	if !g.Player.Grounded || lf.StandTicks == 0 {
		t.Fatalf("never stood: grounded=%v standTicks=%d", g.Player.Grounded, lf.StandTicks)
	}
	if lf.Fell {
		t.Fatalf("flimsy gave way too early (standTicks=%d)", lf.StandTicks)
	}
	run(g, LiftFlimsyDelay+10, Input{})
	if !lf.Fell {
		t.Fatalf("flimsy never fell (standTicks=%d)", lf.StandTicks)
	}
	if lf.Y <= 10 {
		t.Errorf("fallen lift did not sink: y=%.3f", lf.Y)
	}
}

func TestPulleyPairBalancesAndBreaks(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) {
		b.Lift(20, 10, 3, LiftPulley, 2)
		b.Lift(26, 10, 3, LiftPulley, 2)
	})
	g := newGame(t, l)
	if len(g.Lifts) != 2 {
		t.Fatalf("lifts = %d, want 2", len(g.Lifts))
	}
	a, b2 := g.Lifts[0], g.Lifts[1]
	if a.Pair != b2 || b2.Pair != a {
		t.Fatalf("same-row pulleys did not pair: %v %v", a.Pair, b2.Pair)
	}
	g.Player.Pos = Vec{20.5, 8}
	run(g, 40, Input{})
	if !g.Player.Grounded {
		t.Fatal("player never stood on the scale")
	}
	if a.Y <= 10 || b2.Y >= 10 {
		t.Errorf("scale did not balance: stood side y=%.3f other y=%.3f", a.Y, b2.Y)
	}
	// The travel is clamped to ±range around the base row.
	run(g, 120, Input{})
	if a.Y > 12+1e-9 || b2.Y < 8-1e-9 {
		t.Errorf("pulley travel unclamped: y=%.3f pair y=%.3f", a.Y, b2.Y)
	}
	// Stand long enough and the coupling breaks: both sides fall.
	run(g, 200, Input{})
	if !a.Fell || !b2.Fell {
		t.Errorf("overloaded scale never broke: standTicks=%d fell=%v/%v",
			a.StandTicks, a.Fell, b2.Fell)
	}
}

func TestSpringboardCompressesAndBounces(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Springboard(20, 12) })
	g := newGame(t, l)
	if len(g.Springs) != 1 {
		t.Fatalf("springs = %d, want 1", len(g.Springs))
	}
	s := g.Springs[0]
	if !approx(s.Y, 12.5) {
		t.Fatalf("spring top = %f, want 12.5 (lower half of its cell)", s.Y)
	}
	g.Player.Pos = Vec{20.1, 10}
	run(g, 20, Input{})
	if !g.Player.Grounded {
		t.Fatal("player never landed on the spring")
	}
	if s.Compress == 0 || s.Compress >= SpringFullTicks {
		t.Fatalf("compress = %d after landing, want 1..%d", s.Compress, SpringFullTicks-1)
	}
	// A jump at low compression is an ordinary hop.
	g.Update(Input{Up: true})
	if v := g.Player.Vel.Y; !approx(v, JumpVel+Gravity) {
		t.Errorf("early spring jump vel = %f, want ordinary %f", v, JumpVel+Gravity)
	}
	// Stand to full compression: the big bounce (held key: no cut).
	run(g, 40, Input{})
	if s.Compress != SpringMaxTicks {
		t.Fatalf("compress = %d, want %d", s.Compress, SpringMaxTicks)
	}
	g.Update(Input{Up: true})
	if v := g.Player.Vel.Y; !approx(v, SpringJumpVel+Gravity) {
		t.Errorf("full spring jump vel = %f, want %f", v, SpringJumpVel+Gravity)
	}
	peak := g.Player.Pos.Y
	for i := 0; i < 300 && (g.Player.Vel.Y < 0 || !g.Player.Grounded); i++ {
		g.Update(Input{Up: true})
		peak = math.Min(peak, g.Player.Pos.Y)
	}
	if rose := 12.5 - SmallH - peak; rose < 7.5 {
		t.Errorf("full bounce rose only %.2f tiles, want ~8-9 (2× apex)", rose)
	}
	// Step off and the board relaxes.
	run(g, 90, Input{Right: true})
	if s.Compress != 0 {
		t.Errorf("compress = %d after stepping off, want 0", s.Compress)
	}
}
