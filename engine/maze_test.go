package engine

import "testing"

func mazeGame(t *testing.T, upperOK bool) *Game {
	t.Helper()
	l := buildLevel(t, 60, func(b *Builder) { b.Maze(20, 40, upperOK) })
	return newGame(t, l)
}

// driveTier walks the player right across X1 standing on a tier: low
// (row 9) or high (row 6), staged mid-corridor.
func driveTier(t *testing.T, g *Game, tier int) {
	t.Helper()
	g.Player.Pos = Vec{28, float64(tier) - g.Player.H}
	g.Player.Vel = Vec{}
	for i := 0; i < 240 && g.Player.Pos.X < 41; i++ {
		g.Update(Input{Right: true, Run: true})
	}
}

func TestMazeWrongTierLoopsCorridor(t *testing.T) {
	// The low way is correct: the high tier loops. The drive holds
	// right through the loop, so the snap's fall lands on the low
	// tier's edge and the run retries — the loop must have happened,
	// and the eventual crossing must be on the correct tier.
	g := mazeGame(t, false)
	g.Player.Pos = Vec{28, 6 - g.Player.H}
	g.Player.Vel = Vec{}
	snaps := 0
	prev := g.Player.Pos.X
	for i := 0; i < 300 && g.Player.Pos.X < 41; i++ {
		g.Update(Input{Right: true, Run: true})
		if prev-g.Player.Pos.X > 10 { // a corridor loop, not friction
			snaps++
		}
		prev = g.Player.Pos.X
	}
	if snaps == 0 {
		t.Fatal("the high tier crossed without looping the corridor")
	}
	if g.Player.Pos.X < 41 {
		t.Fatalf("the retry stalled at x=%.1f — the loop must leave the corridor playable", g.Player.Pos.X)
	}
	if feet := g.Player.Pos.Y + g.Player.H; feet < 8.5 || feet > 9.5 {
		t.Fatalf("final crossing on feet=%.1f — the retry must take the low tier (9)", feet)
	}
}

func TestMazeRightTierPasses(t *testing.T) {
	g := mazeGame(t, false)
	driveTier(t, g, 9)
	if g.Player.Pos.X < 41 {
		t.Fatalf("low tier stopped at x=%.1f — the correct route must pass", g.Player.Pos.X)
	}
}

func TestMazeUpperZoneMirrors(t *testing.T) {
	// UpperOK zone: the high tier passes, the low loops.
	g := mazeGame(t, true)
	driveTier(t, g, 9)
	if g.Player.Pos.X >= 41 {
		t.Fatalf("low tier crossed at x=%.1f — the corridor must loop", g.Player.Pos.X)
	}
	g = mazeGame(t, true)
	driveTier(t, g, 6)
	if g.Player.Pos.X < 41 {
		t.Fatalf("high tier stopped at x=%.1f — the correct route must pass", g.Player.Pos.X)
	}
}

// TestMazeLoopKeepsPlaying: after a loop the player is back at the
// entry, grounded, and can take the other tier — the loop is a detour,
// never a trap.
func TestMazeLoopKeepsPlaying(t *testing.T) {
	g := mazeGame(t, false)
	driveTier(t, g, 6)
	run(g, 60, Input{}) // fall in at the entry
	if !g.Player.Grounded {
		t.Fatal("player airborne a full second after the loop")
	}
	driveTier(t, g, 9)
	if g.Player.Pos.X < 41 {
		t.Fatalf("second attempt on the right tier stopped at x=%.1f", g.Player.Pos.X)
	}
}

// TestDefaultLevelsFlagReachable already proves the shipped maze
// castles (4-4 retrofit, 7-4) solvable end to end; TestMazeBakesTier
// pins the geometry contract the planner relies on: both tiers exist
// and the wall blocks the ground.
func TestMazeBakesTiers(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Maze(20, 40, false) })
	for x := 24; x <= 26; x++ {
		if !l.At(x, 12).Solid() || !l.At(x, 10).Solid() {
			t.Fatalf("wall missing at (%d,10..12)", x)
		}
	}
	for x := 22; x <= 41; x++ {
		if !l.At(x, 9).Solid() {
			t.Fatalf("low tier gap at (%d,9)", x)
		}
	}
	for x := 27; x <= 41; x++ {
		if !l.At(x, 6).Solid() {
			t.Fatalf("high tier gap at (%d,6)", x)
		}
	}
	if len(l.MazeZones) != 1 || l.MazeZones[0].X0 != 20 || l.MazeZones[0].X1 != 40 {
		t.Fatalf("zone = %+v, want span 20..40", l.MazeZones)
	}
}
