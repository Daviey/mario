package engine

import (
	"fmt"
	"math"
)

// Level geometry conventions: levels are 15 rows tall with two rows of
// ground at the bottom (rows 13-14). X grows right, Y grows down.
const (
	LevelHeight = 15
	GroundTop   = 13
	FlagTopRow  = 7 // row of the flagpole finial placed by Builder.Flag

	// PlantCenterOffset is the X offset from a pipe's left edge that
	// centres a plant in its two-tile pipe mouth: pipe centre (x+1)
	// minus half a plant (1 - PlantW/2 == 0.65). Builder.Plant stores
	// it, ParseLevel re-derives it and Rows reverses it — all three
	// must agree exactly or the marker lands a column off its pipe.
	PlantCenterOffset = 0.65
)

// Builder assembles ASCII level rows programmatically.
type Builder struct {
	w, h   int
	cells  [][]byte
	plants []Vec
	theme  Theme
}

// NewBuilder returns a blank (all empty) builder.
func NewBuilder(w, h int) *Builder {
	b := &Builder{w: w, h: h}
	b.cells = make([][]byte, h)
	for y := range b.cells {
		b.cells[y] = make([]byte, w)
		for x := range b.cells[y] {
			b.cells[y][x] = ' '
		}
	}
	return b
}

// Set writes a character, ignoring out-of-bounds positions.
func (b *Builder) Set(x, y int, ch byte) {
	if x < 0 || x >= b.w || y < 0 || y >= b.h {
		return
	}
	b.cells[y][x] = ch
}

// Fill writes a rectangle (inclusive bounds), ignoring out-of-bounds cells.
func (b *Builder) Fill(x0, y0, x1, y1 int, ch byte) {
	for y := y0; y <= y1; y++ {
		for x := x0; x <= x1; x++ {
			b.Set(x, y, ch)
		}
	}
}

// Ground fills both ground rows from x0 to x1.
func (b *Builder) Ground(x0, x1 int) {
	b.Fill(x0, GroundTop, x1, b.h-1, '#')
}

// Ceiling fills the top row across the whole level with bricks — the
// closed roof of the underground and castle worlds.
func (b *Builder) Ceiling() {
	b.Fill(0, 0, b.w-1, 0, 'B')
}

// Pipe places a two-tile-wide pipe of the given height on the ground.
// It panics above height 3: taller pipes cannot be cleared with a
// normal jump, so no well-formed level may contain one.
func (b *Builder) Pipe(x, height int) {
	if height > 3 {
		panic(fmt.Sprintf("Pipe(x=%d, height=%d): taller than 3 is not jump-clearable", x, height))
	}
	b.Fill(x, GroundTop-height, x+1, GroundTop-1, 'P')
}

// Plant puts a piranha plant in the pipe at column x of the given height
// (the pipe must already/be about to exist there).
func (b *Builder) Plant(x, height int) {
	b.plants = append(b.plants, Vec{float64(x) + PlantCenterOffset, float64(GroundTop - height)})
}

// Theme selects the level's visual world.
func (b *Builder) Theme(t Theme) { b.theme = t }

// StairsUp builds an ascending staircase of h steps (1..h blocks tall).
func (b *Builder) StairsUp(x, h int) {
	for i := range h {
		b.Fill(x+i, GroundTop-1-i, x+i, GroundTop-1, '#')
	}
}

// StairsDown builds a descending staircase starting h blocks tall.
func (b *Builder) StairsDown(x, h int) {
	for i := range h {
		b.Fill(x+i, GroundTop-h+i, x+i, GroundTop-1, '#')
	}
}

// Coins places collectible coins at the given columns on one row.
func (b *Builder) Coins(y int, xs ...int) {
	for _, x := range xs {
		b.Set(x, y, 'c')
	}
}

// Flag places the goal flagpole at column x, finial at FlagTopRow.
func (b *Builder) Flag(x int) {
	b.Set(x, FlagTopRow, 'T')
	b.Fill(x, FlagTopRow+1, x, GroundTop-1, 'F')
}

