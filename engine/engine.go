// Package engine contains the pure, terminal-independent game logic for the
// CLI Mario game. It advances at a fixed 60 Hz logical tick rate and is fully
// deterministic for a given input sequence, which keeps it unit-testable.
package engine

import "math"

// Input is the player input for one logical tick. Quit, Pause, Restart and
// AnyKey are edge triggered (set for exactly one tick per key press) and are
// produced by the input package.
type Input struct {
	Left, Right, Up, Down, Run bool
	Quit, Pause, Restart       bool
	AnyKey                     bool
}

// State is the high level game state.
type State int

const (
	StateTitle State = iota
	StatePlaying
	StateDying
	StateLevelClear
	StateGameOver
	StateWin
)

func (s State) String() string {
	switch s {
	case StateTitle:
		return "title"
	case StatePlaying:
		return "playing"
	case StateDying:
		return "dying"
	case StateLevelClear:
		return "level-clear"
	case StateGameOver:
		return "game-over"
	case StateWin:
		return "win"
	}
	return "unknown"
}

const (
	TicksPerSecond   = 60
	TicksPerTimeUnit = 24 // one game-time unit ticks down every 24 frames
	StartTime        = 300
	StartLives       = 3
	DyingTicks       = 150
	ClearTicks       = 150
	ExtraLifeCoins   = 100
)

// Game is a complete playthrough: levels, lives, score and the live world
// state of the current level. All fields are safe to read for rendering.
type Game struct {
	ViewW, ViewH int
	Levels       []*Level
	Level        *Level
	Player       *Player
	Enemies      []*Enemy
	CoinItems    []*CoinItem
	Mushrooms    []*Mushroom
	Particles    []*Particle

	Score     int
	CoinCount int
	Lives     int
	Time      int
	State     State
	Paused    bool
	CameraX   float64
	Tick      int

	levelIndex int
	stateTimer int
	bumps      map[int]int // tile index -> remaining bump animation ticks
	prevIn     Input
	curIn      Input
}

// NewGame creates a game over the given levels. viewW/viewH is the camera
// viewport in tiles; viewW is clamped down to the narrowest level.
func NewGame(levels []*Level, viewW, viewH int) *Game {
	for _, l := range levels {
		if l.Width < viewW {
			viewW = l.Width
		}
	}
	g := &Game{
		Levels: levels,
		ViewW:  viewW,
		ViewH:  viewH,
		Lives:  StartLives,
		bumps:  map[int]int{},
	}
	g.loadLevel(0, false)
	g.State = StateTitle
	return g
}

// LevelIndex returns the 0-based index of the current level.
func (g *Game) LevelIndex() int { return g.levelIndex }

// LevelName returns the display name of the current level (e.g. "1-1").
func (g *Game) LevelName() string { return g.Level.Name }

// Reset starts a brand new game (used by the restart key).
func (g *Game) Reset() {
	g.Score = 0
	g.CoinCount = 0
	g.Lives = StartLives
	g.loadLevel(0, false)
	g.State = StatePlaying
}

// Update advances the game by one logical tick with the given input.
func (g *Game) Update(in Input) {
	g.Tick++
	switch g.State {
	case StateTitle:
		if in.AnyKey || in.Restart || in.Left || in.Right || in.Up || in.Down {
			g.State = StatePlaying
		}
	case StatePlaying:
		if in.Pause {
			g.Paused = !g.Paused
		}
		if !g.Paused {
			g.updatePlaying(in)
		}
	case StateDying:
		g.stateTimer--
		g.updateDying()
		if g.stateTimer <= 0 {
			if g.Lives > 0 {
				g.loadLevel(g.levelIndex, false)
				g.State = StatePlaying
			} else {
				g.State = StateGameOver
			}
		}
	case StateLevelClear:
		g.stateTimer--
		if g.stateTimer <= 0 {
			if g.levelIndex+1 < len(g.Levels) {
				g.loadLevel(g.levelIndex+1, g.Player.Super)
				g.State = StatePlaying
			} else {
				g.State = StateWin
			}
		}
	case StateGameOver, StateWin:
		if in.Restart {
			g.Reset()
		}
	}
	g.prevIn = in
}

