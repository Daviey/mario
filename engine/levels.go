package engine

// Level geometry conventions: levels are 15 rows tall with two rows of
// ground at the bottom (rows 13-14). X grows right, Y grows down.

const (
	LevelHeight = 15
	GroundTop   = 13
)

// Builder assembles ASCII level rows programmatically.
type Builder struct {
	w, h int
	rows [][]byte
}

// NewBuilder returns a blank (all empty) builder.
func NewBuilder(w, h int) *Builder {
	b := &Builder{w: w, h: h, rows: make([][]byte, h)}
	for y := range b.rows {
		b.rows[y] = make([]byte, w)
		for x := range b.rows[y] {
			b.rows[y][x] = ' '
		}
	}
	return b
}

// Set writes a character, ignoring out-of-bounds positions.
func (b *Builder) Set(x, y int, ch byte) {
	if x < 0 || x >= b.w || y < 0 || y >= b.h {
		return
	}
	b.rows[y][x] = ch
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

// Pipe places a two-tile-wide pipe of the given height on the ground.
// Height must be <= 3 to stay clearable with a normal jump.
func (b *Builder) Pipe(x, height int) {
	b.Fill(x, GroundTop-height, x+1, GroundTop-1, 'P')
}

// StairsUp builds an ascending staircase of h steps (1..h blocks tall).
func (b *Builder) StairsUp(x, h int) {
	for i := 0; i < h; i++ {
		b.Fill(x+i, GroundTop-1-i, x+i, GroundTop-1, '#')
	}
}

// StairsDown builds a descending staircase starting h blocks tall.
func (b *Builder) StairsDown(x, h int) {
	for i := 0; i < h; i++ {
		b.Fill(x+i, GroundTop-(h-i), x+i, GroundTop-1, '#')
	}
}

// Coins places collectible coins at the given columns on one row.
func (b *Builder) Coins(y int, xs ...int) {
	for _, x := range xs {
		b.Set(x, y, 'c')
	}
}

// Flag places the goal flagpole at column x.
func (b *Builder) Flag(x int) {
	b.Fill(x, 8, x, 12, 'F')
	b.Set(x, 7, 'T')
}

// Rows returns the assembled ASCII rows.
func (b *Builder) Rows() []string {
	out := make([]string, b.h)
	for y := range b.rows {
		out[y] = string(b.rows[y])
	}
	return out
}

func mustLevel(name string, b *Builder) *Level {
	l, err := ParseLevel(name, b.Rows())
	if err != nil {
		panic(err)
	}
	return l
}

// DefaultLevels returns the three built-in levels.
func DefaultLevels() []*Level {
	return []*Level{level1(), level2(), level3()}
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
	b.Set(38, 12, 'G')
	b.Pipe(41, 3)
	b.Set(46, 12, 'G')

	b.Coins(9, 56, 57, 58)
	for x, ch := range map[int]byte{60: 'B', 61: '?', 62: 'B', 63: 'U', 64: 'B', 65: '?', 66: 'B'} {
		b.Set(x, 9, ch)
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
	return mustLevel("1-1", b)
}

func level2() *Level {
	b := NewBuilder(170, LevelHeight)
	b.Ground(0, 29)
	b.Ground(33, 60)   // pit 30-32
	b.Ground(65, 100)  // pit 61-64 (needs a running jump)
	b.Ground(105, 169) // pit 101-104 (running jump)
	b.Set(3, 12, 'M')

	b.Set(10, 9, '?')
	b.Pipe(14, 3)
	b.Set(18, 12, 'G')
	b.Set(22, 9, 'B')
	b.Set(23, 9, '?')
	b.Set(24, 9, 'U')
	b.Set(25, 9, 'B')
	b.Set(26, 9, 'B')
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
	b.Set(70, 12, 'G')
	b.Pipe(72, 3)
	b.Set(78, 12, 'G')
	b.Set(80, 12, 'G')

	b.Fill(86, 9, 92, 9, 'B')
	b.Set(87, 9, '?')
	b.Set(91, 9, '?')
	b.Fill(88, 5, 90, 5, 'B')
	b.Set(89, 5, 'U')
	b.Set(96, 12, 'K')

	b.Fill(106, 10, 110, 10, 'B')
	b.Coins(8, 106, 107, 108, 109, 110)
	b.Set(112, 12, 'K')
	b.Set(116, 12, 'G')
	b.Set(118, 12, 'G')
	b.Set(120, 12, 'G')

	b.Pipe(124, 3)
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

	b.Set(24, 12, 'G')
	b.Set(26, 12, 'G')
	b.Fill(30, 10, 33, 10, 'B')
	b.Coins(8, 30, 31, 32, 33)
	b.Set(36, 12, 'K')

	b.Fill(40, 9, 43, 9, 'B')
	b.Set(41, 9, 'U')

	b.Fill(50, 10, 53, 10, 'B')
	b.Set(52, 9, 'G')
	b.Fill(56, 8, 59, 8, 'B')
	b.Coins(6, 56, 57, 58, 59)

	b.Set(64, 12, 'K')
	b.Set(68, 12, 'G')
	b.Set(70, 12, 'G')
	b.Fill(74, 9, 78, 9, 'B')
	b.Set(76, 9, '?')

	b.Pipe(86, 3)
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
