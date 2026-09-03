package engine

import "testing"

// heaven builds a small Coin-Heaven-style room: a floor that ends at
// column 17 (open sky rightward), a few coins, and a drop exit that
// returns to the main level at the given column.
func heaven(t *testing.T, dropX int) *Level {
	t.Helper()
	b := NewBuilder(26, LevelHeight)
	b.Ground(0, 17)
	b.Coins(10, 2, 3, 4, 5)
	l := mustLevel("heaven", b)
	l.DropExitX = dropX
	return l
}

// grabVine stages the player on the ground overlapping the vine column
// and feeds the rising Up press that grabs the stalk.
func grabVine(t *testing.T, g *Game, x int) {
	t.Helper()
	run(g, 2, Input{}) // settle so prevIn is clean
	g.Player.Pos = Vec{float64(x) + 0.3, 13 - g.Player.H}
	g.Player.Vel = Vec{}
	run(g, 2, Input{}) // land grounded under the stalk
	if !g.Player.Grounded {
		t.Fatal("setup: player not grounded under the vine")
	}
	g.Update(Input{Up: true})
	if !g.Player.Climbing {
		t.Fatal("setup: Up press did not grab the vine")
	}
}

func TestVineBrickSproutsOnBump(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Set(20, 9, 'J') })
	g := newGame(t, l)
	if g.vine != nil {
		t.Fatal("no vine before the bump")
	}
	bumpUnder(t, g, 20, 9)
	if got := g.Level.At(20, 9); got != Used {
		t.Fatalf("brick after bump = %v, want Used", got)
	}
	if g.vine == nil || g.vine.X != 20 || g.vine.BaseY != 9 {
		t.Fatalf("vine after bump = %+v, want sprouted at (20,9)", g.vine)
	}
	run(g, 30, Input{})
	if g.vine.GrowTop != VineTopRow {
		t.Fatalf("GrowTop after growth = %d, want %d", g.vine.GrowTop, VineTopRow)
	}
}

func TestSuperBumpSproutsVineNotBreaks(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Set(20, 9, 'J') })
	g := newGame(t, l)
	g.Player.Power = PowerSuper
	g.Player.H = SuperH
	g.Player.Pos.Y = 13 - SuperH
	bumpUnder(t, g, 20, 9)
	if got := g.Level.At(20, 9); got != Used {
		t.Fatalf("super bump: tile = %v, want Used (the vine brick never breaks)", got)
	}
	if g.vine == nil {
		t.Fatal("super bump: no vine sprouted")
	}
}

func TestGrabAndClimbToHeaven(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Set(20, 9, 'J') })
	l.VineRoom = heaven(t, 40)
	g := newGame(t, l)
	bumpUnder(t, g, 20, 9)
	run(g, 30, Input{}) // grow to the crown

	grabVine(t, g, 20)
	// Feet snapped onto the spent brick's top at the grab.
	if got := g.Player.Pos.Y + g.Player.H; got != 9 {
		t.Fatalf("grab: feet at %v, want the brick top (9)", got)
	}
	run(g, 600, Input{Up: true}) // 8 tiles at 1/32 per tick = 256 ticks to the crown
	if !g.inRoom || g.Level.Width != 26 {
		t.Fatalf("after the climb: inRoom=%v width=%d, want the heaven room", g.inRoom, g.Level.Width)
	}
	if n := len(g.CoinItems); n != 4 {
		t.Fatalf("heaven coins = %d, want 4", n)
	}
	if !g.Player.Grounded || g.Player.Climbing {
		t.Fatalf("arrival: grounded=%v climbing=%v, want standing", g.Player.Grounded, g.Player.Climbing)
	}

	// The heaven's right edge is open sky: running off the floor drops
	// out of the room and back into the main level, falling.
	for range 300 {
		if !g.inRoom {
			break
		}
		g.Update(Input{Right: true})
	}
	if g.inRoom || g.Level.Width != 60 {
		t.Fatalf("after the drop: inRoom=%v width=%d, want the main level back", g.inRoom, g.Level.Width)
	}
	if got := int(g.Player.Pos.X); got != 40 {
		t.Fatalf("drop landed at x=%d, want the DropExitX column 40", got)
	}
	run(g, 120, Input{})
	if !g.Player.Grounded {
		t.Fatal("the drop must land: player still falling after 120 ticks")
	}
}