// Rows returns the assembled ASCII rows. It panics when a plant marker
// cannot be placed — a dropped plant is always an authoring bug that
// must fail loudly, never a silent level change.
func (b *Builder) Rows() []string {
	rows := make([]string, b.h)
	for y := range b.cells {
		rows[y] = string(b.cells[y])
	}
	for _, p := range b.plants {
		// The marker lives in the air cell above the pipe mouth (the
		// mouth itself is pipe tiles).
		// Round: p.X-PlantCenterOffset can be 15.999... for a pipe at
		// 16 (float subtraction), and truncation would shift the
		// marker a column left of its pipe.
		x, y := int(math.Round(p.X-PlantCenterOffset)), int(p.Y)-1
		if y < 0 || y >= b.h || x < 0 || x >= b.w {
			panic(fmt.Sprintf("plant at %v: marker (%d,%d) falls outside the level grid", p, x, y))
		}
		if rows[y][x] != ' ' {
			panic(fmt.Sprintf("plant at %v: marker cell (%d,%d) already holds %q; the plant would be silently dropped", p, x, y, rows[y][x]))
		}
		row := []byte(rows[y])
		row[x] = 'V'
		rows[y] = string(row)
	}
	return rows
}

func mustLevel(name string, b *Builder) *Level {
	l, err := ParseLevel(name, b.Rows())
	if err != nil {
		panic(err)
	}
	l.Theme = b.theme
	return l
}

// DefaultLevels returns the seven built-in levels (worlds 1 and 2).
func DefaultLevels() []*Level {
	return []*Level{level1(), level2(), level3(), level4(), level5(), level6(), level7()}
}

func level1() *Level {
	b := NewBuilder(160, LevelHeight)
	b.Ground(0, 51)
	b.Ground(54, 88)  // pit 52-53
	b.Ground(91, 159) // pit 89-90
	b.Set(3, 12, 'M')

	b.Set(16, 9, '?')

	b.Set(20, 9, 'B')
	b.Set(21, 9, '?')
	b.Set(22, 9, 'B')
	b.Set(23, 9, '?')
	b.Set(24, 9, 'B')
	b.Set(22, 5, '?')
	b.Set(24, 12, 'G')

	b.Pipe(28, 2)
	b.Pipe(34, 3)
	b.Plant(34, 3)
	b.Set(38, 12, 'G')
	b.Pipe(41, 3)
	b.Set(30, 8, '1') // hidden 1-UP above the third pipe (jump from the rim)
	b.Set(46, 12, 'G')

	b.Coins(9, 56, 57, 58)
	// A mixed block row: brick-question-brick in reading order.
	for _, s := range []struct {
		x  int
		ch byte
	}{{60, 'B'}, {61, '?'}, {62, 'B'}, {63, 'U'}, {64, 'B'}, {65, 'f'}, {66, 'B'}} {
		b.Set(s.x, 9, s.ch)
	}
	b.Set(64, 12, 'G')
	b.Set(66, 12, 'G')

	b.Fill(72, 10, 75, 10, 'B')
	b.Coins(8, 72, 73, 74, 75)
	b.Set(80, 12, 'K')

	b.Fill(84, 9, 87, 9, 'B')
	b.Set(86, 12, 'G')

	b.Fill(96, 10, 99, 10, 'B')
	b.Coins(8, 96, 97, 98, 99)
	b.Set(104, 12, 'K')
	b.Set(110, 12, 'G')
	b.Set(112, 12, 'G')

	b.Set(118, 9, '?')
	b.Set(121, 9, '?')

	b.StairsUp(126, 8)
	b.StairsDown(135, 8)
	b.Flag(150)
	l := mustLevel("1-1", b)

	// The first pipe is SMB 1-1's bonus-cellar pipe: Down on its mouth
	// trades the surface for the coin cache below, and the cellar's exit
	// pipe surfaces at the far pipe — past the plant pipe and the hidden
	// 1-UP, exactly the shortcut the original pays for exploring.
	room := level1Room()
	l.Warps = []Warp{{X: 28, Top: GroundTop - 2, Dest: room, DestX: 2, DestTop: GroundTop - 2}}
	room.Warps = []Warp{{X: 30, Top: GroundTop - 2, Dest: nil, DestX: 41, DestTop: GroundTop - 3}}
	return l
}

