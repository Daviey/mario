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
