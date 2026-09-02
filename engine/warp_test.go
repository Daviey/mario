package engine

import "testing"

// buildRoom builds a flagless underground warp room: brick box, entry
// pipe on the left, exit pipe on the right, coins at head height so a
// plain walk right collects them (the real 1-1 cellar asks for hops).
func buildRoom(t *testing.T) *Level {
	t.Helper()
	b := NewBuilder(30, LevelHeight)
	b.Theme(ThemeUnderground)
	b.Ground(0, 29)
	b.Ceiling()
	for y := 1; y < GroundTop; y++ {
		b.Set(0, y, 'B')
		b.Set(29, y, 'B')
	}
	b.Pipe(4, 2)
	b.Pipe(20, 2)
	b.Coins(12, 8, 9, 10, 11, 12, 13)
	l, err := ParseLevel("room", b.Rows())
	if err != nil {
		t.Fatalf("ParseLevel room: %v", err)
	}
	if l.FlagX != -1 {
		t.Fatalf("room flagX = %d, want flagless -1", l.FlagX)
	}
	return l
}

// buildWarpWorld returns a game playing a main level whose pipe at x=10
// (h=2) warps to the room's entry pipe at x=4, and the room's exit pipe
// at x=20 warps back to the main pipe at x=30 (h=3).
func buildWarpWorld(t *testing.T) (*Game, *Level, *Level) {
	t.Helper()
	main := buildLevel(t, 60, func(b *Builder) {
		b.Pipe(10, 2)
		b.Pipe(30, 3)
		b.Flag(50)
	})
	room := buildRoom(t)
	main.Warps = []Warp{{X: 10, Top: GroundTop - 2, Dest: room, DestX: 4, DestTop: GroundTop - 2}}
	room.Warps = []Warp{{X: 20, Top: GroundTop - 2, Dest: nil, DestX: 30, DestTop: GroundTop - 3}}
	g := newGame(t, main)
	return g, main, room
}

// standOnPipe teleports the player onto the mouth of a pipe.
func standOnPipe(g *Game, x, top int) {
	p := g.Player
	p.Pos = Vec{float64(x+1) - p.W/2, float64(top) - p.H}
	p.Vel = Vec{}
	p.Grounded = true
}

// takeWarp presses Down on the given mouth and ticks through both warp
// beats, failing if the state machine never returns to play.
func takeWarp(t *testing.T, g *Game, x, top int) {
	t.Helper()
	standOnPipe(g, x, top)
	g.Update(Input{Down: true})
	if g.State != StatePipeIn {
		t.Fatalf("Down on pipe %d: state=%s, want pipe-in", x, g.State)
	}
	for range 2 * PipeWarpTicks {
		g.Update(Input{})
		if g.State == StatePlaying {
			return
		}
	}
	t.Fatalf("warp never finished: state=%s", g.State)
}

// sweepRoom walks the coin row and ends with the player stopped at the
// exit pipe's wall.
func sweepRoom(t *testing.T, g *Game) {
	t.Helper()
	for range 240 {
		g.Update(Input{Right: true})
		if g.State != StatePlaying {
			t.Fatalf("walking the room left playing: %s", g.State)
		}
	}
}

func TestPipeWarpToRoomAndBack(t *testing.T) {
	g, main, room := buildWarpWorld(t)

	// Down on the mouth sinks in and the player rises in the room.
	takeWarp(t, g, 10, GroundTop-2)
	if g.Level.Name != room.Name || !g.inRoom {
		t.Fatalf("after warp: level=%s inRoom=%v, want the room", g.Level.Name, g.inRoom)
	}
	p := g.Player
	if !p.Grounded || int(p.Pos.Y+p.H+0.001) != GroundTop-2 || int(p.Pos.X+p.W/2) != 5 {
		t.Fatalf("after rise: pos=%v grounded=%v, want standing on the entry mouth", p.Pos, p.Grounded)
	}

	// Collect the coins, then leave through the exit pipe. The sweep is
	// long enough to also prove the clock resumed in the room — it holds
	// only during the animation beats (the first TicksPerTimeUnit
	// boundary after the warp lands inside it).
	if n := len(g.CoinItems); n != 6 {
		t.Fatalf("test room coin count: %d, want 6", n)
	}
	sweepRoom(t, g)
	if g.CoinCount != 6 {
		t.Fatalf("coins collected: %d, want 6", g.CoinCount)
	}
	if g.Time >= StartTime {
		t.Fatalf("room time never ticked: %d", g.Time)
	}
	roomScore := g.Score

	takeWarp(t, g, 20, GroundTop-2)
	if g.Level.Name != main.Name || g.inRoom {
		t.Fatalf("after exit warp: level=%s inRoom=%v, want the main level", g.Level.Name, g.inRoom)
	}
	if int(p.Pos.X+p.W/2) != 31 || int(p.Pos.Y+p.H+0.001) != GroundTop-3 {
		t.Fatalf("exit position: pos=%v, want standing on pipe 30's mouth", p.Pos)
	}
	if g.Score != roomScore {
		t.Fatalf("score changed across the exit warp: %d vs %d", g.Score, roomScore)
	}
}