func (g *Game) updatePlaying(in Input) {
	g.curIn = in

	if g.Tick%TicksPerTimeUnit == 0 {
		g.Time--
		if g.Time <= 0 {
			g.Time = 0
			g.kill()
			return
		}
	}

	g.updatePlayer(in)
	if g.State != StatePlaying {
		return
	}

	if g.Player.Pos.X+g.Player.W >= float64(g.Level.FlagX)+0.3 {
		g.clearLevel()
		return
	}

	g.updateEnemies()
	g.playerEnemyInteractions()
	if g.State != StatePlaying {
		return
	}

	g.updateMushrooms()
	g.collectCoins()
	g.updateParticles()
	g.decayBumps()
	g.updateCamera()

	if g.Player.Pos.Y > float64(g.Level.Height)+1 {
		g.kill()
		return
	}
	g.cleanup()
}

func (g *Game) updateDying() {
	p := g.Player
	p.Vel.Y += Gravity
	if p.Vel.Y > MaxFall {
		p.Vel.Y = MaxFall
	}
	p.Pos.Y += p.Vel.Y
}

func (g *Game) kill() {
	if g.State != StatePlaying {
		return
	}
	g.Lives--
	g.State = StateDying
	g.stateTimer = DyingTicks
	g.Player.Vel.X = 0
	g.Player.Vel.Y = -0.38
}

func (g *Game) clearLevel() {
	g.State = StateLevelClear
	g.stateTimer = ClearTicks
	g.Score += g.Time * TimeBonusPerUnit
	g.Player.Vel = Vec{}
}

func (g *Game) updateCamera() {
	target := g.Player.Pos.X - float64(g.ViewW)*0.35
	if target < 0 {
		target = 0
	}
	if max := float64(g.Level.Width - g.ViewW); target > max {
		target = max
	}
	g.CameraX = target
}

func (g *Game) loadLevel(i int, super bool) {
	src := g.Levels[i]
	lvl := &Level{
		Name:        src.Name,
		Width:       src.Width,
		Height:      src.Height,
		FlagX:       src.FlagX,
		Tiles:       make([]Tile, len(src.Tiles)),
		PlayerStart: src.PlayerStart,
	}
	copy(lvl.Tiles, src.Tiles)
	lvl.GoombaSpawns = append([]Vec(nil), src.GoombaSpawns...)
	lvl.KoopaSpawns = append([]Vec(nil), src.KoopaSpawns...)
	lvl.CoinSpawns = append([]Vec(nil), src.CoinSpawns...)

	g.Level = lvl
	g.levelIndex = i
	g.Time = StartTime
	g.CameraX = 0
	g.bumps = map[int]int{}

	g.Enemies = nil
	for _, s := range lvl.GoombaSpawns {
		g.Enemies = append(g.Enemies, newGoomba(s))
	}
	for _, s := range lvl.KoopaSpawns {
		g.Enemies = append(g.Enemies, newKoopa(s))
	}
	g.CoinItems = nil
	for _, s := range lvl.CoinSpawns {
		g.CoinItems = append(g.CoinItems, &CoinItem{Pos: s})
	}
	g.Mushrooms = nil
	g.Particles = nil
	g.Player = newPlayer(lvl.PlayerStart, super)
	g.Paused = false
	g.stateTimer = 0
}

func (g *Game) cleanup() {
	g.Enemies = filter(g.Enemies, func(e *Enemy) bool { return !e.Gone })
	g.CoinItems = filter(g.CoinItems, func(c *CoinItem) bool { return !c.Gone })
	g.Mushrooms = filter(g.Mushrooms, func(m *Mushroom) bool { return !m.Gone })
}

func (g *Game) decayBumps() {
	for k, v := range g.bumps {
		if v <= 1 {
			delete(g.bumps, k)
		} else {
			g.bumps[k] = v - 1
		}
	}
}

// BumpActive reports whether a tile is currently playing its bump animation.
func (g *Game) BumpActive(tx, ty int) bool {
	return g.Level != nil && g.bumps[ty*g.Level.Width+tx] > 0
}

func (g *Game) addCoin() {
	g.CoinCount++
	g.Score += CoinScore
	if g.CoinCount >= ExtraLifeCoins {
		g.CoinCount -= ExtraLifeCoins
		g.Lives++
	}
}

func filter[T any](s []T, f func(T) bool) []T {
	out := s[:0]
	for _, v := range s {
		if f(v) {
			out = append(out, v)
		}
	}
	return out
}

// horizontalOverlap returns how much of the [tx, tx+1) tile column overlaps
// a body spanning [px, px+pw).
func horizontalOverlap(px, pw, tx float64) float64 {
	lo := px
	if tx > lo {
		lo = tx
	}
	hi := px + pw
	if tx+1 < hi {
		hi = tx + 1
	}
	if hi <= lo {
		return 0
	}
	return hi - lo
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-6 }
