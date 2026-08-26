package engine

import "fmt"

// Theme is a level's visual world: palette and scenery selection.
type Theme uint8

const (
	ThemeOverworld Theme = iota
	ThemeUnderground
	ThemeSky    // athletic: pale sky, sandstone terrain, clouds only
	ThemeCastle // finale: black sky, grey stone, lava pools, fire bars
)

// Tile is a single grid cell of a level.
type Tile uint8

const (
	Empty Tile = iota
	Ground
	Brick
	Question     // question block containing a coin
	QuestionMush // question block containing a mushroom
	QuestionFire // question block containing a fire flower
	QuestionStar // question block containing a star
	Used         // spent question block
	Pipe
	Lava       // castle hazard: not solid, kills on touch
	HiddenCoin // invisible block: only bumps from below, pays a coin
	HiddenLife // invisible block: only bumps from below, pays a 1-UP
	FlagPole
	FlagTop
)

// Solid reports whether bodies collide with the tile.
func (t Tile) Solid() bool {
	switch t {
	case Ground, Brick, Question, QuestionMush, QuestionFire, QuestionStar, Used, Pipe:
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
	Theme         Theme
	CheckpointX   float64 // mid-level respawn column

	PlayerStart  Vec
	GoombaSpawns []Vec
	KoopaSpawns  []Vec
	ParaSpawns   []Vec // flying koopas (hop while walking)
	CoinSpawns   []Vec
	PlantSpawns  []Vec     // piranha plants; Y is the pipe-mouth row
	BarSpawns    []FireBar // castle fire bars; hub centre in tile coords
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
	'f': QuestionFire,
	'S': QuestionStar,
	'P': Pipe,
	'L': Lava,
	'H': HiddenCoin,
	'1': HiddenLife,
	'F': FlagPole,
	'T': FlagTop,
}

// ParseLevel builds a level from ASCII rows. Tile characters:
//
//	' ' empty    '#' ground   'B' brick   '?' question (coin)
//	'U' question (mushroom)   'f' question (fire flower)
//	'S' question (star)       'P' pipe     'L' lava (castle)
//	'H' hidden coin block     '1' hidden 1-UP block (bump from below only)
//	'F' flag pole   'T' flag top
//	'G' goomba   'K' koopa   'W' flying koopa   'c' coin   'M' player start
//	'V' piranha plant (on the pipe below its cell)
//	'h' fire-bar hub (rotating hazard anchored at the cell centre)
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
			case 'W':
				l.ParaSpawns = append(l.ParaSpawns, Vec{float64(x), float64(y) + 1 - KoopaH})
			case 'h': // fire-bar hub: rotates about the cell centre
				l.BarSpawns = append(l.BarSpawns, NewFireBar(float64(x), float64(y)))
			case 'c':
				l.CoinSpawns = append(l.CoinSpawns, Vec{float64(x) + 0.2, float64(y) + 0.2})
			case 'V': // air cell above a pipe's left column
				l.PlantSpawns = append(l.PlantSpawns, Vec{float64(x) + 0.65, float64(y) + 1})
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
	l.computeCheckpoint()
	return l, nil
}

// computeCheckpoint picks the mid-level respawn column: the first column
// at or past the middle with two rows of ground under it and standing room
// above. Falls back to the player start.
func (l *Level) computeCheckpoint() {
	ground := l.Height - 2
	for x := l.Width / 2; x < l.Width-8; x++ {
		if !l.At(x, ground).Solid() || !l.At(x, ground+1).Solid() {
			continue
		}
		if l.At(x, ground-1).Solid() || l.At(x, ground-2).Solid() {
			continue // no standing room (pipe, blocks, stairs)
		}
		l.CheckpointX = float64(x)
		return
	}
	l.CheckpointX = l.PlayerStart.X
}
