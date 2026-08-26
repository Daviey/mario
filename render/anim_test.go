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
	if got := marioArt(&engine.Player{Grounded: true, Power: engine.PowerSuper}); !sameArt(got, sprMarioSuper) {
		t.Error("idle super player must use the super stand frame")
	}
	if got := marioArt(&engine.Player{}); !sameArt(got, sprMarioSmallJump) {
		t.Error("airborne small player must use the jump pose")
	}
	if got := marioArt(&engine.Player{Power: engine.PowerSuper}); !sameArt(got, sprMarioSuperJump) {
		t.Error("airborne super player must use the jump pose")
	}
	skid := &engine.Player{Grounded: true, Skidding: true, Vel: engine.Vec{X: 0.05}}
	if got := marioArt(skid); !sameArt(got, sprMarioSmallSkid) {
		t.Error("skidding player must use the skid pose")
	}
	if got := marioArt(&engine.Player{Grounded: true, Power: engine.PowerSuper, Skidding: true,
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
		"smallSquash": sprMarioSmallSquash, "smallStretch": sprMarioSmallStretch,
		"smallDead":   sprMarioDead,
		"superStride": sprMarioSuperStride, "superPass": sprMarioSuperPass,
		"superJump": sprMarioSuperJump, "superSkid": sprMarioSuperSkid,
		"superSquash": sprMarioSuperSquash, "superStretch": sprMarioSuperStretch,
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
	t.Log("\nsmall: stand stride pass jump skid squash stretch dead\n" + dumpFrame(sheet(
		marioSmallWalk[0], sprMarioSmallStride, sprMarioSmallPass,
		sprMarioSmallJump, sprMarioSmallSkid,
		sprMarioSmallSquash, sprMarioSmallStretch, sprMarioDead)))
	t.Log("\nsuper: stand stride pass jump skid squash stretch\n" + dumpFrame(sheet(
		marioSuperWalk[0], sprMarioSuperStride, sprMarioSuperPass,
		sprMarioSuperJump, sprMarioSuperSkid,
		sprMarioSuperSquash, sprMarioSuperStretch)))
	t.Log("\nenemies: goomba goombaWalk koopa koopaWalk\n" + dumpFrame(sheet(
		sprGoomba, sprGoombaWalk, sprKoopa, sprKoopaWalk)))
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

// Liftoff stretch outranks the jump pose; landing squash outranks skid.
func TestMarioArtStretchSquash(t *testing.T) {
	rising := &engine.Player{StretchT: 4}
	if got := marioArt(rising); !sameArt(got, sprMarioSmallStretch) {
		t.Error("rising small player must use the stretch pose")
	}
	if got := marioArt(&engine.Player{Power: engine.PowerSuper, StretchT: 4}); !sameArt(got, sprMarioSuperStretch) {
		t.Error("rising super player must use the super stretch pose")
	}
	landed := &engine.Player{Grounded: true, SquashT: 4, Vel: engine.Vec{X: 0.05}, Skidding: true}
	if got := marioArt(landed); !sameArt(got, sprMarioSmallSquash) {
		t.Error("landed player must use the squash pose over skid")
	}
	if got := marioArt(&engine.Player{Grounded: true, Power: engine.PowerSuper, SquashT: 4}); !sameArt(got, sprMarioSuperSquash) {
		t.Error("landed super player must use the super squash pose")
	}
}

// regionHash fingerprints a pixel rect of a rendered frame.
func regionHash(f *Frame, x0, y0, x1, y1 int) string {
	var sb strings.Builder
	for y := y0; y < y1; y++ {
		for x := x0; x < x1; x++ {
			c := f.At(x, y).RGB
			sb.WriteByte(byte(c >> 16))
			sb.WriteByte(byte(c >> 8))
			sb.WriteByte(byte(c))
		}
	}
	return sb.String()
}

func playerRegionHash(g *engine.Game) string {
	f := worldFrame(g, testPal)
	pl := g.Player
	art := marioArt(pl)
	bottom := int(math.Round((pl.Pos.Y + pl.H - CameraY(g)) * Pix))
	cx := int(math.Round(pl.Pos.X*Pix)) + int(pl.W*Pix)/2
	return regionHash(f, cx-4, bottom-sprH(art), cx+5, bottom)
}

// Dying swaps in the death pose through the real drawPlayerPx path.
func TestDyingPoseDrawn(t *testing.T) {
	g := newGame(t)
	g.Player.Pos.X, g.Player.Grounded = 5, true
	g.State = engine.StatePlaying
	alive := playerRegionHash(g)
	g.State = engine.StateDying
	dead := playerRegionHash(g)
	if alive == dead {
		t.Error("dying player rendered identically to standing player")
	}
}

// Enemies waddle: their stamped pixels change with walk distance.
func TestEnemyWaddleDrawn(t *testing.T) {
	g := newGame(t) // helper level has a goomba at (14,12) and a koopa at (17,12)
	var e *engine.Enemy
	for _, en := range g.Enemies {
		if en.Kind == engine.KindGoomba {
			e = en
		}
	}
	if e == nil {
		t.Fatal("no goomba in helper level")
	}
	hash := func() string {
		f := worldFrame(g, testPal)
		cx := int((e.Pos.X + e.W/2) * Pix)
		bottom := int((e.Pos.Y + e.H - CameraY(g)) * Pix)
		return regionHash(f, cx-4, bottom-6, cx+5, bottom)
	}
	e.WalkDist = 0
	stepA := hash()
	e.WalkDist = engine.EnemyFrameLen
	if hash() == stepA {
		t.Error("goomba pixels identical across waddle frames")
	}
}

// The title cast walks in place: frames differ within the blink-steady
// window, so only the cast animation can explain the change.
func TestTitleCastAnimates(t *testing.T) {
	g := engine.NewGame(engine.DefaultLevels(), 30, 12)
	g.Tick = 0
	a := regionHash(worldFrame(g, testPal), 0, 0, g.ViewW*Pix, g.ViewH*Pix)
	g.Tick = 10
	b := regionHash(worldFrame(g, testPal), 0, 0, g.ViewW*Pix, g.ViewH*Pix)
	if a == b {
		t.Error("title frames identical 10 ticks apart (cast not animating)")
	}
}
