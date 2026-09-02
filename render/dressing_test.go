package render

import (
	"testing"

	"github.com/Daviey/mario/engine"
)

// A bush or hill anchored on the last solid column before a pit must not
// spill its right half over the gap: the overhang reads as solid footing
// exactly where a jump is required (live report 2026-09-02). The gates
// used to check the anchor column only.
func TestDressingNeverOverhangsPit(t *testing.T) {
	// Sweep for real hash anchors well inside a wide level.
	bushX, hillX := -1, -1
	for tx := 10; tx < 300; tx++ {
		if bushX < 0 && BushAt(tx) && !HillAt(tx) {
			bushX = tx
		}
		if hillX < 0 && HillAt(tx) && !BushAt(tx) {
			hillX = tx
		}
	}
	if bushX < 0 || hillX < 0 {
		t.Fatalf("no dressing anchors found: bush=%d hill=%d", bushX, hillX)
	}

	build := func(anchor int) *engine.Game {
		b := engine.NewBuilder(320, engine.LevelHeight)
		b.Ground(0, anchor) // solid up to and including the anchor…
		// …then a five-column pit swallowing every sprite's right half.
		b.Ground(anchor+6, 319)
		b.Set(2, 12, 'M')
		b.Flag(300)
		l, err := engine.ParseLevel("t", b.Rows())
		if err != nil {
			t.Fatalf("ParseLevel: %v", err)
		}
		g := engine.NewGame([]*engine.Level{l}, anchor+10, engine.LevelHeight)
		g.State = engine.StatePlaying
		return g
	}

	// Bush band: the two pixel rows just above the ground surface. Any
	// non-sky pixel there at the pit columns is dressing overhang.
	for _, tc := range []struct {
		name    string
		anchor  int
		span    int
		topRows int // sprite rows above the ground surface
	}{
		{"bush", bushX, sprW(sprBush) / Pix, sprH(sprBush)},
		{"hill", hillX, sprW(sprHill) / Pix, sprH(sprHill)},
	} {
		g := build(tc.anchor)
		s := Render(g, testPal)
		pit0 := (tc.anchor + 1) * Pix
		pit1 := (tc.anchor + 6) * Pix
		for x := pit0; x < pit1; x++ {
			for y := engine.GroundTop*Pix - tc.topRows; y < engine.GroundTop*Pix; y++ {
				if got := worldPx(s, x, y); got != testPal.Sky {
					t.Fatalf("%s overhangs the pit: pixel at (%d,%d) = %+v", tc.name, x, y, got)
				}
			}
		}
	}
}

// The suppression must not be total: on flat ground the dressing still
// draws (non-vacuous, the cloud-suppression convention).
func TestDressingStillDrawsOnFlatGround(t *testing.T) {
	b := engine.NewBuilder(320, engine.LevelHeight)
	b.Ground(0, 319)
	b.Set(2, 12, 'M')
	b.Flag(300)
	l, err := engine.ParseLevel("t", b.Rows())
	if err != nil {
		t.Fatalf("ParseLevel: %v", err)
	}
	g := engine.NewGame([]*engine.Level{l}, 320, engine.LevelHeight)
	g.State = engine.StatePlaying
	s := Render(g, testPal)

	count := func(topRows int) int {
		n := 0
		for x := 0; x < s.W; x++ {
			for y := engine.GroundTop*Pix - topRows; y < engine.GroundTop*Pix; y++ {
				if worldPx(s, x, y) != testPal.Sky {
					n++
					break // one hit per column is enough
				}
			}
		}
		return n
	}
	if n := count(sprH(sprBush)); n == 0 {
		t.Error("no bush drawn anywhere on flat ground")
	}
	if n := count(sprH(sprHill)); n == 0 {
		t.Error("no hill drawn anywhere on flat ground")
	}
}
