package engine

import (
	"fmt"
	"math"
)

// Theme is a level's visual world: palette and scenery selection.
type Theme uint8

// Level themes, weakest flavour first; the renderer picks palettes and
// scenery per theme.
const (
	ThemeOverworld Theme = iota
	ThemeUnderground
	ThemeSky    // athletic: pale sky, sandstone terrain, clouds only
	ThemeCastle // finale: black sky, grey stone, lava pools, fire bars
)

// Tile is a single grid cell of a level.
type Tile uint8

// Level tiles. Empty is the zero value so a fresh grid is open air;
// Solid carries the collision subset (see Tile.Solid).
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
	TileBridge // castle boss bridge: solid plank over the lava pool
)

// Solid reports whether bodies collide with the tile.
func (t Tile) Solid() bool {
	switch t {
	case Ground, Brick, Question, QuestionMush, QuestionFire, QuestionStar, Used, Pipe, TileBridge:
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
	AxeX, AxeY    int // the boss-arena goal marker ('x'); -1 when absent
	Theme         Theme
	CheckpointX   float64 // mid-level respawn column

	PlayerStart  Vec
	GoombaSpawns []Vec
	KoopaSpawns  []Vec
	ParaSpawns   []Vec // flying koopas (hop while walking)
	CoinSpawns   []Vec
	PlantSpawns  []Vec     // piranha plants; Y is the pipe-mouth row
	BarSpawns    []FireBar // castle fire bars; hub centre in tile coords
	BowserSpawns []Vec     // boss spawns; feet planted on the row below the marker

	// Warps are this level's enterable pipes (see warp.go): press Down
	// while standing on the mouth to travel. Nil on levels without one.
	Warps []Warp
}

// Warp is one enterable pipe. X/Top locate the pipe in the level that
// owns the warp (left column of the two-wide pipe, mouth row); Dest is
// the destination level — nil means the run's main level — whose pipe
// at DestX/DestTop is the one the player rises back out of.
type Warp struct {
	X, Top         int
	Dest           *Level
	DestX, DestTop int
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

// poleTopRow returns the row of the flagpole's topmost tile (the finial)
// in the flag column. Builder.Flag places it at FlagTopRow; the scan keeps
// hand-authored rows honest, and the fallback covers flagless levels.
func (l *Level) poleTopRow() int {
	for ty := range l.Height {
		if t := l.At(l.FlagX, ty); t == FlagTop || t == FlagPole {
			return ty
		}
	}
	return FlagTopRow
}

// GoalX returns the level's goal column: the flagpole when the level
// ends by pole, else the axe (the boss arena's goal). -1 when neither
// exists (warp rooms) — the castle walk, the reachability check and the
// renderer's castle all no-op on that.
func (l *Level) GoalX() int {
	if l.FlagX >= 0 {
		return l.FlagX
	}
	return l.AxeX
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
	'b': TileBridge,
}

// ParseLevel builds a level from ASCII rows. Tile characters:
//
//	' ' empty    '#' ground   'B' brick   '?' question (coin)
//	'U' question (mushroom)   'f' question (fire flower)
//	'S' question (star)       'P' pipe     'L' lava (castle)
//	'H' hidden coin block     '1' hidden 1-UP block (bump from below only)
//	'F' flag pole   'T' flag top   'b' bridge plank (solid, over lava)
//	'G' goomba   'K' koopa   'W' flying koopa   'c' coin   'M' player start
//	'V' piranha plant (on the pipe below its cell)
//	'h' fire-bar hub (rotating hazard anchored at the cell centre)
//	'Z' Bowser (feet planted on the row below the marker)
//	'x' axe — the boss arena's goal (one per level; last one wins)
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
		AxeX:   -1,
		AxeY:   -1,
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
				l.PlantSpawns = append(l.PlantSpawns, Vec{float64(x) + PlantCenterOffset, float64(y) + 1})
			case 'Z': // Bowser: feet planted on the row below the marker
				l.BowserSpawns = append(l.BowserSpawns, Vec{float64(x), float64(y) + 1 - BowserH})
			case 'x': // the axe: the boss arena's goal (last one wins)
				l.AxeX, l.AxeY = x, y
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

	// A level without a flag is legal: it is a warp room (or an arena)
	// that play never ends by pole — FlagX stays -1 and Update's flag
	// grab, the castle walk and the renderer's castle all no-op on it.
	if !playerSet {
		l.PlayerStart = Vec{1, float64(l.Height-3) + 1 - SmallH}
	}
	l.computeCheckpoint()
	return l, nil
}

// computeCheckpoint picks the mid-level respawn column: the first column
// at or past the middle with two rows of ground under it, standing room
// above, and no spawned threat at the column. Enemies reset to their
// spawn points when the level reloads, so a spawn overlapping the
// respawn footprint killed the player on the first playing tick — 1-1's
// guard goomba sat exactly on the auto-picked column and every life
// after a death past the checkpoint drained in a death loop (live bug,
// 2026-08-30). Falls back to the player start.
func (l *Level) computeCheckpoint() {
	// The two ground rows every level stands on sit at the bottom, so
	// GroundTop is the ground surface a respawn column must have.
	ground := GroundTop
	for x := l.Width / 2; x < l.Width-8; x++ {
		if !l.At(x, ground).Solid() || !l.At(x, ground+1).Solid() {
			continue
		}
		if l.At(x, ground-1).Solid() || l.At(x, ground-2).Solid() {
			continue // no standing room (pipe, blocks, stairs)
		}
		if l.spawnThreatNear(float64(x), ground) {
			continue // an enemy spawn or fire bar would kill on arrival
		}
		l.CheckpointX = float64(x)
		return
	}
	l.CheckpointX = l.PlayerStart.X
}

// spawnThreatNear reports whether a player standing with its feet on
// (colX, ground) would overlap — or sit inside a fire bar's sweep — a
// threat as it spawns. Enemy spawns reset on every level reload, so this
// is exactly the world a respawning player walks into. Plants count too:
// their rise mercy keeps a hidden plant down, but a pipe mouth under the
// respawn column is still a trap the moment the player steps off it.
func (l *Level) spawnThreatNear(colX float64, ground int) bool {
	for _, s := range l.GoombaSpawns {
		if math.Abs(s.X-colX) < SmallW+GoombaW {
			return true
		}
	}
	for _, s := range l.KoopaSpawns {
		if math.Abs(s.X-colX) < SmallW+KoopaW {
			return true
		}
	}
	for _, s := range l.ParaSpawns {
		if math.Abs(s.X-colX) < SmallW+KoopaW {
			return true
		}
	}
	for _, s := range l.PlantSpawns {
		if math.Abs(s.X+PlantW/2-(colX+SmallW/2)) < (SmallW+PlantW)/2+0.25 {
			return true
		}
	}
	// A bowser is a two-tile wall of boss: an AABB test on both axes
	// (centres closer than the half-extents summed) keeps a respawn
	// column out of his spawn box.
	for _, s := range l.BowserSpawns {
		if math.Abs(s.X+BowserW/2-(colX+SmallW/2)) < (SmallW+BowserW)/2 &&
			math.Abs(s.Y+BowserH/2-(float64(ground)-SmallH/2)) < (SmallH+BowserH)/2 {
			return true
		}
	}
	// A fire bar sweeps a full disc around its hub: MaxReach (entity.go)
	// is the far edge of the outermost ball's collision box, the exact
	// law the sweep guard must mirror — counting only FireBarLen-1
	// gaps left it a tile short of the sweep.
	x0, x1 := colX, colX+SmallW
	y0, y1 := float64(ground)-SmallH, float64(ground)
	for _, fb := range l.BarSpawns {
		reach := fb.MaxReach()
		dx, dy := 0.0, 0.0
		if fb.X < x0 {
			dx = x0 - fb.X
		} else if fb.X > x1 {
			dx = fb.X - x1
		}
		if fb.Y < y0 {
			dy = y0 - fb.Y
		} else if fb.Y > y1 {
			dy = fb.Y - y1
		}
		if dx*dx+dy*dy < reach*reach {
			return true
		}
	}
	return false
}