// level1Room is 1-1's underground bonus cellar: a brick-lined pocket
// under the surface with a coin cache, pipe in on the left, pipe out on
// the right. It has no flag — play ends only by travelling or dying,
// and the level-1 index (and the leaderboard's level column) stay with
// the surface.
func level1Room() *Level {
	b := NewBuilder(34, LevelHeight)
	b.Theme(ThemeUnderground)
	b.Ground(0, 33)
	b.Ceiling()
	for y := 1; y < GroundTop; y++ { // brick walls close the cellar
		b.Set(0, y, 'B')
		b.Set(33, y, 'B')
	}
	b.Pipe(2, 2)  // entry: the player rises out of this mouth
	b.Pipe(30, 2) // exit: Down here returns to the surface
	b.Coins(11, 8, 9, 10, 11, 12, 13, 14, 15, 16, 17, 18, 19, 20, 21)
	b.Coins(9, 12, 13, 14, 15, 16)
	b.Set(6, 12, 'M')
	return mustLevel("1-1", b)
}

// level2 is the underground world: dark palette, brick ceiling, plants.
func level2() *Level {
	b := NewBuilder(170, LevelHeight)
	b.Theme(ThemeUnderground)
	b.Ground(0, 29)
	b.Ground(33, 60)   // pit 30-32
	b.Ground(65, 100)  // pit 61-64 (needs a running jump)
	b.Ground(105, 169) // pit 101-104 (running jump)
	b.Ceiling()        // the underground brick ceiling
	b.Set(3, 12, 'M')

	b.Set(10, 9, '?')
	b.Pipe(14, 3)
	b.Set(18, 12, 'G')
	b.Set(22, 9, 'B')
	b.Set(23, 9, '?')
	b.Set(24, 9, 'U')
	b.Set(25, 9, 'B')
	b.Set(26, 9, 'B')
	b.Set(25, 5, 'H') // hidden coin high above the brick row
	b.Set(28, 12, 'K')

	// Tiered climb with coins on the high route.
	b.Fill(34, 10, 38, 10, 'B')
	b.Coins(8, 34, 35, 36, 37, 38)
	b.Set(36, 9, 'G')
	b.Fill(42, 8, 46, 8, 'B')
	b.Coins(6, 42, 43, 44, 45, 46)
	b.Fill(50, 6, 54, 6, 'B')
	b.Coins(4, 50, 51, 52, 53, 54)

	b.Pipe(66, 2)
	b.Plant(66, 2)
	b.Set(70, 12, 'G')
	b.Pipe(72, 3)
	b.Plant(72, 3)
	b.Set(78, 12, 'G')
	b.Set(80, 12, 'G')

	b.Fill(86, 9, 92, 9, 'B')
	b.Set(87, 9, '?')
	b.Set(91, 9, '?')
	b.Fill(88, 5, 90, 5, 'B')
	b.Set(89, 5, 'f')
	b.Set(96, 12, 'K')

	b.Fill(106, 10, 110, 10, 'B')
	b.Coins(8, 106, 107, 108, 109, 110)
	b.Set(112, 12, 'K')
	b.Set(116, 12, 'G')
	b.Set(118, 12, 'G')
	b.Set(120, 12, 'G')

	b.Pipe(124, 3)
	b.Plant(124, 3)
	b.Pipe(130, 2)

	b.Fill(134, 9, 137, 9, 'B')
	b.Coins(7, 134, 135, 136, 137)

	b.StairsUp(142, 8)
	b.StairsDown(151, 8)
	b.Flag(164)
	return mustLevel("1-2", b)
}

