package render

import (
	"testing"

	"github.com/Daviey/mario/engine"
)

// TestVineStalkVisible pins the beanstalk's read: before the bump the
// vine brick's column is open sky; after the bump and growth the stalk
// paints green pixels the whole way up, or the climb invite is
// invisible (the same failure class as the springboard's invisible
// squash, 2026-09-02).
func TestVineStalkVisible(t *testing.T) {
	b := engine.NewBuilder(80, engine.LevelHeight)
	b.Ground(0, 79)
	b.Set(20, 9, 'J')
	l, err := engine.ParseLevel("vine", b.Rows())
	if err != nil {
		t.Fatal(err)
	}
	g := engine.NewGame([]*engine.Level{l}, 40, engine.LevelHeight)
	g.State = engine.StatePlaying

	before := worldFrame(nil, g, testPal)

	// Bump the vine brick from below through the real physics.
	g.Player.Pos = engine.Vec{X: 20.15, Y: 13 - g.Player.H}
	g.Player.Vel = engine.Vec{}
	for range 60 {
		g.Update(engine.Input{Up: true})
	}
	if g.Vine() == nil {
		t.Fatal("setup: the bump did not sprout the vine")
	}
	for range 30 {
		g.Update(engine.Input{})
	}
	if g.Vine().GrowTop != engine.VineTopRow {
		t.Fatalf("setup: GrowTop = %d, want %d", g.Vine().GrowTop, engine.VineTopRow)
	}

	// The camera has followed the player to the brick: sample the stem
	// column from the live camera (drawVine subtracts camX), not tile 0.
	camY := CameraY(g)
	stemX := int((20.5-g.CameraX)*Pix) - 4/2 + 1
	rowY := func(row int) int { return int((float64(row) - camY) * Pix) }
	if got := before.At(stemX, rowY(5)); got != testPal.Sky {
		t.Fatalf("pre-bump sky at the stalk column = %+v, want sky", got)
	}

	after := worldFrame(nil, g, testPal)
	pal := paletteFor(g, testPal)
	for _, row := range []int{1, 3, 5, 7} {
		if got := after.At(stemX, rowY(row)+2); got != pal.GreenDark {
			t.Errorf("stalk stem at row %d = %+v, want pal.GreenDark", row, got)
		}
	}
}
