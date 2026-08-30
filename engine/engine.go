// Package engine contains the pure, terminal-independent game logic for the
// CLI Mario game. It advances at a fixed 60 Hz logical tick rate and is fully
// deterministic for a given input sequence, which keeps it unit-testable.
package engine

import "math"

// Input is the player input for one logical tick. Quit, Pause, Restart,
// Suicide and AnyKey are edge triggered (set for exactly one tick per key
// press) and are produced by the input package.
type Input struct {
	Left, Right, Up, Down, Run            bool
	Quit, Pause, Restart, Suicide, AnyKey bool
}

// State is the high level game state.
type State int

const (
	StateTitle State = iota
	StateWorldCard
	StatePlaying
	StateFlagSlide
	StateWalkCastle
	StateScoreTick
	StateDying
	StateGameOver
	StateWin
)

func (s State) String() string {
	switch s {
	case StateTitle:
		return "title"
	case StateWorldCard:
		return "world-card"
	case StatePlaying:
		return "playing"
	case StateFlagSlide:
		return "flag-slide"
	case StateWalkCastle:
		return "walk-castle"
	case StateScoreTick:
		return "score-tick"
	case StateDying:
		return "dying"
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
	DyingTicks       = 180 // death freeze-frame + arc + beat
	DeathFreezeTicks = 30  // held still before the arc (the classic beat)
	WorldCardTicks   = 90  // "WORLD 1-2 x3" interstitial
	CastleDwellTicks = 45  // door-entry pause before the score countdown
	HurryTime        = 100 // HUD turns red below this
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
	Plants       []*Plant
	FireBars     []FireBar
	CoinItems    []*CoinItem
	Mushrooms    []*Mushroom
	FireFlowers  []*FireFlower
	Fireballs    []*Fireball
	Particles    []*Particle

	Score     int
	CoinCount int
	Lives     int
	Time      int
	State     State
	Paused    bool
	CameraX   float64
	Tick      int

	// Flow and feel state (all deterministic).
	Best       int      // local best score; hydrated by the host, kept live
	Daily      bool     // daily-challenge run (card title + leaderboard mode)
	Demo       bool     // attract mode: title art is drawn over live play
	Cheats     bool     // cheat mode: no fireball cap (host also refuses to record/submit)
	Hurry      bool     // time crossed HurryTime: HUD turns red
	HurryT     int      // "HURRY!" flash countdown
	FlagDrop   float64  // 0 pennant at the top of the pole, 1 at the base
	CastleFlag float64  // 0..1 castle victory-flag rise after door entry
	InCastle   bool     // the player has walked into the castle door
	Events     []string // sound events emitted this tick, consumed by the host

	levelIndex int
	stateTimer int
	checkpoint float64 // tile X of the mid-level respawn point; -1 unreached
	flagTopY   float64 // pole-slide start height (drives FlagDrop)
	bumps      map[int]int
	ceilBuf    []int // moveY rising-collision column scratch, reused per tick
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
	g.loadLevel(0, PowerSmall)
	g.State = StateTitle
	return g
}

// SetViewport changes the camera viewport mid-game (the terminal runner
// does this when its window is resized). It is a view change only — the
// viewport feeds the camera and the renderer, never the simulation, so a
// recording replays identically at any viewport. Non-positive dimensions
// keep their current values.
func (g *Game) SetViewport(viewW, viewH int) {
	for _, l := range g.Levels {
		if l.Width < viewW {
			viewW = l.Width
		}
	}
	if viewH > g.Levels[0].Height {
		viewH = g.Levels[0].Height
	}
	if viewW <= 0 {
		viewW = g.ViewW
	}
	if viewH <= 0 {
		viewH = g.ViewH
	}
	g.ViewW, g.ViewH = viewW, viewH
}

// LevelIndex returns the 0-based index of the current level.
func (g *Game) LevelIndex() int { return g.levelIndex }

// LevelName returns the display name of the current level (e.g. "1-1").
func (g *Game) LevelName() string { return g.Level.Name }

// CardName is what the world-card interstitial calls the current level.
func (g *Game) CardName() string {
	if g.Daily {
		return "DAILY " + g.Level.Name
	}
	return "WORLD " + g.Level.Name
}

// Reset starts a brand new game (used by the restart key).
func (g *Game) Reset() {
	g.Score = 0
	g.CoinCount = 0
	g.Lives = StartLives
	g.loadLevel(0, PowerSmall)
	g.State = StateWorldCard
	g.stateTimer = WorldCardTicks
}

// emit records a sound event for the host to play this tick. Immediate
// repeats of the same event collapse (coin showers, score-tick runs).
func (g *Game) emit(ev string) {
	if n := len(g.Events); n > 0 && g.Events[n-1] == ev {
		return
	}
	g.Events = append(g.Events, ev)
}

// Update advances the game by one logical tick with the given input.
func (g *Game) Update(in Input) {
	g.Tick++
	g.Events = g.Events[:0]
	if g.HurryT > 0 {
		g.HurryT--
	}
	switch g.State {
	case StateTitle:
		if in.AnyKey || in.Restart || in.Left || in.Right || in.Up || in.Down {
			g.startRun()
		}
	case StateWorldCard:
		g.stateTimer--
		if in.AnyKey {
			g.stateTimer = 0
		}
		if g.stateTimer <= 0 {
			g.State = StatePlaying
		}
	case StatePlaying:
		if in.Pause {
			g.Paused = !g.Paused
			g.emit("pause")
		}
		if g.Paused {
			if in.Restart { // pause-menu restart
				g.Reset()
			}
		} else {
			g.updatePlaying(in)
		}
	case StateFlagSlide:
		g.updateFlagSlide()
		g.updateParticles()
		g.decayBumps()
	case StateWalkCastle:
		g.updateWalkCastle()
		g.updateParticles()
		g.decayBumps()
	case StateScoreTick:
		g.updateScoreTick()
		g.updateParticles()
	case StateDying:
		g.stateTimer--
		if g.stateTimer > DyingTicks-DeathFreezeTicks {
			// freeze-frame beat: hold the pose before the death arc
		} else {
			g.updateDying()
		}
		if g.stateTimer <= 0 {
			if g.Lives > 0 {
				g.respawn()
			} else {
				g.State = StateGameOver
				g.emit("gameover")
			}
		}
	case StateGameOver, StateWin:
		if in.Restart {
			g.Reset()
		}
	}
	if g.Score > g.Best {
		g.Best = g.Score
	}
	g.prevIn = in
}

// startRun kicks off a fresh run from the title screen.
func (g *Game) startRun() {
	g.Score = 0
	g.CoinCount = 0
	g.Lives = StartLives
	g.loadLevel(0, PowerSmall)
	g.State = StateWorldCard
	g.stateTimer = WorldCardTicks
}

// BeginDaily resets into a daily-challenge run: the host has already
// swapped in the challenge level (Levels[0]); this arms the flag, resets
// the run and starts from the world card.
func (g *Game) BeginDaily() {
	g.Daily = true
	g.Score, g.CoinCount, g.Lives = 0, 0, StartLives
	g.loadLevel(0, PowerSmall)
	g.State = StateWorldCard
	g.stateTimer = WorldCardTicks
}

// BeginDemo starts the attract-mode demo from the title screen.
func (g *Game) BeginDemo() {
	if g.State != StateTitle {
		return
	}
	g.Demo = true
	g.loadLevel(0, PowerSmall)
	g.Lives = 99 // the demo loops forever; it never shows game over
	g.State = StatePlaying
}

// EndDemo returns to a fresh title screen.
func (g *Game) EndDemo() {
	g.Demo = false
	g.loadLevel(0, PowerSmall)
	g.Lives = StartLives
	g.State = StateTitle
}

// respawn reloads the level after a death, honoring the checkpoint.
func (g *Game) respawn() {
	cp := g.checkpoint
	g.loadLevel(g.levelIndex, PowerSmall)
	if cp >= 0 {
		g.checkpoint = cp
		g.Player.Pos.X = cp
	}
	g.clearSpawnThreats()
	g.State = StateWorldCard
	g.stateTimer = WorldCardTicks
}

// clearSpawnThreats removes enemies overlapping the player's footprint.
// A level reload puts every enemy back at its spawn point, so a spawn
// inside the checkpoint (or level-start) footprint killed the player on
// the first playing tick — 1-1's guard goomba sat exactly on the
// auto-computed checkpoint, draining every remaining life in a death
// loop (live bug, 2026-08-30). computeCheckpoint avoids threatened
// columns; this is the belt-and-braces invariant for hand-authored and
// generated levels whose checkpoints bypass that picker.
func (g *Game) clearSpawnThreats() {
	p := g.Player
	g.Enemies = filter(g.Enemies, func(e *Enemy) bool {
		return !(e.Pos.X < p.Pos.X+p.W && e.Pos.X+e.W > p.Pos.X &&
			e.Pos.Y < p.Pos.Y+p.H && e.Pos.Y+e.H > p.Pos.Y)
	})
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
		if !g.Hurry && g.Time <= HurryTime {
			g.Hurry = true
			g.HurryT = 120
			g.emit("hurry")
		}
	}

	// The suicide key ('k'): a trapped player forfeits the life on demand
	// instead of waiting out the clock. Rising edge, live play only.
	if in.Suicide && !g.prevIn.Suicide {
		g.kill()
		return
	}

	// Fireballs throw on the run-key rising edge (run and fire share the
	// key, exactly like SMB's B button). Cheat mode lifts the cap.
	if in.Run && !g.prevIn.Run && g.Player.Power == PowerFire &&
		(g.Cheats || g.aliveFireballs() < MaxFireballs) {
		g.throwFireball()
	}

	g.updatePlayer(in)
	if g.State != StatePlaying {
		return
	}

	if g.checkpoint < 0 && g.Player.Pos.X >= g.Level.CheckpointX {
		g.checkpoint = g.Level.CheckpointX
	}

	if g.Player.Pos.X+g.Player.W >= float64(g.Level.FlagX)+0.3 {
		g.grabFlag()
		return
	}

	g.updateEnemies()
	g.updatePlants()
	g.updateFireBars()
	g.playerEnemyInteractions()
	if g.State != StatePlaying {
		return
	}
	if g.touchingLava() {
		g.kill()
		return
	}

	g.updateMushrooms()
	g.updateFlowers()
	g.updateFireballs()
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

// grabFlag starts the flagpole slide: score by grab height, pennant down.
func (g *Game) grabFlag() {
	p := g.Player
	g.State = StateFlagSlide
	g.emit("flag")

	// Height bonus: the higher the grab, the bigger the pay, SMB style.
	// The scale is the pole itself — feet at the finial pay the top tier,
	// feet at the base the minimum — so every tier sits on the pole the
	// player can actually see and reach.
	feet := p.Pos.Y + p.H
	bonus := flagGrabBonus(feet, float64(g.Level.Height-2), float64(g.Level.poleTopRow()))
	g.Score += bonus
	g.spawnScorePop(p.Pos.X, p.Pos.Y, bonus, false)

	// Lock onto the pole (its visual centre) and hold the pose.
	p.Pos.X = float64(g.Level.FlagX) + 0.42 - p.W/2
	p.Vel = Vec{}
	p.Facing = 1
	g.flagTopY = p.Pos.Y
}

// flagGrabBonus returns the height-tier flagpole bonus. frac normalises the
// grab height onto the pole span — 1 with the feet at the finial (topRow),
// 0 at the ground beside the pole (groundRow) — and the tiers descend from
// there; grabbing above the finial still pays the top tier.
func flagGrabBonus(feet, groundRow, topRow float64) int {
	frac := (groundRow - feet) / (groundRow - topRow)
	switch {
	case frac > 0.8:
		return 5000
	case frac > 0.6:
		return 2000
	case frac > 0.4:
		return 800
	case frac > 0.2:
		return 400
	}
	return 100
}

func (g *Game) updateFlagSlide() {
	p := g.Player
	bottom := float64(g.Level.Height-2) - p.H
	if p.Pos.Y < bottom {
		p.Pos.Y = math.Min(p.Pos.Y+FlagSlideSpeed, bottom)
		if bottom > g.flagTopY {
			g.FlagDrop = (p.Pos.Y - g.flagTopY) / (bottom - g.flagTopY)
		}
		return
	}
	g.FlagDrop = 1
	// Feet on the ground: hop off towards the castle.
	g.State = StateWalkCastle
	p.Pos.X = float64(g.Level.FlagX) + 0.8
	p.Vel = Vec{X: CastleWalkSpeed, Y: -0.22}
	p.Facing = 1
}

func (g *Game) updateWalkCastle() {
	p := g.Player
	if g.InCastle {
		if g.CastleFlag < 1 {
			g.CastleFlag = math.Min(1, g.CastleFlag+1.0/60)
		}
		g.stateTimer--
		if g.stateTimer <= 0 {
			g.State = StateScoreTick
			g.emit("clear")
		}
		return
	}
	p.Vel.Y += Gravity
	if p.Vel.Y > MaxFall {
		p.Vel.Y = MaxFall
	}
	g.moveX(&p.Pos, p.W, p.H, CastleWalkSpeed)
	landed, _, _ := g.moveY(&p.Pos, p.W, p.H, p.Vel.Y)
	if landed {
		p.Vel.Y = 0
	}
	p.WalkDist += CastleWalkSpeed
	g.updateCamera()

	doorX := float64(g.Level.FlagX + 5) // castle door centre
	if p.Pos.X+p.W/2 >= doorX || p.Pos.X > float64(g.Level.FlagX+7) {
		g.InCastle = true
		g.stateTimer = CastleDwellTicks
	}
}

func (g *Game) updateScoreTick() {
	if g.CastleFlag < 1 {
		g.CastleFlag = math.Min(1, g.CastleFlag+1.0/60)
	}
	if g.Time > 0 {
		d := min(2, g.Time)
		g.Time -= d
		g.Score += d * TimeBonusPerUnit
		if g.Tick%2 == 0 {
			g.emit("tick")
		}
		return
	}
	if g.levelIndex+1 < len(g.Levels) {
		g.loadLevel(g.levelIndex+1, g.Player.Power)
		g.State = StateWorldCard
		g.stateTimer = WorldCardTicks
	} else {
		g.State = StateWin
		g.emit("win")
	}
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
	g.emit("die")
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

func (g *Game) loadLevel(i int, power PowerLevel) {
	src := g.Levels[i]
	lvl := &Level{
		Name:        src.Name,
		Width:       src.Width,
		Height:      src.Height,
		FlagX:       src.FlagX,
		Tiles:       make([]Tile, len(src.Tiles)),
		PlayerStart: src.PlayerStart,
		Theme:       src.Theme,
	}
	lvl.CheckpointX = src.CheckpointX
	copy(lvl.Tiles, src.Tiles)
	lvl.GoombaSpawns = append([]Vec(nil), src.GoombaSpawns...)
	lvl.KoopaSpawns = append([]Vec(nil), src.KoopaSpawns...)
	lvl.ParaSpawns = append([]Vec(nil), src.ParaSpawns...)
	lvl.CoinSpawns = append([]Vec(nil), src.CoinSpawns...)
	lvl.PlantSpawns = append([]Vec(nil), src.PlantSpawns...)
	lvl.BarSpawns = append([]FireBar(nil), src.BarSpawns...)

	g.Level = lvl
	g.levelIndex = i
	g.Time = StartTime
	g.CameraX = 0
	g.bumps = map[int]int{}
	g.checkpoint = -1
	g.Hurry = false
	g.HurryT = 0
	g.FlagDrop = 0
	g.CastleFlag = 0
	g.InCastle = false

	g.Enemies = nil
	for _, s := range lvl.GoombaSpawns {
		g.Enemies = append(g.Enemies, newGoomba(s))
	}
	for _, s := range lvl.KoopaSpawns {
		g.Enemies = append(g.Enemies, newKoopa(s))
	}
	for _, s := range lvl.ParaSpawns {
		g.Enemies = append(g.Enemies, newPara(s))
	}
	g.Plants = nil
	for _, s := range lvl.PlantSpawns {
		g.Plants = append(g.Plants, newPlant(s))
	}
	g.FireBars = append([]FireBar(nil), lvl.BarSpawns...)
	g.CoinItems = nil
	for _, s := range lvl.CoinSpawns {
		g.CoinItems = append(g.CoinItems, &CoinItem{Pos: s})
	}
	g.Mushrooms = nil
	g.FireFlowers = nil
	g.Fireballs = nil
	g.Particles = nil
	g.Player = newPlayer(lvl.PlayerStart, power)
	g.Paused = false
	g.stateTimer = 0
	g.clearSpawnThreats()
}

func (g *Game) cleanup() {
	g.Enemies = filter(g.Enemies, func(e *Enemy) bool { return !e.Gone })
	g.CoinItems = filter(g.CoinItems, func(c *CoinItem) bool { return !c.Gone })
	g.Mushrooms = filter(g.Mushrooms, func(m *Mushroom) bool { return !m.Gone })
	g.FireFlowers = filter(g.FireFlowers, func(f *FireFlower) bool { return !f.Gone })
	g.Fireballs = filter(g.Fireballs, func(f *Fireball) bool { return !f.Gone })
	g.Plants = filter(g.Plants, func(p *Plant) bool { return !p.Gone })
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
	g.emit("coin")
	if g.CoinCount >= ExtraLifeCoins {
		g.CoinCount -= ExtraLifeCoins
		g.Lives++
		g.emit("oneup")
	}
}

// oneUp awards an extra life with its event.
func (g *Game) oneUp() {
	g.Lives++
	g.emit("oneup")
}

// awardStomp pays the combo ladder for an airborne stomp chain.
func (g *Game) awardStomp(x, y float64) {
	if g.Player.stompChain >= len(stompLadder) {
		g.oneUp()
		g.spawnScorePop(x, y, 0, true)
	} else {
		v := stompLadder[g.Player.stompChain]
		g.Score += v
		g.spawnScorePop(x, y, v, false)
	}
	g.Player.stompChain++
}

// awardShell pays the combo ladder for consecutive kills by one sliding shell.
func (g *Game) awardShell(e *Enemy, chain int) {
	if chain >= len(stompLadder) {
		g.oneUp()
		g.spawnScorePop(e.Pos.X, e.Pos.Y, 0, true)
	} else {
		v := stompLadder[chain]
		g.Score += v
		g.spawnScorePop(e.Pos.X, e.Pos.Y, v, false)
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
	l := math.Max(px, tx)
	r := math.Min(px+pw, tx+1)
	if r <= l {
		return 0
	}
	return r - l
}

func approx(a, b float64) bool { return math.Abs(a-b) < 1e-6 }