func level3() *Level {
	b := NewBuilder(180, LevelHeight)
	b.Ground(0, 19)
	b.Ground(22, 45)   // pit 20-21
	b.Ground(48, 80)   // pit 46-47
	b.Ground(84, 119)  // pit 81-83
	b.Ground(123, 179) // pit 120-122
	b.Set(3, 12, 'M')

	b.Set(8, 9, '?')
	b.Set(12, 9, 'U')
	b.Pipe(16, 2)
	b.Plant(16, 2)

	b.Set(24, 12, 'G')
	b.Set(26, 12, 'G')
	b.Fill(30, 10, 33, 10, 'B')
	b.Coins(8, 30, 31, 32, 33)
	b.Set(36, 12, 'K')

	b.Fill(40, 9, 43, 9, 'B')
	b.Set(41, 9, 'f')

	b.Fill(50, 10, 53, 10, 'B')
	b.Set(52, 9, 'G')
	b.Fill(56, 8, 59, 8, 'B')
	b.Coins(6, 56, 57, 58, 59)

	b.Set(64, 12, 'K')
	b.Set(68, 12, 'G')
	b.Set(70, 12, 'G')
	b.Fill(74, 9, 78, 9, 'B')
	b.Set(76, 9, '?')
	b.Set(75, 9, 'S') // star power in the brick run

	b.Pipe(86, 3)
	b.Plant(86, 3)
	b.Set(90, 12, 'G')
	b.Set(92, 12, 'K')
	b.Fill(96, 10, 100, 10, 'B')
	b.Coins(8, 96, 97, 98, 99, 100)
	b.Fill(104, 8, 108, 8, 'B')
	b.Coins(6, 104, 105, 106, 107, 108)

	b.Set(112, 12, 'G')
	b.Set(114, 12, 'G')
	b.Set(116, 12, 'G')
	b.Set(118, 12, 'K')

	b.Pipe(126, 3)
	b.Plant(126, 3)
	b.Pipe(132, 3)
	b.Fill(136, 9, 140, 9, 'B')
	b.Set(138, 9, '?')

	b.Set(144, 12, 'G')
	b.Set(148, 12, 'K')

	b.StairsUp(152, 8)
	b.StairsDown(161, 8)
	b.Flag(174)
	return mustLevel("1-3", b)
}

// level4 is the first level of world 2: pipes bite, fire flowers flow.
func level4() *Level {
	b := NewBuilder(190, LevelHeight)
	b.Ground(0, 29)
	b.Ground(33, 64)   // pit 30-32
	b.Ground(69, 104)  // pit 65-68 (running jump)
	b.Ground(108, 189) // pit 105-107
	b.Set(3, 12, 'M')

	b.Set(10, 9, '?')
	b.Set(14, 9, 'f')
	b.Pipe(18, 2)
	b.Set(26, 12, 'G')

	b.Pipe(22, 3)
	b.Plant(22, 3)

	for _, s := range []struct {
		x  int
		ch byte
	}{{34, 'B'}, {35, '?'}, {36, 'B'}, {37, 'U'}, {38, 'B'}} {
		b.Set(s.x, 9, s.ch)
	}
	b.Coins(8, 34, 35, 36, 37, 38)

	// Shell-bowling lane: kick the koopa's shell into the goomba row.
	b.Set(48, 12, 'K')
	b.Set(52, 12, 'G')
	b.Set(54, 12, 'G')
	b.Set(56, 12, 'G')

	b.Fill(58, 9, 60, 9, 'B')
	b.Coins(7, 58, 59, 60)

	b.Pipe(72, 2)
	b.Plant(72, 2)
	b.Set(78, 12, 'G')
	b.Set(80, 12, 'G')
	b.Set(84, 12, 'K')
	b.Set(85, 5, '1') // hidden 1-UP before the pipe gauntlet

	b.Fill(88, 9, 92, 9, 'B')
	b.Set(89, 9, '?')
	b.Set(91, 9, 'f')
	b.Coins(8, 88, 92)

	b.Set(112, 12, 'G')
	b.Set(114, 12, 'G')
	b.Pipe(118, 3)
	b.Plant(118, 3)

	b.StairsUp(124, 4)
	b.StairsDown(128, 4)

	b.Set(138, 12, 'K')
	b.Set(142, 12, 'G')
	b.Set(144, 12, 'G')

	b.Fill(148, 10, 152, 10, 'B')
	b.Coins(8, 148, 149, 150, 151, 152)

	b.Pipe(158, 2)

	b.StairsUp(166, 8)
	b.Flag(182)
	return mustLevel("2-1", b)
}

