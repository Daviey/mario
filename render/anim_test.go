package render

import (
	"math"
	"strings"
	"testing"

	"github.com/Daviey/mario/engine"
)

func sameArt(a, b []string) bool {
	return len(a) == len(b) && strings.Join(a, "|") == strings.Join(b, "|")
}

// Pose selection: jump in the air, skid on reverse turns, stand when idle,
// and the walk cycle keyed off distance travelled while running.
func TestMarioArtPoses(t *testing.T) {
	smallStand := marioSmallWalk[0]
	pl := &engine.Player{Grounded: true, Facing: 1}
	if got := marioArt(pl); !sameArt(got, smallStand) {
		t.Error("idle small player must use the stand frame")
	}
	if got := marioArt(&engine.Player{Grounded: true, Super: true}); !sameArt(got, sprMarioSuper) {
		t.Error("idle super player must use the super stand frame")
	}
	if got := marioArt(&engine.Player{}); !sameArt(got, sprMarioSmallJump) {
		t.Error("airborne small player must use the jump pose")
	}
	if got := marioArt(&engine.Player{Super: true}); !sameArt(got, sprMarioSuperJump) {
		t.Error("airborne super player must use the jump pose")
	}
	skid := &engine.Player{Grounded: true, Skidding: true, Vel: engine.Vec{X: 0.05}}
	if got := marioArt(skid); !sameArt(got, sprMarioSmallSkid) {
		t.Error("skidding player must use the skid pose")
	}
	if got := marioArt(&engine.Player{Grounded: true, Super: true, Skidding: true,
		Vel: engine.Vec{X: -0.05}}); !sameArt(got, sprMarioSuperSkid) {
		t.Error("skidding super player must use the super skid pose")
	}
}

func TestWalkCycleFrameOrder(t *testing.T) {
	base := &engine.Player{Grounded: true, Vel: engine.Vec{X: 0.05}}
	want := []int{0, 1, 2, 0}
	for i, phase := range want {
		base.WalkDist = float64(i) * engine.WalkFrameLen
		got := marioArt(base)
		if !sameArt(got, marioSmallWalk[phase]) {
			t.Errorf("walk dist %f: frame %d, want cycle index %d", base.WalkDist, i, phase)
		}
	}
	// Half-way through a frame keeps the current art (truncating clock).
	base.WalkDist = 1.5 * engine.WalkFrameLen
	if got := marioArt(base); !sameArt(got, marioSmallWalk[1]) {
		t.Error("mid-frame distance must hold the current frame")
	}
}

// The three walk frames must actually be distinct pixel grids, otherwise
// the cycle degenerates into a static sprite.
func TestWalkFramesAreDistinct(t *testing.T) {
	for _, frames := range [][][]string{marioSmallWalk, marioSuperWalk} {
		for i := range frames {
			for j := i + 1; j < len(frames); j++ {
				if sameArt(frames[i], frames[j]) {
					t.Errorf("walk frames %d and %d are identical", i, j)
				}
			}
		}
	}
}

// Every art row must match its sprite's width and only use palette runes.
func TestMarioArtRowsWellFormed(t *testing.T) {
	arts := map[string][]string{
		"smallStride": sprMarioSmallStride, "smallPass": sprMarioSmallPass,
		"smallJump": sprMarioSmallJump, "smallSkid": sprMarioSmallSkid,
		"superStride": sprMarioSuperStride, "superPass": sprMarioSuperPass,
		"superJump": sprMarioSuperJump, "superSkid": sprMarioSuperSkid,
	}
	for name, art := range arts {
		w := sprW(art)
		for row, line := range art {
			if len([]rune(line)) != w {
				t.Errorf("%s row %d width %d, want %d", name, row, len([]rune(line)), w)
			}
			for _, r := range line {
				if r != '.' && !strings.ContainsRune("RSBD", r) {
					t.Errorf("%s row %d has off-palette rune %q", name, row, r)
				}
			}
		}
	}
}

// Visual check of the full pose sheet: go test -run TestDumpWalkCycle -v
func TestDumpWalkCycle(t *testing.T) {
	if testing.Short() {
		t.Skip("visual dump")
	}
	rc := runeColors(testPal)
	sheet := func(arts ...[]string) *Frame {
		f := NewFrame(9+(len(arts))*(7+3), 15, testPal.Sky)
		x := 9
		for _, a := range arts {
			f.DrawSprite(a, rc, x, 14-sprH(a), false, 1)
			x += sprW(a) + 3
		}
		return f
	}
	t.Log("\nsmall: stand stride pass jump skid\n" + dumpFrame(sheet(
		marioSmallWalk[0], sprMarioSmallStride, sprMarioSmallPass,
		sprMarioSmallJump, sprMarioSmallSkid)))
	t.Log("\nsuper: stand stride pass jump skid\n" + dumpFrame(sheet(
		marioSuperWalk[0], sprMarioSuperStride, sprMarioSuperPass,
		sprMarioSuperJump, sprMarioSuperSkid)))
}

// End-to-end through worldFrame: with position and camera pinned, cycling
// WalkDist must change the stamped pixels — the animation is really drawn.
func TestWalkCycleStampsDistinctPixels(t *testing.T) {
	g := newGame(t)
	pl := g.Player
	pl.Pos.X, pl.Grounded, pl.Vel.X = 5, true, 0.05
	seen := map[string]bool{}
	for _, d := range []float64{0, 1, 2} {
		pl.WalkDist = float64(d) * engine.WalkFrameLen
		f := worldFrame(g, testPal)
		var sb strings.Builder
		art := marioArt(pl)
		bottom := int(math.Round((pl.Pos.Y + pl.H - CameraY(g)) * Pix))
		cx := int(math.Round(pl.Pos.X*Pix)) + int(pl.W*Pix)/2
		for y := bottom - sprH(art); y < bottom; y++ {
			for x := cx - 4; x <= cx+4; x++ {
				c := f.At(x, y).RGB
				sb.WriteByte(byte(c >> 16))
				sb.WriteByte(byte(c >> 8))
				sb.WriteByte(byte(c))
			}
		}
		seen[sb.String()] = true
	}
	if len(seen) != 3 {
		t.Errorf("world frame showed %d distinct walk poses, want 3", len(seen))
	}
}
