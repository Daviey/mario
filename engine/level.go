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
	ThemeSky        // athletic: pale sky, sandstone terrain, clouds only
	ThemeCastle     // finale: black sky, grey stone, lava pools, fire bars
	ThemeUnderwater // 2-2: blue-teal swim world (the engine keys swim off Level.Underwater, not the theme)
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
	BrickCoin  // multi-coin brick: pays per bump until count or clock runs out
	BrickVine  // vine brick: bump spends it and sprouts the beanstalk ('J')
)

// Solid reports whether bodies collide with the tile.
func (t Tile) Solid() bool {
	switch t {
	case Ground, Brick, BrickCoin, BrickVine, Question, QuestionMush, QuestionFire, QuestionStar, Used, Pipe, TileBridge:
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

	// SMB1-fidelity extension fields (contract S1): starting timer,
	// world flavour flags, the new hazard spawners and the
	// castle-cutscene retainer. All optional — zero values reproduce
	// the classic behaviour, and the engine keys every rule off these
	// flags (not the theme) so a custom level can opt in piecemeal.
	Time         int  // starting timer units; 0 → StartTime. Ground/underground/underwater 400, athletic/castle 300
	Night        bool // world-3 night palette (render-side only)
	Underwater   bool // swim regime + cheep-swim spawner (theme-independent flag)
	CheepLeaping bool // 2-3 bridge leap spawner active
	CheepStopX   int  // leaping stops once the player X reaches this (the stone steps)

	Currents       []CurrentZone // 2-2 downward drag zones
	PodobooSpawns  []Vec         // lava pools: X = pool centre column, Y = lava surface row (raw; newPodoboo takes these)
	BlooberSpawns  []Vec         // bloobers, feet-planted like a goomba (1.0 tall, so Y = marker row)
	HammerSpawns   []Vec         // hammer bros, feet-planted (1.7 tall: Y = marker row - 0.7)
	BuzzySpawns    []Vec         // buzzy beetles: koopa box, feet-planted like 'K'
	KoopaRedSpawns []Vec         // red koopas (edge-turners), feet-planted like 'K'
	ParaRedSpawns  []Vec         // red vertical paratroopas, feet-planted like 'W'

	Retainer   int // 0 none, 1 toad, 2 princess: castle cutscene after the castle walk
	RetainerAt Vec // the retainer's cell ('t'/'p' marker); the player walks to X-1.5

	BowserDisguise EnemyKind // the castle boss's true identity: KindGoomba (none), KindKoopa, KindBuzzy (fakes, all of them)

	LiftSpawns   []LiftSpawn // rideable platforms (Builder.Lift); Y is the platform's top surface
	SpringSpawns []Vec       // springboards: X = left edge, Y = top surface (box 1 wide, 0.5 tall)

	// The beanstalk (vine.go): the vine brick's bump destination, and —
	// on room levels — the fall-out exit column. Both optional.
	VineRoom  *Level // 1-1's Coin Heaven; nil = the stalk tops out bare
	DropExitX int    // room-only: falling out below the room returns to the main level at this X
}

// CurrentZone is one of 2-2's downward currents: a column span that
// drags a swimming player toward the waterbed (and the pit below it).
type CurrentZone struct {
	X0, X1 int     // inclusive tile-column span of the drag zone
	Drag   float64 // extra downward velocity per tick inside the zone
}

// Current adds one of 2-2's downward currents: every tick a swimming
// player whose centre sits inside the inclusive column span [x0, x1]
// sinks CurrentDrag faster (see updatePlayer). The builder records it;
// mustLevel (levels.go) carries it onto the parsed level.
func (b *Builder) Current(x0, x1 int) {
	b.currents = append(b.currents, CurrentZone{X0: x0, X1: x1, Drag: CurrentDrag})
}

// Warp is one enterable pipe. X/Top locate the pipe in the level that
// owns the warp (left column of the two-wide pipe, mouth row); Dest is
// the destination level — nil means the run's main level — whose pipe
// at DestX/DestTop is the one the player rises back out of. A warp with
// JumpTo > 0 ignores Dest entirely: it is a warp-zone pipe that skips
// the run ahead to that 0-based level index (see performWarp).
type Warp struct {
	X, Top         int
	Dest           *Level
	DestX, DestTop int
	JumpTo         int // 0 = ordinary room travel; else 0-based target level index
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
	'C': BrickCoin,
	'J': BrickVine,
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
//	' ' empty    '#' ground   'B' brick   'C' multi-coin brick   '?' question (coin)
//	'U' question (mushroom)   'f' question (fire flower)
//	'S' question (star)       'P' pipe     'L' lava (castle)
//	'H' hidden coin block     '1' hidden 1-UP block (bump from below only)
//	'F' flag pole   'T' flag top   'b' bridge plank (solid, over lava)
//	'G' goomba   'K' koopa   'W' flying koopa   'c' coin   'M' player start
//	'z' buzzy beetle (koopa box; fire-immune, stomps to a shell)   'R' red koopa
//	'r' red vertical paratroopa   'q' bloober   'm' hammer bro   'o' podoboo
//	'V' piranha plant (on the pipe below its cell)
//	'h' fire-bar hub (rotating hazard anchored at the cell centre)
//	'Z' Bowser (feet planted on the row below the marker)
//	'x' axe — the boss arena's goal (one per level; last one wins)
//	't' toad retainer / 'p' princess: the castle cutscene after the walk
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
			case 'z': // buzzy beetle: koopa box, feet planted like a koopa
				l.BuzzySpawns = append(l.BuzzySpawns, Vec{float64(x), float64(y) + 1 - KoopaH})
			case 'R': // red koopa: the edge-turner
				l.KoopaRedSpawns = append(l.KoopaRedSpawns, Vec{float64(x), float64(y) + 1 - KoopaH})
			case 'r': // red paratroopa: the vertical flyer
				l.ParaRedSpawns = append(l.ParaRedSpawns, Vec{float64(x), float64(y) + 1 - KoopaH})
			case 'q': // bloober: 1.0 tall, so the planted spawn equals the marker row
				l.BlooberSpawns = append(l.BlooberSpawns, Vec{float64(x), float64(y)})
			case 'm': // hammer bro: 1.7 tall, feet planted on the row below the marker
				l.HammerSpawns = append(l.HammerSpawns, Vec{float64(x), float64(y) - 0.7})
			case 'o': // podoboo: X = lava-pool centre column, Y = lava surface row (raw)
				l.PodobooSpawns = append(l.PodobooSpawns, Vec{float64(x), float64(y)})
			case 't': // toad retainer: the castle cutscene waits at this cell
				l.Retainer = 1
				l.RetainerAt = Vec{float64(x), float64(y)}
			case 'p': // princess retainer (the final castle's ending)
				l.Retainer = 2
				l.RetainerAt = Vec{float64(x), float64(y)}
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
// walkInBerth widens a walker's respawn berth past its spawn box:
// enemies patrol the moment the world card hands control back, and a
// walker parked just outside the static berth reaches the column in
// under a second (live 2026-09-02: a goomba 1.8 tiles from 2-4's
// checkpoint closed the gap in 39 ticks and looped every remaining
// life; 3-1's koopa did the same from 0.8 tiles past the column).
const walkInBerth = WorldCardTicks * EnemyWalk

func (l *Level) spawnThreatNear(colX float64, ground int) bool {
	for _, s := range l.GoombaSpawns {
		if math.Abs(s.X-colX) < SmallW+GoombaW+walkInBerth {
			return true
		}
	}
	for _, s := range l.KoopaSpawns {
		if math.Abs(s.X-colX) < SmallW+KoopaW+walkInBerth {
			return true
		}
	}
	for _, s := range l.ParaSpawns {
		if math.Abs(s.X-colX) < SmallW+KoopaW+walkInBerth {
			return true
		}
	}
	// The SMB1-fidelity walkers ride the same rule as their green
	// cousins — buzzy beetles and red koopas/paratroopas share the
	// koopa box, a hammer bro gets its patrol berth — widened by the
	// same walk-in margin.
	for _, s := range l.BuzzySpawns {
		if math.Abs(s.X-colX) < SmallW+KoopaW+walkInBerth {
			return true
		}
	}
	for _, s := range l.KoopaRedSpawns {
		if math.Abs(s.X-colX) < SmallW+KoopaW+walkInBerth {
			return true
		}
	}
	for _, s := range l.ParaRedSpawns {
		if math.Abs(s.X-colX) < SmallW+KoopaW+walkInBerth {
			return true
		}
	}
	for _, s := range l.HammerSpawns {
		if math.Abs(s.X-colX) < SmallW+KoopaW+walkInBerth {
			return true
		}
	}
	// Bloobers drift through walls toward the player, so a spawn on the
	// respawn column is a trap even underwater (plant-style centre test;
	// a bloober is 0.9 wide).
	for _, s := range l.BlooberSpawns {
		if math.Abs(s.X+0.45-(colX+SmallW/2)) < (SmallW+0.9)/2+0.25 {
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
	// A podoboo arcs PodobooJumpVel's worth of height above its lava
	// surface on its own column: a respawn column inside the leap lane
	// at a height the arc sweeps through is a trap whenever the phase
	// window lands (the guard treats the whole arc band as threatened —
	// phases are pure functions of the tick, so any offset is possible).
	for _, s := range l.PodobooSpawns {
		if math.Abs(s.X+PodobooW/2-(colX+SmallW/2)) < (SmallW+PodobooW)/2 {
			top := s.Y - (PodobooJumpVel*PodobooJumpVel)/(2*Gravity) - PodobooH
			if float64(ground) > top && float64(ground)-SmallH < s.Y {
				return true
			}
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