// level5 is world 2's underground gauntlet: pipes bite, paratroopas hop.
func level5() *Level {
	b := NewBuilder(190, LevelHeight)
	b.Theme(ThemeUnderground)
	b.Ground(0, 39)
	b.Ground(43, 79)   // pit 40-42
	b.Ground(84, 119)  // pit 80-83 (running jump)
	b.Ground(123, 189) // pit 120-122
	b.Ceiling()        // the underground brick ceiling
	b.Set(3, 12, 'M')

	b.Set(10, 9, '?')
	b.Set(14, 9, 'U')
	b.Pipe(18, 2)
	b.Pipe(24, 3)
	b.Plant(24, 3)
	b.Set(22, 12, 'G')
	b.Set(30, 12, 'G')
	b.Set(34, 12, 'W')

	b.Fill(36, 9, 38, 9, 'B')
	b.Coins(8, 36, 37, 38)

	b.Pipe(46, 2)
	b.Pipe(52, 3)
	b.Plant(52, 3)
	b.Set(50, 12, 'W')
	b.Set(56, 12, 'K')
	b.Set(60, 12, 'G')
	b.Set(62, 12, 'G')

	for _, s := range []struct {
		x  int
		ch byte
	}{{66, 'B'}, {67, '?'}, {68, 'B'}, {69, 'f'}, {70, 'B'}} {
		b.Set(s.x, 9, s.ch)
	}
	b.Coins(8, 66, 67, 68, 69, 70)

	// Tiered climb out of the second pit landing.
	b.Fill(86, 10, 90, 10, 'B')
	b.Coins(9, 86, 87, 88, 89, 90)
	b.Fill(94, 8, 98, 8, 'B')
	b.Set(95, 4, 'H') // hidden coin above the upper tier — bump from the platform
	b.Coins(7, 94, 95, 96, 97, 98)

	b.Set(100, 12, 'W')
	b.Set(102, 12, 'G')
	b.Set(104, 12, 'G')
	b.Pipe(108, 3)
	b.Plant(108, 3)
	b.Set(114, 12, 'K')

	b.Fill(126, 9, 130, 9, 'B')
	b.Set(128, 9, 'U')
	b.Coins(8, 126, 127, 128, 129, 130)
	b.Set(134, 12, 'W')
	b.Set(138, 12, 'G')
	b.Set(140, 12, 'G')
	b.Set(144, 12, 'K')

	b.Fill(148, 9, 152, 9, 'B')
	b.Coins(8, 148, 149, 150, 151, 152)
	b.Pipe(156, 2)

	b.StairsUp(160, 8)
	b.Flag(176)
	return mustLevel("2-2", b)
}

