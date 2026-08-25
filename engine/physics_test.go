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
	for i := 0; i < 300; i++ {
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
	for i := 0; i < 80; i++ {
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
	for i := 0; i < 60; i++ {
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
	for i := 0; i < 80; i++ {
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
	for i := 0; i < 5 && !g.Player.Grounded; i++ {
		g.Update(Input{})
	}
	if !g.Player.Grounded {
		t.Fatalf("player should have landed; y=%f", g.Player.Pos.Y)
	}
	// Landing this very tick consumes the buffered jump.
	for i := 0; i < 10; i++ {
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
	for i := 0; i < 400 && g.State == StatePlaying; i++ {
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
	for i := 0; i < 30; i++ {
		g.Update(Input{Up: true})
	}
	if g.Player.Pos.Y < 11 {
		t.Errorf("player passed through ceiling: y=%f", g.Player.Pos.Y)
	}
}
