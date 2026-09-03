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

	// Lifts, springboards and water currents are placed through method
	// calls (they carry parameters no tile char can hold); mustLevel
	// copies them into the parsed Level. The append methods live in
	// level.go/lifts.go.
	lifts    []LiftSpawn
	springs  []Vec
	currents []CurrentZone
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
	l.LiftSpawns = b.lifts
	l.SpringSpawns = b.springs
	l.Currents = b.currents
	return l
}

// DefaultLevels returns the twenty-four built-in levels (worlds 1-6):
// 1-1, 1-2, 1-3, 1-4 (castle), 2-1, 2-2 (underwater), 2-3 (bridge),
// 2-4 (castle), 3-1, 3-2, 3-3 (world-3 night), 3-4 (castle, toad),
// 4-1, 4-2 (underground vine cellar), 4-3 (athletic), 4-4 (castle,
// toad), 5-1 (bullet-bill blasters), 5-2 (underground, flooded
// middle), 5-3 (elevator flight), 5-4 (castle, toad), 6-1 (plant
// gauntlet), 6-2 (lift cave), 6-3 (short hop chain) and 6-4 (final
// castle, the princess ends the quest).
func DefaultLevels() []*Level {
	return []*Level{level1(), level2(), level3(), level4(), level5(), level6(),
		level7(), level8(), level9(), level10(), level11(), level12(),
		level13(), level14(), level15(), level16(),
		level17(), level18(), level19(), level20(),
		level21(), level22(), level23(), level24()}
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
	b.Set(77, 9, 'J') // the beanstalk brick (SMB's vine block): bump, climb, Coin Heaven
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
	l.Time = 400

	// The first pipe is SMB 1-1's bonus-cellar pipe: Down on its mouth
	// trades the surface for the coin cache below, and the cellar's exit
	// pipe surfaces at the far pipe — past the plant pipe and the hidden
	// 1-UP, exactly the shortcut the original pays for exploring.
	room := level1Room()
	l.Warps = []Warp{{X: 28, Top: GroundTop - 2, Dest: room, DestX: 2, DestTop: GroundTop - 2}}
	room.Warps = []Warp{{X: 30, Top: GroundTop - 2, Dest: nil, DestX: 41, DestTop: GroundTop - 3}}
	l.VineRoom = level1CoinHeaven()
	return l
}

