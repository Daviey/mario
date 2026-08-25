package engine

import "fmt"

// Tile is a single grid cell of a level.
type Tile uint8

const (
	Empty Tile = iota
	Ground
	Brick
	Question     // question block containing a coin
	QuestionMush // question block containing a mushroom
	Used         // spent question block
	Pipe
	FlagPole
	FlagTop
)

// Solid reports whether bodies collide with the tile.
func (t Tile) Solid() bool {
	switch t {
	case Ground, Brick, Question, QuestionMush, Used, Pipe:
		return true
	}
	return false
}

// Level is static level data: the tile grid plus entity spawn points.
// A working copy is made per load so tiles can mutate (used blocks, broken
// bricks) without corrupting the source level.
type Level struct {
	Name          string
	Width, Height int
	Tiles         []Tile
	FlagX         int

	PlayerStart  Vec
	GoombaSpawns []Vec
	KoopaSpawns  []Vec
	CoinSpawns   []Vec
}

// At returns the tile at a grid position. Out-of-level sides act as solid
// walls; above and below the level is open air.
func (l *Level) At(tx, ty int) Tile {
	if ty < 0 || ty >= l.Height {
		return Empty
	}
	if tx < 0 || tx >= l.Width {
		return Ground
	}
	return l.Tiles[ty*l.Width+tx]
}

// Set overwrites the tile at a grid position, ignoring out-of-range cells.
func (l *Level) Set(tx, ty int, t Tile) {
	if tx < 0 || tx >= l.Width || ty < 0 || ty >= l.Height {
		return
	}
	l.Tiles[ty*l.Width+tx] = t
}

var tileChars = map[byte]Tile{
	' ': Empty,
	'#': Ground,
	'B': Brick,
	'?': Question,
	'U': QuestionMush,
	'P': Pipe,
	'F': FlagPole,
	'T': FlagTop,
}

// ParseLevel builds a level from ASCII rows. Tile characters:
//
//	' ' empty    '#' ground   'B' brick   '?' question (coin)
//	'U' question (mushroom)   'P' pipe    'F' flag pole   'T' flag top
//	'G' goomba   'K' koopa    'c' coin    'M' player start
//
// Rows are padded with spaces to the width of the longest row. Entity
// characters are removed from the tile grid and turned into spawn points.
func ParseLevel(name string, rows []string) (*Level, error) {
	if len(rows) == 0 {
		return nil, fmt.Errorf("level %q: no rows", name)
	}
	w := 0
	for _, r := range rows {
		if len(r) > w {
			w = len(r)
		}
	}
	if w == 0 {
		return nil, fmt.Errorf("level %q: empty rows", name)
	}

	l := &Level{
		Name:   name,
		Width:  w,
		Height: len(rows),
		Tiles:  make([]Tile, w*len(rows)),
		FlagX:  -1,
	}
	playerSet := false

	for y, row := range rows {
		for x := 0; x < w; x++ {
			var ch byte = ' '
			if x < len(row) {
				ch = row[x]
			}
			switch ch {
			case 'M':
				l.PlayerStart = Vec{float64(x), float64(y) + 1 - SmallH}
				playerSet = true
			case 'G':
				l.GoombaSpawns = append(l.GoombaSpawns, Vec{float64(x), float64(y) + 1 - GoombaH})
			case 'K':
				l.KoopaSpawns = append(l.KoopaSpawns, Vec{float64(x), float64(y) + 1 - KoopaH})
			case 'c':
				l.CoinSpawns = append(l.CoinSpawns, Vec{float64(x) + 0.2, float64(y) + 0.2})
			case 'F', 'T':
				l.Tiles[y*w+x] = tileChars[ch]
				if l.FlagX < 0 || x < l.FlagX {
					l.FlagX = x
				}
			default:
				t, ok := tileChars[ch]
				if !ok {
					return nil, fmt.Errorf("level %q: unknown tile %q at (%d,%d)", name, ch, x, y)
				}
				l.Tiles[y*w+x] = t
			}
		}
	}

	if l.FlagX < 0 {
		return nil, fmt.Errorf("level %q: no flag", name)
	}
	if !playerSet {
		l.PlayerStart = Vec{1, float64(l.Height-3) + 1 - SmallH}
	}
	return l, nil
}