func TestRoomStatePersistsAcrossReentry(t *testing.T) {
	g, main, _ := buildWarpWorld(t)

	// First visit: sweep the coins.
	takeWarp(t, g, 10, GroundTop-2)
	sweepRoom(t, g)
	if n := len(g.CoinItems); n != 0 {
		t.Fatalf("coins left after sweeping the room: %d", n)
	}

	// Leave, walk back, re-enter: the room must be as it was left.
	takeWarp(t, g, 20, GroundTop-2)
	if g.Level.Name != main.Name {
		t.Fatal("exit warp did not return to main")
	}
	for range 600 {
		g.Update(Input{Left: true})
	}
	takeWarp(t, g, 10, GroundTop-2)
	if n := len(g.CoinItems); n != 0 {
		t.Fatalf("room reset across re-entry: %d coins back, want the collected state", n)
	}
}

func TestRoomResetsAfterDeath(t *testing.T) {
	g, _, _ := buildWarpWorld(t)

	takeWarp(t, g, 10, GroundTop-2)
	sweepRoom(t, g)
	if n := len(g.CoinItems); n != 0 {
		t.Fatalf("setup: coins left: %d", n)
	}

	// Die in the room: the respawn rebuilds the main level and a fresh room.
	g.Update(Input{Suicide: true})
	for g.State == StateDying {
		g.Update(Input{})
	}
	if g.State == StateGameOver {
		t.Fatal("suicide drained all lives; want a respawn card")
	}
	for g.State != StatePlaying {
		g.Update(Input{}) // card counts down
	}
	if g.inRoom {
		t.Fatal("respawn left the player in the room")
	}
	takeWarp(t, g, 10, GroundTop-2)
	if n := len(g.CoinItems); n != 6 {
		t.Fatalf("room after death: %d coins, want the full 6 back", n)
	}
}

func TestCheckpointHeldThroughRoomVisit(t *testing.T) {
	g, _, _ := buildWarpWorld(t)

	// Dying inside the room respawns at the surface checkpoint, not the
	// level start and not the room's fallback column.
	g.checkpoint = 20.0
	takeWarp(t, g, 10, GroundTop-2)
	g.Update(Input{Suicide: true})
	for g.State == StateDying {
		g.Update(Input{})
	}
	for g.State != StatePlaying {
		g.Update(Input{})
	}
	if x := g.Player.Pos.X; x < 19 || x > 21 {
		t.Fatalf("death-in-room respawn x = %v, want the checkpoint column 20", x)
	}

	// Surviving the visit: the checkpoint survives the round trip too.
	g.checkpoint = 20.0
	takeWarp(t, g, 10, GroundTop-2)
	takeWarp(t, g, 20, GroundTop-2)
	if g.checkpoint != 20.0 {
		t.Fatalf("checkpoint moved by room round trip: %v", g.checkpoint)
	}
}

func TestWarpDeterministicDoubleRun(t *testing.T) {
	script := func(g *Game) (int, int, string) {
		standOnPipe(g, 10, GroundTop-2)
		g.Update(Input{Down: true})
		for range 2 * PipeWarpTicks {
			g.Update(Input{})
		}
		for range 240 {
			g.Update(Input{Right: true})
		}
		standOnPipe(g, 20, GroundTop-2)
		g.Update(Input{Down: true})
		for range 2 * PipeWarpTicks {
			g.Update(Input{})
		}
		return g.Score, g.CoinCount, g.State.String()
	}
	g1, _, _ := buildWarpWorld(t)
	s1, c1, st1 := script(g1)
	g2, _, _ := buildWarpWorld(t)
	s2, c2, st2 := script(g2)
	if s1 != s2 || c1 != c2 || st1 != st2 {
		t.Fatalf("warp runs diverged: %d/%d/%s vs %d/%d/%s", s1, c1, st1, s2, c2, st2)
	}
	if s1 == 0 || c1 == 0 {
		t.Fatal("vacuous: nothing accrued on the warp script")
	}
}

func TestCameraCentersNarrowRoom(t *testing.T) {
	g, _, room := buildWarpWorld(t)
	g.ViewW = 60 // wider than the 30-tile room
	takeWarp(t, g, 10, GroundTop-2)
	if g.Level.Name != room.Name {
		t.Fatal("not in the room")
	}
	if want := float64(room.Width-g.ViewW) / 2; g.CameraX != want {
		t.Fatalf("room camera = %v, want centred %v", g.CameraX, want)
	}
}

func TestDownOffPipeDoesNothing(t *testing.T) {
	g, _, _ := buildWarpWorld(t)
	// Standing on plain ground next to the pipe: Down must not warp.
	g.Player.Pos = Vec{13, float64(GroundTop) - g.Player.H}
	g.Player.Grounded = true
	g.Update(Input{Down: true})
	if g.State != StatePlaying {
		t.Fatalf("Down on flat ground warped: %s", g.State)
	}
	// Standing on a pipe with no warp (the exit target on main): same.
	standOnPipe(g, 30, GroundTop-3)
	g.Update(Input{Down: true})
	if g.State != StatePlaying {
		t.Fatalf("Down on a warpless pipe warped: %s", g.State)
	}
}