func TestVineSurvivesWarpWorldSwap(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Set(20, 9, 'J') })
	h := heaven(t, 40)
	l.VineRoom = h
	g := newGame(t, l)
	bumpUnder(t, g, 20, 9)
	run(g, 30, Input{})
	top := g.vine.GrowTop

	// A pipe visit stashes the main world (vine included) and fields
	// the room's vine-less one; the return must bring the stalk back.
	w := g.stashWorld()
	g.applyWorld(g.roomFor(h))
	if g.vine != nil {
		t.Fatal("a fresh room must not inherit the main level's vine")
	}
	g.applyWorld(w)
	if g.vine == nil || g.vine.GrowTop != top {
		t.Fatalf("vine after the round-trip = %+v, want preserved (GrowTop %d)", g.vine, top)
	}
}

func TestShellSmashesVineBrickNoSprout(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Set(20, 12, 'J') })
	g := newGame(t, l)
	shell := kickedShell(g, 15, 1)
	for range 300 {
		if shell.Pos.X >= 22 {
			break
		}
		g.Update(Input{})
	}
	if got := g.Level.At(20, 12); got != Empty {
		t.Fatalf("tile after shell smash = %v, want Empty", got)
	}
	if g.vine != nil {
		t.Fatal("a smashed vine brick must never sprout")
	}
}

func TestClimbDownReleasesAndHopOff(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Set(20, 9, 'J') })
	g := newGame(t, l)
	bumpUnder(t, g, 20, 9)
	run(g, 30, Input{})

	// Climb up a stretch, then ride Down back to the brick and step off.
	grabVine(t, g, 20)
	run(g, 100, Input{Up: true})
	if !g.Player.Climbing {
		t.Fatal("climb: fell off the vine mid-ascent")
	}
	run(g, 400, Input{Down: true})
	if g.Player.Climbing {
		t.Fatal("descend to the base must step off the vine")
	}
	if got := g.Player.Pos.Y + g.Player.H; got != 9 {
		t.Fatalf("step-off feet at %v, want standing on the brick (9)", got)
	}
	if !g.Player.Grounded {
		t.Fatal("step-off must be grounded on the spent brick")
	}

	// Grab again, climb, and hop off with the run key mid-stalk.
	g.Update(Input{Up: true})
	if !g.Player.Climbing {
		t.Fatal("re-grab failed from the brick top")
	}
	run(g, 60, Input{Up: true})
	g.Update(Input{Up: true, Run: true})
	if g.Player.Climbing {
		t.Fatal("run-key press must hop off the vine")
	}
	if g.Player.Vel.Y >= 0 {
		t.Fatalf("hop-off velocity Y = %v, want upward", g.Player.Vel.Y)
	}
}

func TestBareVineTopsOut(t *testing.T) {
	// No VineRoom on the level: the stalk still grows and climbs, and
	// the crown simply holds the player (custom-level contract).
	l := buildLevel(t, 60, func(b *Builder) { b.Set(20, 9, 'J') })
	g := newGame(t, l)
	bumpUnder(t, g, 20, 9)
	run(g, 30, Input{})
	grabVine(t, g, 20)
	run(g, 600, Input{Up: true})
	if g.inRoom {
		t.Fatal("a bare vine must not warp anywhere")
	}
	if !g.Player.Climbing {
		t.Fatal("a bare crown holds the player on the stalk")
	}
	if got := g.Player.Pos.Y; got < float64(VineTopRow)-0.01 || got > float64(VineTopRow)+VineClimbSpeed {
		t.Fatalf("crown clamp: Pos.Y = %v, want ~%d", got, VineTopRow)
	}
}

func TestDeathMidClimbResets(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Set(20, 9, 'J') })
	g := newGame(t, l)
	bumpUnder(t, g, 20, 9)
	run(g, 30, Input{})
	grabVine(t, g, 20)
	run(g, 50, Input{Up: true})
	g.kill()
	if g.Player.Climbing {
		t.Fatal("death must clear the climbing flag")
	}
}
