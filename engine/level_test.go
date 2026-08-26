package engine

import (
	"fmt"
	"strings"
	"testing"
)

// mustParse parses rows or fails the test.
func mustParse(t *testing.T, rows ...string) *Level {
	t.Helper()
	l, err := ParseLevel("test", rows)
	if err != nil {
		t.Fatalf("ParseLevel: %v", err)
	}
	return l
}

// buildLevel makes a solid-ground level of the given width with a player
// start and a flag near the right edge, applying optional modifications.
func buildLevel(t *testing.T, w int, mods ...func(*Builder)) *Level {
	t.Helper()
	b := NewBuilder(w, LevelHeight)
	b.Ground(0, w-1)
	b.Set(2, 12, 'M')
	b.Flag(w - 5)
	for _, m := range mods {
		m(b)
	}
	return mustParse(t, b.Rows()...)
}

// newGame starts a playing game on one level with a 40-tile viewport.
func newGame(t *testing.T, l *Level) *Game {
	t.Helper()
	g := NewGame([]*Level{l}, 40, LevelHeight)
	g.State = StatePlaying
	return g
}

func run(g *Game, n int, in Input) {
	for i := 0; i < n; i++ {
		g.Update(in)
	}
}

func TestParseLevelTilesAndEntities(t *testing.T) {
	l := mustParse(t,
		"             ",
		"   B?U       ",
		"  G   c      ",
		"  M   K   F  ",
		"#############",
	)
	if l.Width != 13 || l.Height != 5 {
		t.Fatalf("dims = %dx%d, want 13x5", l.Width, l.Height)
	}
	for x, want := range map[int]Tile{3: Brick, 4: Question, 5: QuestionMush} {
		if got := l.At(x, 1); got != want {
			t.Errorf("At(%d,1) = %v, want %v", x, got, want)
		}
	}
	if len(l.GoombaSpawns) != 1 || l.KoopaSpawns[0].X != 6 {
		t.Errorf("goomba=%v koopa=%v", l.GoombaSpawns, l.KoopaSpawns)
	}
	if len(l.CoinSpawns) != 1 {
		t.Fatalf("coin spawns = %v", l.CoinSpawns)
	}
	// Coin sits centered in its tile.
	if c := l.CoinSpawns[0]; c.X != 6.2 || c.Y != 2.2 {
		t.Errorf("coin pos = %v, want (6.2 2.2)", c)
	}
	if l.FlagX != 10 {
		t.Errorf("FlagX = %d, want 11", l.FlagX)
	}
	// Entities are removed from the tile grid.
	if at := l.At(2, 3); at != Empty || l.At(2, 2) != Empty {
		t.Errorf("entity chars leaked into tiles: %v %v", at, l.At(2, 2))
	}
	// Player start keeps feet on the row's floor.
	if s := l.PlayerStart; s.X != 2 || s.Y != 3+1-SmallH {
		t.Errorf("player start = %v", s)
	}
}

func TestParseLevelPadsUnevenRows(t *testing.T) {
	l := mustParse(t, "###", "  F", "#")
	if l.Width != 3 {
		t.Fatalf("width = %d, want 3 (longest row)", l.Width)
	}
	if l.At(2, 2) != Empty {
		t.Errorf("short row not padded with empty")
	}
}

func TestParseLevelErrors(t *testing.T) {
	if _, err := ParseLevel("e", nil); err == nil {
		t.Error("no rows: want error")
	}
	if _, err := ParseLevel("e", []string{"", ""}); err == nil {
		t.Error("empty rows: want error")
	}
	if _, err := ParseLevel("e", []string{"Z#", " F"}); err == nil {
		t.Error("unknown char: want error")
	}
	if _, err := ParseLevel("e", []string{"###", "###"}); err == nil {
		t.Error("missing flag: want error")
	}
}

func TestParseLevelDefaultPlayerStart(t *testing.T) {
	l := mustParse(t, "   ", " F ", "###")
	if s := l.PlayerStart; s.X != 1 {
		t.Errorf("default start x = %v, want 1", s.X)
	}
}

func TestTileSolid(t *testing.T) {
	solid := []Tile{Ground, Brick, Question, QuestionMush, Used, Pipe}
	for _, tl := range solid {
		if !tl.Solid() {
			t.Errorf("Tile(%d).Solid() = false, want true", tl)
		}
	}
	for _, tl := range []Tile{Empty, FlagPole, FlagTop} {
		if tl.Solid() {
			t.Errorf("Tile(%d).Solid() = true, want false", tl)
		}
	}
}

func TestLevelAtBounds(t *testing.T) {
	l := mustParse(t, "   ", " F ", "###")
	if !l.At(-1, 0).Solid() || !l.At(3, 0).Solid() {
		t.Error("level sides must be solid walls")
	}
	if l.At(0, -1).Solid() || l.At(0, 3).Solid() {
		t.Error("above/below level must be open")
	}
	l.Set(99, 99, Ground) // must not panic
	if l.At(0, 2) != Ground {
		t.Error("Set missed in-bounds write")
	}
}