// level6 is the athletic sky world: floating platforms, coin runs and
// hopping paratroopas over wide running-jump gaps.
func level6() *Level {
	b := NewBuilder(200, LevelHeight)
	b.Theme(ThemeSky)
	b.Ground(0, 24)   // pit 25-28 (running jump)
	b.Ground(29, 47)  // pit 48-51 (running jump)
	b.Ground(52, 83)  // pit 84-87 (running jump)
	b.Ground(88, 119) // pit 120-123 (running jump)
	b.Ground(124, 199)
	b.Set(3, 12, 'M')

	b.Set(8, 9, '?')
	b.Set(12, 9, 'U')
	b.Pipe(16, 2)
	b.Set(20, 12, 'G')

	// High coin route over the first gap.
	b.Fill(30, 9, 34, 9, 'B')
	b.Coins(8, 30, 31, 32, 33, 34)
	b.Fill(38, 7, 42, 7, 'B')
	b.Coins(6, 38, 39, 40, 41, 42)
	b.Set(40, 6, 'W') // paratroopa patrols the high coin platform
	b.Set(32, 12, 'G')
	b.Set(40, 12, 'K')
	b.Set(44, 12, 'W')

	b.Fill(54, 9, 58, 9, 'B')
	b.Set(66, 5, 'H') // hidden coin over the mid-stretch
	b.Set(56, 9, 'f')
	b.Coins(8, 54, 55, 56, 57, 58)
	b.Set(62, 12, 'G')
	b.Set(68, 12, 'K')
	b.Set(58, 12, 'W')
	b.Set(72, 12, 'G')
	b.Set(74, 12, 'G')

	b.Fill(78, 10, 82, 10, 'B')
	b.Coins(9, 78, 79, 80, 81, 82)

	b.Set(90, 12, 'W')
	b.Fill(92, 9, 96, 9, 'B')
	b.Coins(8, 92, 93, 94, 95, 96)
	b.Set(94, 8, 'K')
	b.Set(100, 12, 'G')
	b.Set(104, 12, 'W')
	b.Set(108, 12, 'G')
	b.Set(110, 12, 'G')

	b.Fill(112, 9, 116, 9, 'B')
	b.Set(114, 9, 'U')
	b.Coins(8, 112, 113, 114, 115, 116)

	b.Set(128, 12, 'W')
	b.Set(132, 12, 'G')
	b.Set(136, 12, 'K')
	b.Set(140, 12, 'G')
	b.Pipe(144, 3)
	b.Plant(144, 3)

	b.Fill(150, 10, 154, 10, 'B')
	b.Coins(9, 150, 151, 152, 153, 154)
	b.Set(152, 9, 'W')
	b.Set(158, 12, 'G')
	b.Set(162, 12, 'K')

	b.StairsUp(168, 8)
	b.Flag(184)
	return mustLevel("2-3", b)
}

// level7 is the castle finale: grey stone, lava pools and fire bars.
func level7() *Level {
	b := NewBuilder(170, LevelHeight)
	b.Theme(ThemeCastle)
	b.Ground(0, 29)
	b.Ground(33, 64)   // lava 30-32
	b.Ground(69, 104)  // lava 65-68 (running jump)
	b.Ground(108, 169) // lava 105-107
	b.Ceiling()        // the castle brick ceiling
	b.Fill(30, 13, 32, 14, 'L')
	b.Fill(65, 13, 68, 14, 'L')
	b.Fill(105, 13, 107, 14, 'L')
	b.Set(3, 12, 'M')

	// Fire bars: a brick pillar with a rotating hub on top. Alternate
	// hub columns spin opposite ways (see NewFireBar).
	pillar := func(x int) {
		b.Fill(x, 11, x, 12, 'B')
		b.Set(x, 10, 'h')
	}

	b.Set(8, 9, '?')
	b.Set(12, 9, 'U')
	b.Set(14, 12, 'G')
	pillar(21)
	b.Set(24, 12, 'K')
	b.Set(26, 12, 'G')

	b.Fill(34, 9, 38, 9, 'B')
	b.Set(34, 9, '?')
	b.Set(40, 12, 'W')
	pillar(44)
	b.Set(48, 12, 'G')
	b.Set(52, 12, 'K')
	b.Set(56, 12, 'G')
	pillar(61)

	b.Coins(9, 72, 73, 74, 75, 76)
	b.Set(72, 12, 'W')
	b.Set(76, 9, 'f')
	b.Set(80, 12, 'K')
	pillar(82)
	b.Set(88, 12, 'G')
	pillar(97)
	b.Set(92, 12, 'K')
	b.Set(100, 12, 'G')

	b.Set(110, 12, 'W')
	b.Fill(116, 9, 120, 9, 'B')
	b.Set(122, 8, '1') // hidden 1-UP past the pillar bars
	b.Set(118, 9, 'U')
	b.Coins(8, 116, 117, 118, 119, 120)
	pillar(112)
	b.Set(124, 12, 'G')
	b.Set(128, 12, 'K')
	pillar(129)
	b.Set(136, 12, 'G')
	b.Set(140, 12, 'W')
	b.Set(144, 12, 'K')
	b.Set(148, 12, 'G')

	b.StairsUp(150, 8)
	b.Flag(160)
	return mustLevel("2-4", b)
}
