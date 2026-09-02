package render

import (
	"testing"

	"github.com/Daviey/mario/engine"
)

// buildWarpScene returns a game mid-pipe-warp on a purpose-built level:
// a warp pipe at x=10 leading to a 20-tile underground room whose entry
// pipe is at x=3.
func buildWarpScene(t *testing.T, viewW int) *engine.Game {
	t.Helper()
	mainB := engine.NewBuilder(50, engine.LevelHeight)
	mainB.Ground(0, 49)
	mainB.Pipe(10, 2)
	mainB.Set(2, 12, 'M')
	mainB.Flag(40)
	main, err := engine.ParseLevel("wmain", mainB.Rows())
	if err != nil {
		t.Fatalf("ParseLevel main: %v", err)
	}
	roomB := engine.NewBuilder(20, engine.LevelHeight)
	roomB.Ground(0, 19)
	roomB.Ceiling()
	roomB.Pipe(3, 2)
	roomB.Pipe(16, 2)
	roomB.Coins(11, 8, 9, 10)
	room, err := engine.ParseLevel("wroom", roomB.Rows())
	if err != nil {
		t.Fatalf("ParseLevel room: %v", err)
	}
	room.Theme = engine.ThemeUnderground
	room.FlagX = -1
	main.Warps = []engine.Warp{{X: 10, Top: engine.GroundTop - 2, Dest: room, DestX: 3, DestTop: engine.GroundTop - 2}}
	room.Warps = []engine.Warp{{X: 16, Top: engine.GroundTop - 2, Dest: nil, DestX: 10, DestTop: engine.GroundTop - 2}}
	g := engine.NewGame([]*engine.Level{main}, viewW, engine.LevelHeight)
	g.State = engine.StatePlaying
	// Stand on the warp mouth and sink for half the animation.
	p := g.Player
	p.Pos = engine.Vec{X: 10.6, Y: float64(engine.GroundTop-2) - p.H}
	p.Vel = engine.Vec{}
	p.Grounded = true
	g.Update(engine.Input{Down: true})
	for range engine.PipeWarpTicks / 2 {
		g.Update(engine.Input{})
	}
	return g
}

// During the sink the player draws UNDER the pipe: no player pixel may
// survive where pipe tiles are, and the pipe rim stays pipe-coloured.
func TestPipeWarpSinkOccludedByPipe(t *testing.T) {
	g := buildWarpScene(t, 40)
	if g.State != engine.StatePipeIn {
		t.Fatalf("scene state = %s, want pipe-in", g.State)
	}
	s := Render(g, testPal)
	// The mouth spans tile columns 10-11, rows GroundTop-2..GroundTop-1.
	for x := 10 * Pix; x < 12*Pix; x++ {
		for y := (engine.GroundTop - 2) * Pix; y < engine.GroundTop*Pix; y++ {
			if got := worldPx(s, x, y); got == testPal.Player || got == testPal.Overall {
				t.Fatalf("player pixel visible through the pipe at (%d,%d)", x, y)
			}
		}
	}
	// The pipe itself is still there, rim and all.
	// The pipe itself is still there.
	if got := worldPx(s, 10*Pix+1, (engine.GroundTop-2)*Pix+1); got != testPal.GreenDark {
		t.Fatalf("pipe rim pixel = %+v, want dark green (E rune)", got)
	}
}

// After the warp the room renders in the underground palette (black
// sky) and the player rises out of the entry pipe visibly.
func TestPipeWarpRoomUndergroundPalette(t *testing.T) {
	g := buildWarpScene(t, 40)
	for range 4 * engine.PipeWarpTicks {
		g.Update(engine.Input{})
		if g.State == engine.StatePlaying {
			break
		}
	}
	if g.State != engine.StatePlaying {
		t.Fatalf("warp never finished: %s", g.State)
	}
	s := Render(g, testPal)
	ox := int(g.CameraX * Pix)
	// Underground sky is black — pick an open pixel above the floor,
	// clear of ceiling, pipes and coins.
	if got := worldPx(s, 9*Pix+1-ox, 6*Pix); got.RGB != 0 {
		t.Fatalf("room sky pixel = %+v, want underground black (RGB=0)", got)
	}
	if got := worldPx(s, 4*Pix+1-ox, (engine.GroundTop-3)*Pix); got != testPal.Player {
		t.Fatalf("player cap pixel on the mouth = %+v, want player red", got)
	}
}