// level1CoinHeaven is 1-1's beanstalk bonus room: a sky ledge with a
// coin harvest and an open right edge — running off the floor drops the
// player back into 1-1 at the final staircase, falling from above the
// sky (the original's skip-ahead exit). Rooms are not part of
// DefaultLevels; the vine brick's stalk crown leads here (level1).
func level1CoinHeaven() *Level {
	b := NewBuilder(26, LevelHeight)
	b.Theme(ThemeSky)
	b.Ground(0, 17) // the ledge: open sky from column 18 rightward
	for y := 1; y < GroundTop; y++ {
		b.Set(0, y, 'B') // brick wall closes the left edge
	}
	b.Coins(10, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13) // the long harvest row
	b.Coins(7, 10, 11, 12, 13)                          // ...and its high finish
	l := mustLevel("1-1 heaven", b)
	l.DropExitX = 124 // the final staircase's base, back in 1-1
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

// level2 is the underground world 1-2: dark palette, brick ceiling,
// plants — and near the end the original's vertical lifts plus a roof
// gap that leads to the hidden warp room above the exit stretch.
func level2() *Level {
	b := NewBuilder(206, LevelHeight)
	b.Theme(ThemeUnderground)
	b.Ground(0, 29)
	b.Ground(33, 60)            // pit 30-32
	b.Ground(65, 100)           // pit 61-64 (needs a running jump)
	b.Ground(105, 205)          // pit 101-104 (running jump)
	b.Ceiling()                 // the underground brick ceiling
	b.Fill(158, 0, 173, 0, ' ') // roof gap over the warp alcove
	b.Set(3, 12, 'M')

	b.Set(10, 9, '?')
	b.Pipe(14, 3)
	b.Set(18, 12, 'G')
	b.Set(22, 9, 'C') // multi-coin brick leads the row: ten coins or ~4s of bumping
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

	// SMB 1-2's exit stretch: a brick shelf, then the original's first
	// VERTICAL LIFTS flanking a koopa on a block platform (power-up
	// brick above it).
	b.Fill(134, 10, 136, 10, 'B')
	b.Coins(9, 134, 135, 136)
	b.Lift(137, 8, 3, LiftVert, 3)
	b.Fill(141, 9, 144, 9, 'B')
	b.Set(142, 8, 'K')
	b.Set(142, 5, 'U')
	b.Lift(147, 8, 3, LiftVert, 3)

	b.Set(150, 12, 'G')
	b.Set(152, 12, 'G')
	b.StairsUp(155, 4)

	// The warp alcove: a brick ledge under the roof gap, reached by the
	// shaft lift that rises through the hole in the ledge. The pipe on
	// the ledge (mouth row 1, above the cleared ceiling) hides the Warp
	// Zone — the cellar-pipe pattern, one storey up.
	b.Fill(156, 3, 172, 3, 'B')
	b.Fill(160, 3, 162, 3, ' ') // the lift shaft hole
	b.Lift(160, 8, 3, LiftVert, 5)
	b.Fill(168, 1, 169, 2, 'P')

	b.StairsUp(178, 8)
	b.StairsDown(187, 8)
	b.Flag(200)
	l := mustLevel("1-2", b)
	l.Time = 400

	room := level2WarpRoom()
	l.Warps = []Warp{{X: 168, Top: 1, Dest: room, DestX: 2, DestTop: GroundTop - 2}}
	return l
}

// level2WarpRoom is 1-2's hidden Warp Zone above the exit stretch: a
// sealed roof pocket whose pipes jump the run forward — world 3 on the
// left, world 2 on the right (the original's third pipe, world 4, is
// out of scope). It has no flag: every pipe ends the visit.
func level2WarpRoom() *Level {
	b := NewBuilder(26, LevelHeight)
	b.Theme(ThemeUnderground)
	b.Ground(0, 25)
	b.Ceiling()
	for y := 1; y < GroundTop; y++ { // brick walls close the pocket
		b.Set(0, y, 'B')
		b.Set(25, y, 'B')
	}
	b.Pipe(2, 2)  // arrival: the player rises out of this mouth
	b.Pipe(8, 2)  // world 4
	b.Pipe(14, 2) // world 3
	b.Pipe(20, 2) // world 2
	b.Set(6, 12, 'M')
	l := mustLevel("1-2", b)
	l.Warps = []Warp{
		{X: 8, Top: GroundTop - 2, JumpTo: 12, DestX: 2, DestTop: GroundTop - 2},
		{X: 14, Top: GroundTop - 2, JumpTo: 8, DestX: 2, DestTop: GroundTop - 2},
		{X: 20, Top: GroundTop - 2, JumpTo: 4, DestX: 2, DestTop: GroundTop - 2},
	}
	return l
}

// level3 is world 1's athletic closing act: island hops over running
// gaps, red koopas on the islands, red paratroopas flying the gaps and
// two horizontal lifts with coin trails above them.
func level3() *Level {
	b := NewBuilder(190, LevelHeight)
	b.Ground(0, 19)
	b.Ground(22, 33)   // pit 20-21
	b.Ground(37, 48)   // pit 34-36
	b.Ground(52, 63)   // pit 49-51
	b.Ground(67, 74)   // pit 64-66
	b.Ground(78, 95)   // pit 75-77
	b.Ground(100, 119) // pit 96-99 (lift or running jump)
	b.Ground(124, 139) // pit 120-123 (lift or running jump)
	b.Ground(143, 149) // pit 140-142
	b.Ground(153, 189) // pit 150-152
	b.Set(3, 12, 'M')

	b.Coins(9, 10, 11, 12, 13)
	b.Set(26, 12, 'R') // red koopas patrol the islands

	b.Set(40, 12, 'G')
	b.Set(42, 12, 'G')

	// Tall island: a brick crown with coins on top.
	b.Fill(56, 9, 59, 9, 'B')
	b.Coins(8, 56, 57, 58, 59)

	// The short island under it carries the level's only power-up.
	b.Set(70, 9, 'U')

	b.Set(76, 7, 'r') // red paratroopa flying the gap
	b.Set(82, 12, 'G')
	b.Set(86, 12, 'R')

	// Two horizontal lifts with coins above (the original's onscreen
	// oscillators).
	b.Lift(97, 10, 3, LiftHoriz, 4)
	b.Coins(7, 96, 97, 98, 99)

	b.Coins(9, 104, 105, 106, 107)
	b.Set(112, 7, 'r')

	b.Lift(121, 9, 3, LiftHoriz, 4)
	b.Coins(6, 120, 121, 122, 123)

	b.Coins(8, 145, 146, 147)
	b.Set(156, 12, 'R')

	b.StairsUp(166, 8)
	b.Flag(180)
	l := mustLevel("1-3", b)
	l.Time = 300
	return l
}

// level4 is world 1's castle: fire-bar corridors over lava, two lava
// bubbles, hidden coin blocks before the boss and a fake Bowser (a
// goomba in disguise) guarding the axe under a horizontal lift. Toad
// waits behind the arena.
func level4() *Level {
	b := NewBuilder(182, LevelHeight)
	b.Theme(ThemeCastle)
	b.Ground(0, 29)
	b.Ground(33, 64)   // lava 30-32
	b.Ground(69, 104)  // lava 65-68
	b.Ground(108, 181) // lava 105-107
	b.Ceiling()        // the castle brick ceiling
	b.Fill(30, 13, 32, 14, 'L')
	b.Fill(65, 13, 68, 14, 'L')
	b.Fill(105, 13, 107, 14, 'L')
	b.Set(3, 8, 'M')

	// The original's elevated start: a brick shelf and stairs down
	// toward the first lava pit.
	b.Fill(2, 9, 9, 9, 'B')
	b.StairsDown(11, 4)

	// A fire-bar island in the first lava pool, power-up floating above.
	b.Fill(31, 11, 31, 12, 'B')
	b.Set(31, 10, 'h')
	b.Set(31, 8, 'U')

	// Corridor of ceiling fire-bars: raised walkway, dropped ceiling,
	// bars sweeping the full height.
	b.Fill(34, 9, 54, 12, '#')
	b.Fill(34, 5, 54, 5, 'B')
	b.Set(38, 6, 'h')
	b.Set(45, 6, 'h')
	b.Set(52, 6, 'h')

	// The chamber with bars on the ceiling AND the floor: jump the low
	// bar, duck the hanging ones.
	b.Fill(56, 5, 64, 5, 'B')
	b.Set(58, 6, 'h')
	b.Set(62, 6, 'h')
	b.Set(60, 12, 'h')

	b.Set(66, 13, 'o') // lava bubble in the second pool

	b.Fill(72, 9, 76, 9, 'B')
	b.Coins(8, 72, 73, 74, 75, 76)

	b.Set(106, 13, 'o') // lava bubble in the third pool

	// Hidden coin blocks right before the boss (the original's six).
	for _, x := range []int{112, 114, 116, 118, 120, 122} {
		b.Set(x, 9, 'H')
	}

	// The boss arena: a short stair up, a bridge of planks over lava,
	// the fake Bowser on it, a horizontal lift above the bridge and the
	// axe behind him.
	b.StairsUp(146, 4)
	b.Fill(150, 13, 157, 14, 'L') // the lava pool under the bridge
	for x := 150; x <= 157; x++ {
		b.Set(x, 13, 'b') // planks flush to the ledge at 158
	}
	b.Lift(152, 8, 3, LiftHoriz, 3)
	b.Set(154, 12, 'Z') // fake Bowser (goomba), planted on the bridge
	b.Set(161, 12, 'x') // the axe
	b.Set(170, 12, 't') // Toad behind the arena
	l := mustLevel("1-4", b)
	l.Time = 300
	l.BowserDisguise = KindGoomba
	return l
}

// level5 is the first level of world 2: pipes bite, fire flowers flow —
// and the exit is gated by the original's springboard-over-tall-bricks
// finale (with the hidden-block alternative over two bricks).
func level5() *Level {
	b := NewBuilder(200, LevelHeight)
	b.Ground(0, 29)
	b.Ground(33, 64)   // pit 30-32
	b.Ground(69, 104)  // pit 65-68 (running jump)
	b.Ground(108, 199) // pit 105-107
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

	// The springboard finale: compress and release at max to clear the
	// tall brick wall — or bump the hidden block over the two bricks
	// and hop the wall the quiet way.
	b.Springboard(163, 12)
	b.Fill(164, 10, 165, 10, 'B')
	b.Set(165, 7, 'H')
	b.Fill(168, 5, 168, 12, 'B') // the tall brick wall

	b.StairsUp(174, 8)
	b.Flag(190)
	l := mustLevel("2-1", b)
	l.Time = 400
	return l
}

// level5FlagRoom is 2-2's overworld exit: the player rises out of the
// pipe (with the original's piranha plant in it), walks the short
// surface stretch to the flagpole. Rooms are not part of
// DefaultLevels; the warp from 2-2's underwater body leads here.
func level5FlagRoom() *Level {
	b := NewBuilder(44, LevelHeight)
	b.Ground(0, 43)
	b.Pipe(2, 2)
	b.Plant(2, 2) // the original's plant in the exit pipe
	b.Coins(9, 20, 21, 22)
	b.StairsUp(28, 4)
	b.Flag(38)
	b.Set(6, 12, 'M')
	return mustLevel("2-2", b)
}

// level6 is world 2's underwater world: swim physics, bloobers lunging
// from the deep, spawned cheep-cheeps crossing the screen and three
// downward currents dragging the player toward the pit floors. The exit
// pipe warps to the overworld flag room.
func level6() *Level {
	b := NewBuilder(190, LevelHeight)
	b.Theme(ThemeUnderwater)
	b.Ground(0, 29)
	b.Ground(34, 63)   // pit 30-33 (current)
	b.Ground(68, 97)   // pit 64-67 (current)
	b.Ground(102, 141) // pit 98-101 (current)
	b.Ground(146, 189) // pit 142-145
	b.Ceiling()        // the enclosed water body
	b.Set(3, 12, 'M')

	// Three downward currents, one per pit crossing.
	b.Current(29, 34)
	b.Current(63, 68)
	b.Current(97, 102)

	// Floor texture: two stone humps on the waterbed.
	b.Fill(50, 11, 54, 12, '#')
	b.Fill(118, 11, 122, 12, '#')

	// Bloobers drift down and lunge up across the whole body.
	b.Set(26, 7, 'q')
	b.Set(44, 8, 'q')
	b.Set(58, 6, 'q')
	b.Set(80, 7, 'q')
	b.Set(112, 8, 'q')
	b.Set(132, 6, 'q')

	b.Coins(8, 30, 31, 32, 33)
	b.Coins(9, 50, 51, 52, 53, 54)
	b.Coins(8, 64, 65, 66, 67)
	b.Coins(8, 98, 99, 100, 101)
	b.Coins(9, 118, 119, 120, 121, 122)
	b.Coins(10, 150, 151, 152, 153, 154)
	b.Coins(8, 160, 161, 162, 163, 164)

	// The exit pipe: Down on the mouth trades water for sky.
	b.Pipe(176, 3)
	l := mustLevel("2-2", b)
	l.Time = 400
	l.Underwater = true

	room := level5FlagRoom()
	l.Warps = []Warp{{X: 176, Top: GroundTop - 3, Dest: room, DestX: 2, DestTop: GroundTop - 2}}
	return l
}

// level7 is world 2's bridge run: long plank bridges over the
// bottomless fall, small islands in the second half and red
// cheep-cheeps leaping from below until the stone steps at the end.
func level7() *Level {
	b := NewBuilder(170, LevelHeight)
	b.Theme(ThemeSky)
	b.Ground(0, 19)
	b.Fill(20, 13, 59, 13, 'b') // bridge run 1
	b.Ground(60, 69)
	b.Fill(70, 13, 109, 13, 'b') // bridge run 2
	b.Ground(110, 117)
	b.Fill(118, 13, 121, 13, 'b') // island hop
	b.Ground(122, 129)
	b.Fill(130, 13, 133, 13, 'b') // island hop
	b.Ground(134, 169)
	b.Set(3, 12, 'M')

	// The level's single power-up, mid-bridge.
	b.Set(64, 9, 'U')

	// Coin trails over the open air the leaps arc through.
	b.Coins(8, 34, 35, 36, 37, 38)
	b.Coins(9, 44, 45, 46, 47, 48)
	b.Coins(8, 80, 81, 82, 83, 84)
	b.Coins(8, 92, 93, 94, 95, 96)
	b.Coins(9, 102, 103, 104, 105, 106)
	b.Coins(8, 112, 113, 114, 115)
	b.Coins(8, 124, 125, 126, 127)

	// The stone steps: leaps stop once the player reaches them.
	b.StairsUp(146, 8)
	b.Coins(10, 148, 149)
	b.Flag(160)
	l := mustLevel("2-3", b)
	l.Time = 300
	l.CheepLeaping = true
	l.CheepStopX = 144
	return l
}

// level8 is the castle at the end of world 2: grey stone, lava pools
// with leaping lava bubbles, fire bars — and the boss bridge where the
// fake Bowser (a green koopa in disguise) guards the axe. Toad waits
// behind the arena.
func level8() *Level {
	b := NewBuilder(178, LevelHeight)
	b.Theme(ThemeCastle)
	b.Ground(0, 29)
	b.Ground(33, 64)   // lava 30-32
	b.Ground(69, 104)  // lava 65-68 (running jump)
	b.Ground(108, 177) // lava 105-107
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
	// Atop the boss-arena stair (the old floor cell at 148,12 is now
	// inside the staircase).
	b.Set(148, 9, 'G')

	// The lava pools boil: two lava bubbles leap on the way in.
	b.Set(31, 13, 'o')
	b.Set(66, 13, 'o')

	// The boss arena: a short stair up, a bridge of planks over lava,
	// Bowser patrolling it and the axe behind him. Touching the axe
	// collapses the bridge and drops him in the pool (bowser.go).
	b.StairsUp(146, 4)
	b.Fill(150, 13, 157, 14, 'L') // the lava pool under the bridge
	for x := 150; x <= 157; x++ {
		b.Set(x, 13, 'b') // planks flush to the ledge at 158 — no notch to walk into
	}
	b.Set(154, 12, 'Z') // fake Bowser (koopa), planted on the bridge
	b.Set(161, 12, 'x') // the axe
	b.Set(171, 12, 't') // Toad behind the arena
	l := mustLevel("2-4", b)
	l.Time = 300
	l.BowserDisguise = KindKoopa
	return l
}

// level9 is world 3's opener (night palette): the debut of the hammer
// bros — one pair on a block formation — plus the springboard over
// tall bricks and the goomba bridge with its hidden 1-UP.
func level9() *Level {
	b := NewBuilder(200, LevelHeight)
	b.Ground(0, 29)
	b.Ground(33, 72)   // pit 30-32
	b.Ground(77, 116)  // pit 73-76
	b.Ground(121, 199) // pit 117-120
	b.Set(3, 12, 'M')

	// Question blocks open the level (right-most pays the power-up)
	// while paratroopas hop toward the player.
	b.Set(10, 9, '?')
	b.Set(12, 9, '?')
	b.Set(14, 9, 'U')
	b.Set(19, 12, 'W')
	b.Set(23, 12, 'W')
	b.Pipe(26, 2)

	// The goomba bridge with the hidden 1-UP above it.
	b.Fill(36, 9, 42, 9, 'B')
	b.Set(40, 9, 'C') // the bridge's multi-coin brick, under the goombas' feet
	b.Set(37, 8, 'G')
	b.Set(39, 8, 'G')
	b.Set(41, 8, 'G')
	b.Set(39, 5, '1')
	b.Coins(8, 44, 45, 46)

	// Plant pipes, then the starman formation (left-most brick).
	b.Pipe(48, 3)
	b.Plant(48, 3)
	b.Pipe(52, 3)
	b.Plant(52, 3)
	b.Set(56, 9, 'S')
	b.Set(57, 9, 'B')
	b.Set(58, 9, 'B')
	b.Set(62, 12, 'K')
	b.Set(64, 12, 'G')
	b.Set(66, 12, 'G')

	// THE HAMMER BROS: exactly one pair on a block formation, the
	// second question of which pays the power-up.
	b.Fill(67, 9, 71, 9, 'B')
	b.Set(68, 9, '?')
	b.Set(70, 9, 'U')
	b.Set(68, 8, 'm')
	b.Set(70, 8, 'm')

	b.Set(80, 12, 'K')
	b.Set(82, 12, 'G')
	b.Set(84, 12, 'G')
	b.Pipe(88, 3)
	b.Plant(88, 3)
	b.Coins(9, 90, 91, 92)

	// Block row over the enemy groups.
	b.Fill(94, 9, 98, 9, 'B')
	b.Set(96, 9, '?')
	b.Coins(8, 94, 95, 96, 97, 98)
	b.Set(102, 12, 'K')
	b.Set(104, 12, 'G')
	b.Set(106, 12, 'G')

	// Stone pillar with the power-up on top, koopa pairs around it.
	b.Fill(126, 10, 126, 12, '#')
	b.Set(126, 6, 'U')
	b.Coins(9, 122, 123, 124, 125)
	b.Set(130, 12, 'K')
	b.Set(132, 12, 'K')
	b.StairsUp(136, 6)

	// The springboard over tall bricks (hidden-block alternative over
	// the two bricks, as in 2-1).
	b.Coins(9, 144, 145, 146, 147)
	b.Springboard(148, 12)
	b.Fill(152, 10, 153, 10, 'B')
	b.Set(153, 7, 'H')
	b.Fill(158, 5, 158, 12, 'B') // the tall brick wall

	// Stone stairs with goombas descending, then the finale.
	b.StairsDown(162, 4)
	b.Set(163, 9, 'G')
	b.Set(164, 10, 'G')
	b.StairsUp(172, 8)
	b.Flag(190)
	l := mustLevel("3-1", b)
	l.Time = 400
	l.Night = true
	return l
}

// level10 is world 3's koopa gauntlet (night palette): rows of koopas
// for shell-chain bowling, a lone plant pipe and the starman brick
// above the question block.
func level10() *Level {
	b := NewBuilder(190, LevelHeight)
	b.Ground(0, 29)
	b.Ground(34, 79)   // pit 30-33
	b.Ground(84, 129)  // pit 80-83
	b.Ground(134, 189) // pit 130-133
	b.Set(3, 12, 'M')

	// The gauntlet opens on koopa pairs.
	b.Set(10, 12, 'K')
	b.Set(13, 12, 'K')
	b.Set(16, 12, 'G')
	b.Set(18, 12, 'G')

	// Stone pillar with the power-up on top.
	b.Fill(22, 10, 22, 12, '#')
	b.Set(22, 6, 'U')
	b.Set(26, 12, 'K')

	b.Set(36, 12, 'G')
	b.Set(38, 12, 'G')

	// The question block with the starman brick above it.
	b.Set(44, 9, '?')
	b.Set(44, 5, 'S')
	b.Coins(9, 46, 47, 48)

	// First koopa row: kick a shell, bowl the rest.
	b.Set(50, 12, 'K')
	b.Set(53, 12, 'K')
	b.Set(56, 12, 'K')

	b.Set(62, 12, 'W')

	b.Set(70, 12, 'K')
	b.Set(72, 12, 'G')
	b.Set(74, 12, 'G')

	// Separated platform with a koopa, then the pillar.
	b.Fill(92, 9, 96, 9, 'B')
	b.Set(94, 8, 'K')
	b.Coins(9, 88, 89, 90)
	b.Fill(102, 10, 102, 12, '#')

	// The long koopa row.
	b.Set(106, 12, 'K')
	b.Set(109, 12, 'K')
	b.Set(112, 12, 'K')
	b.Set(115, 12, 'K')

	b.Pipe(120, 3)
	b.Plant(120, 3)

	// Closing pairs and the staircase.
	b.Set(138, 12, 'K')
	b.Set(140, 12, 'K')
	b.Set(142, 12, 'G')
	b.Set(144, 12, 'G')
	b.Coins(9, 146, 147, 148, 149)
	b.StairsUp(152, 8)
	b.Flag(172)
	l := mustLevel("3-2", b)
	l.Time = 300
	l.Night = true
	return l
}

// level11 is world 3's athletic night flight: island hops, horizontal
// lifts, a flimsy lift up to the high coin path and the balance-lift
// pair over the last gap — red koopas all the way.
func level11() *Level {
	b := NewBuilder(200, LevelHeight)
	b.Theme(ThemeSky)
	b.Ground(0, 17)
	b.Ground(22, 39)   // pit 18-21
	b.Ground(44, 71)   // pit 40-43 (lift)
	b.Ground(76, 103)  // pit 72-75 (lift)
	b.Ground(108, 135) // pit 104-107 (flimsy lift)
	b.Ground(140, 147) // pit 148-151 (balance lifts)
	b.Ground(152, 199)
	b.Set(3, 12, 'M')

	b.Set(28, 12, 'R')
	b.Coins(9, 30, 31, 32)

	// Horizontal lift over the gap, coins above its sweep.
	b.Lift(41, 10, 3, LiftHoriz, 4)
	b.Coins(7, 40, 41, 42, 43)

	b.Set(48, 12, 'G')
	b.Fill(52, 9, 56, 9, 'B')
	b.Set(54, 9, 'U')
	b.Set(60, 12, 'R')

	b.Lift(73, 9, 3, LiftHoriz, 4)
	b.Coins(6, 72, 73, 74, 75)

	b.Set(82, 7, 'r') // red paratroopa flying up/down
	b.Set(88, 12, 'R')

	// The flimsy lift: board it, jump quick — it falls — to the high
	// brick path and its coin run.
	b.Lift(105, 9, 3, LiftFlimsy, 0)
	b.Fill(110, 6, 118, 6, 'B')
	b.Coins(5, 111, 112, 113, 114, 115, 116, 117)

	b.Set(124, 12, 'R')

	b.Lift(137, 10, 3, LiftHoriz, 4)
	b.Coins(7, 136, 137, 138, 139)

	// The balance pair: standing on one lowers it and raises the other.
	b.Lift(145, 9, 3, LiftPulley, 3)
	b.Lift(152, 9, 3, LiftPulley, 3)

	b.Set(160, 12, 'R')
	b.StairsUp(176, 8)
	b.Flag(192)
	l := mustLevel("3-3", b)
	l.Time = 300
	l.Night = true
	return l
}

// level12 is world 3's castle: a hall of fire-bar pillars over boiling
// lava — six pools, six lava bubbles — the three question blocks, and
// the fake Bowser (a buzzy beetle in disguise) behind his brick
// barrier with a horizontal lift above. Toad waits behind the arena:
// the princess is in another castle (4-4, since world 4 exists).
func level12() *Level {
	b := NewBuilder(200, LevelHeight)
	b.Theme(ThemeCastle)
	b.Ground(0, 29)
	b.Ground(34, 59)   // lava 30-33
	b.Ground(64, 89)   // lava 60-63
	b.Ground(94, 103)  // lava 90-93
	b.Ground(108, 119) // lava 104-107
	b.Ground(124, 133) // lava 120-123
	b.Ground(138, 199) // lava 134-137
	b.Ceiling()        // the castle brick ceiling
	b.Fill(30, 13, 33, 14, 'L')
	b.Fill(60, 13, 63, 14, 'L')
	b.Fill(90, 13, 93, 14, 'L')
	b.Fill(104, 13, 107, 14, 'L')
	b.Fill(120, 13, 123, 14, 'L')
	b.Fill(134, 13, 137, 14, 'L')
	b.Set(3, 12, 'M')

	// Every pool boils with a lava bubble.
	b.Set(31, 13, 'o')
	b.Set(61, 13, 'o')
	b.Set(91, 13, 'o')
	b.Set(105, 13, 'o')
	b.Set(121, 13, 'o')
	b.Set(135, 13, 'o')

	// Fire bars: a brick pillar with a rotating hub on top. Alternate
	// hub columns spin opposite ways (see NewFireBar).
	pillar := func(x int) {
		b.Fill(x, 11, x, 12, 'B')
		b.Set(x, 10, 'h')
	}

	// The opening hall: three lone pillars between the first pools.
	pillar(38)
	pillar(46)
	pillar(54)

	// The three question blocks — the centre one pays the power-up.
	b.Set(70, 9, '?')
	b.Set(72, 9, 'U')
	b.Set(74, 9, '?')

	// Three sets of paired pillars.
	pillar(78)
	pillar(83)
	b.Coins(9, 86, 87, 88)
	pillar(98)
	pillar(101)
	b.Coins(9, 112, 113)
	pillar(128)
	pillar(131)

	// The boss arena: a short stair up, a brick barrier on the bridge
	// in front of the fake Bowser, a horizontal lift above it all and
	// the axe behind him.
	b.StairsUp(150, 4)
	b.Fill(158, 13, 165, 14, 'L') // the lava pool under the bridge
	for x := 158; x <= 165; x++ {
		b.Set(x, 13, 'b') // planks flush to the ledge at 166
	}
	b.Fill(159, 10, 159, 12, 'B') // the brick barrier
	b.Lift(160, 7, 3, LiftHoriz, 4)
	b.Set(162, 12, 'Z') // fake Bowser (buzzy beetle), on the bridge
	b.Set(169, 12, 'x') // the axe
	b.Set(178, 12, 't') // the toad — the princess is in another castle (4-4)
	l := mustLevel("3-4", b)
	l.Time = 300
	l.BowserDisguise = KindBuzzy
	return l
}
