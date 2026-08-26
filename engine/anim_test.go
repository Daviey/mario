package engine

import "testing"

// The walk-cycle clock advances with ground distance while moving (including
// the friction slide after the key releases) and freezes once fully stopped.
func TestWalkDistAdvancesOnlyWhileMoving(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	run(g, 30, Input{Right: true})
	moved := g.Player.WalkDist
	if moved <= 0 {
		t.Fatalf("walk dist = %f after walking right, want > 0", moved)
	}
	for range 120 {
		if g.Player.Vel.X == 0 {
			break
		}
		g.Update(Input{})
	}
	if g.Player.Vel.X != 0 {
		t.Fatalf("velocity %f never bled off", g.Player.Vel.X)
	}
	frozen := g.Player.WalkDist
	run(g, 20, Input{})
	if g.Player.WalkDist != frozen {
		t.Errorf("walk dist = %f after idling, want frozen at %f", g.Player.WalkDist, frozen)
	}
}

// Skidding is flagged exactly while input direction opposes ground velocity.
func TestSkidFlagTracksTurnAgainstMotion(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	run(g, 40, Input{Right: true})
	if g.Player.Skidding {
		t.Fatal("skidding while running with the input")
	}
	run(g, 1, Input{Left: true})
	if !g.Player.Skidding {
		t.Fatal("no skid flag on the first tick of a reverse turn")
	}
	if g.Player.Facing != -1 {
		t.Errorf("facing = %d during skid, want -1 (input direction)", g.Player.Facing)
	}
	run(g, 90, Input{Left: true})
	if g.Player.Skidding {
		t.Error("skid flag stuck after velocity aligned with input")
	}
}

func countDust(g *Game) int {
	n := 0
	for _, p := range g.Particles {
		if p.Kind == ParticleDust {
			n++
		}
	}
	return n
}

// Jump liftoff sets the stretch timer and kicks up two dust puffs.
func TestJumpStretchAndDust(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	run(g, 5, Input{})
	g.Update(Input{Up: true})
	if g.Player.StretchT <= 0 {
		t.Fatalf("stretch timer = %d after liftoff, want > 0", g.Player.StretchT)
	}
	if countDust(g) < 2 {
		t.Errorf("dust puffs = %d after liftoff, want >= 2", countDust(g))
	}
	run(g, 10, Input{Up: true})
	if g.Player.StretchT != 0 {
		t.Errorf("stretch timer = %d after 10 ticks, want decayed to 0", g.Player.StretchT)
	}
}

// A full jump arc ends in a squash pose and landing dust.
func TestHardLandingSquashAndDust(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	run(g, 5, Input{})
	g.Update(Input{Up: true})
	for i := range 120 {
		if i > 2 && g.Player.Grounded {
			break
		}
		g.Update(Input{})
	}
	if !g.Player.Grounded {
		t.Fatal("player never landed")
	}
	if g.Player.SquashT <= 0 {
		t.Fatalf("squash timer = %d after hard landing, want > 0", g.Player.SquashT)
	}
	if countDust(g) < 2 {
		t.Errorf("dust puffs = %d after landing, want >= 2", countDust(g))
	}
	run(g, 10, Input{})
	if g.Player.SquashT != 0 {
		t.Errorf("squash timer = %d after 10 ticks, want decayed to 0", g.Player.SquashT)
	}
}

// Skidding kicks up dust periodically while the slide lasts.
func TestSkidKicksUpDust(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	run(g, 40, Input{Right: true})
	run(g, 15, Input{Left: true})
	if countDust(g) == 0 {
		t.Error("no dust puffs during a reverse skid")
	}
}

// Enemies accumulate walk distance while alive, driving their waddle.
func TestEnemyWalkClockAdvances(t *testing.T) {
	g := newGame(t, buildLevel(t, 60, func(b *Builder) {
		b.Set(20, 12, 'G')
	}))
	var e *Enemy
	for _, en := range g.Enemies {
		e = en
	}
	if e == nil {
		t.Fatal("no goomba in level")
	}
	run(g, 30, Input{})
	if e.WalkDist <= 0 {
		t.Errorf("enemy walk dist = %f after 30 ticks, want > 0", e.WalkDist)
	}
}