func TestBuilderShapes(t *testing.T) {
	b := NewBuilder(30, LevelHeight)
	b.Ground(0, 29)
	b.Pipe(4, 2)
	b.StairsUp(10, 3)
	b.StairsDown(20, 3)
	b.Coins(5, 7, 8)
	b.Flag(27)
	rows := b.Rows()

	at := func(x, y int) byte {
		if y < 0 || y >= len(rows) || x < 0 || x >= len(rows[y]) {
			return '?'
		}
		return rows[y][x]
	}
	if at(4, 11) != 'P' || at(5, 12) != 'P' || at(4, 9) != ' ' {
		t.Errorf("pipe shape wrong: %c %c %c", at(4, 11), at(5, 12), at(4, 9))
	}
	if at(10, 12) != '#' || at(11, 11) != '#' || at(12, 10) != '#' || at(12, 9) != ' ' {
		t.Errorf("stairsUp wrong")
	}
	if at(20, 10) != '#' || at(22, 12) != '#' || at(22, 11) != ' ' {
		t.Errorf("stairsDown wrong")
	}
	if at(7, 5) != 'c' || at(7, 6) != ' ' {
		t.Errorf("coins wrong")
	}
	if at(27, 7) != 'T' || at(27, 12) != 'F' || at(27, 6) != ' ' {
		t.Errorf("flag wrong")
	}
	// Out-of-bounds writes are ignored, not panicking.
	b.Set(-1, 0, 'X')
	b.Fill(29, 0, 40, 3, 'X')
	if at(29, 0) == 'X' {
		t.Error("out-of-bounds fill leaked")
	}
}

func TestDefaultLevelsValid(t *testing.T) {
	for i, l := range DefaultLevels() {
		if l.Width < 100 {
			t.Errorf("level %d: width %d too small", i, l.Width)
		}
		if l.Height != LevelHeight {
			t.Errorf("level %d: height %d", i, l.Height)
		}
		if l.FlagX <= 10 || l.FlagX >= l.Width-4 {
			t.Errorf("level %d: flag at %d", i, l.FlagX)
		}
		if !l.At(l.FlagX, GroundTop).Solid() {
			t.Errorf("level %d: no ground under flag", i)
		}
		// Player starts standing on solid ground, not inside a tile.
		sx, sy := int(l.PlayerStart.X), int(l.PlayerStart.Y+SmallH)
		if !l.At(sx, sy).Solid() {
			t.Errorf("level %d: no ground under spawn (%d,%d)", i, sx, sy)
		}
		if l.At(sx, int(l.PlayerStart.Y+SmallH/2)).Solid() {
			t.Errorf("level %d: spawn inside solid", i)
		}
		// Pits are jumpable: at most 4 columns wide.
		run, maxRun := 0, 0
		for x := 0; x < l.Width; x++ {
			if !l.At(x, 13).Solid() && !l.At(x, 14).Solid() {
				run++
				if run > maxRun {
					maxRun = run
				}
			} else {
				run = 0
			}
		}
		if maxRun > 4 {
			t.Errorf("level %d: pit of width %d exceeds jump range", i, maxRun)
		}
		// No entity spawns inside solid tiles.
		for _, e := range append(append([]Vec{}, l.GoombaSpawns...), l.KoopaSpawns...) {
			if l.At(int(e.X+0.45), int(e.Y+0.45)).Solid() {
				t.Errorf("level %d: enemy spawn inside solid at %v", i, e)
			}
		}
		if len(l.GoombaSpawns)+len(l.KoopaSpawns) < 5 {
			t.Errorf("level %d: too few enemies", i)
		}
	}
}

func TestLoadLevelCopiesTiles(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Set(10, 9, 'B') })
	g := newGame(t, l)
	g.Player.grow()
	g.Player.Pos = Vec{9.6, 12.1 - (SuperH - SmallH)}
	g.Level.Set(10, 9, Empty) // simulate a break

	g.loadLevel(0, PowerSmall)
	if g.Level.At(10, 9) != Brick {
		t.Error("reload did not restore the source tiles")
	}
	if g.Player.Power >= PowerSuper {
		t.Error("loadLevel(false) must spawn small")
	}
}

func TestLevelNameAndIndex(t *testing.T) {
	levels := DefaultLevels()
	g := NewGame(levels, 40, LevelHeight)
	for i, want := range []string{"1-1", "1-2", "1-3", "2-1"} {
		if g.LevelIndex() != i || g.LevelName() != want {
			t.Errorf("level %d: got %d/%s, want %d/%s", i, g.LevelIndex(), g.LevelName(), i, want)
		}
		if i+1 < len(levels) {
			g.loadLevel(i+1, PowerSmall)
		}
	}
}

// snapshot captures a comparable fingerprint of game state (determinism).
func snapshot(g *Game) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "s=%d c=%d l=%d t=%d st=%v cam=%.4f p=(%.4f,%.4f,%.4f,%.4f) sup=%v",
		g.Score, g.CoinCount, g.Lives, g.Time, g.State, g.CameraX,
		g.Player.Pos.X, g.Player.Pos.Y, g.Player.Vel.X, g.Player.Vel.Y, g.Player.Power)
	for _, e := range g.Enemies {
		fmt.Fprintf(&sb, "|e(%.3f,%.3f,%v,%d)", e.Pos.X, e.Pos.Y, e.State, e.Dir)
	}
	for _, c := range g.CoinItems {
		fmt.Fprintf(&sb, "|c(%.1f,%.1f,%v)", c.Pos.X, c.Pos.Y, c.Gone)
	}
	return sb.String()
}
