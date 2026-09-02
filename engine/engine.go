// Package engine contains the pure, terminal-independent game logic for the
// CLI Mario game. It advances at a fixed 60 Hz logical tick rate and is fully
// deterministic for a given input sequence, which keeps it unit-testable.
package engine

import "math"

// Input is the player input for one logical tick. Quit, Pause, Restart,
// Suicide and AnyKey are edge triggered (set for exactly one tick per key
// press) and are produced by the input package.
//
// Down is a reserved replay bit: the recording wire format (replay.go
// maskOf) freezes it as bit 3, so it must never be reclaimed or
// renumbered — that would rewrite every existing recording. In play it
// is the pipe-travel key: its rising edge while standing on an enterable
// pipe mouth sinks the player in (warp.go). There is still no crouch.
type Input struct {
	Left, Right, Up, Down, Run            bool
	Quit, Pause, Restart, Suicide, AnyKey bool
}

// State is the high level game state.
type State int

// The run lifecycle: Title → WorldCard → Playing, then on level clear
// FlagSlide → WalkCastle → ScoreTick → the next level's WorldCard (or
// Win after the last); a death detours through Dying → WorldCard
// respawn, or GameOver when the lives run out.
const (
	StateTitle State = iota
	StateWorldCard
	StatePlaying
	StatePipeIn
	StatePipeOut
	StateFlagSlide
	StateWalkCastle
	StateScoreTick
	StateDying
	StateGameOver
	StateWin
)

// String returns the state's kebab-case name, as used in logs and debug
// overlays.
func (s State) String() string {
	switch s {
	case StateTitle:
		return "title"
	case StateWorldCard:
		return "world-card"
	case StatePlaying:
		return "playing"
	case StatePipeIn:
		return "pipe-in"
	case StatePipeOut:
		return "pipe-out"
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

// Run pacing: tick rate, clock and lives, plus the fixed beat lengths of
// the non-playing interstitials.
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
	HurryFlashTicks  = 120 // "HURRY!" flash duration once time crosses HurryTime
	ExtraLifeCoins   = 100
	ScoreTickPace    = 2 // time units cashed into score per countdown beat; also the tick-sound period
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
	Best  int  // local best score; hydrated by the host, kept live
	Daily bool // daily-challenge run (card title + leaderboard mode)
	Demo  bool // attract mode: title art is drawn over live play
	// Cheat mode. Engine-side it lifts only the fireball cap; the rest
	// of the contract lives in the host: mario.go refuses to record a
	// cheat run (App.Step gates the Recorder on !Cheats), so the UI
	// refuses to submit it (UNRECORDED) and no leaderboard row can
	// leave the machine — the server's require_replay trigger would
	// reject such a row anyway.
	Cheats     bool
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
	prevIn     Input // previous tick's input, for rising/falling edges
	curIn      Input // current tick's input (stomp bounce reads held jump)

	// Pipe-warp state (see warp.go): the stash of the main-level world
	// while the player is in a room, the live world per room template,
	// the room currently occupied, and the warp in flight.
	savedWorld   *worldState
	roomWorlds   map[*Level]*worldState
	roomTemplate *Level
	inRoom       bool
	pending      *Warp
	pipeTop      int
}

// clampViewport narrows a viewport to the level set so the camera can
// never see past a level's edge: the width is clamped to the narrowest
// level, the height to the first level's rows. NewGame and SetViewport
// share it; SetViewport additionally substitutes the current values for
// non-positive dimensions.
func clampViewport(levels []*Level, viewW, viewH int) (int, int) {
	for _, l := range levels {
		if l.Width < viewW {
			viewW = l.Width
		}
	}
	if viewH > levels[0].Height {
		viewH = levels[0].Height
	}
	return viewW, viewH
}

// NewGame creates a game over the given levels. viewW/viewH is the camera
// viewport in tiles, clamped to the levels by clampViewport.
func NewGame(levels []*Level, viewW, viewH int) *Game {
	viewW, viewH = clampViewport(levels, viewW, viewH)
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
	viewW, viewH = clampViewport(g.Levels, viewW, viewH)
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
	g.newRun()
}

// newRun resets the counters and kicks off a fresh run from the world
// card; Reset, BeginDaily and the title-screen start all funnel through here.
func (g *Game) newRun() {
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
			g.newRun()
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
	case StatePipeIn:
		g.updatePipeIn()
	case StatePipeOut:
		g.updatePipeOut()
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

// BeginDaily resets into a daily-challenge run: the host has already
// swapped in the challenge level (Levels[0]); this arms the flag, resets
// the run and starts from the world card.
func (g *Game) BeginDaily() {
	g.Daily = true
	g.newRun()
}

// DemoLives is the attract-mode life pool: effectively endless, so the
// demo can loop forever without ever tripping into game over.
const DemoLives = 99

// BeginDemo starts the attract-mode demo from the title screen.
func (g *Game) BeginDemo() {
	if g.State != StateTitle {
		return
	}
	g.Demo = true
	g.loadLevel(0, PowerSmall)
	g.Lives = DemoLives
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

// updatePlaying runs one live-play tick. The ordering is load-bearing and
// must not be reshuffled: clock → suicide → fireballs → player physics →
// checkpoint → flag grab → enemies/plants/bars/combats → lava → pickups →
// particles → camera → pit → cleanup. The early returns are not optional
// style — a kill (time-out, suicide, lava, fall-out) or a flag grab
// invalidates every later step, which would otherwise read and mutate a
// world the run has already left (dead players must not collect coins;
// a flag grab ends combat mid-stride). Player physics itself can kill or
// clear the level, hence its immediate re-check before the world moves.
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
			g.HurryT = HurryFlashTicks
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

	// Pipe travel: Down on an enterable pipe mouth sinks the player in
	// (warp.go). Rising edge only, and it consumes the tick — the press
	// must not also move the player.
	if in.Down && !g.prevIn.Down && g.Player.Grounded {
		if w := g.warpUnder(g.Player); w != nil {
			g.beginPipeIn(w)
			return
		}
	}

	g.updatePlayer(in)
	if g.State != StatePlaying {
		return
	}

	// The checkpoint is a main-level concept: a room's computed fallback
	// must never be mistaken for reaching it.
	if !g.inRoom && g.checkpoint < 0 && g.Player.Pos.X >= g.Level.CheckpointX {
		g.checkpoint = g.Level.CheckpointX
	}

	// Flagless levels (warp rooms) never end by pole.
	if g.Level.FlagX >= 0 && g.Player.Pos.X+g.Player.W >= float64(g.Level.FlagX)+0.3 {
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
	// Height-2: the ground surface — every level stands on two ground rows.
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
	// GroundTop: the ground surface — every level stands on two ground rows.
	bottom := float64(GroundTop) - p.H
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
	p.Vel = Vec{X: CastleWalkSpeed, Y: CastleHopVel}
	p.Facing = 1
}

// Castle-door geometry, in tile columns relative to the flagpole: the
// walk-to-castle sequence ends when the player's centre reaches the door
// column, with a force-entry column as the overshoot fallback.
// render.castleRect (render/camera.go) derives its footprint FROM these
// constants (door centre = two tiles inside the castle's left edge; the
// castle's last column = CastleDoorPastX) — changing either const moves
// the drawn castle with the door, no manual sync.
const (
	CastleDoorOffset = 5 // door centre column, relative to FlagX
	CastleDoorPastX  = 7 // force door entry past this column, relative to FlagX
)

func (g *Game) updateWalkCastle() {
	p := g.Player
	if g.InCastle {
		if g.CastleFlag < 1 {
			g.CastleFlag = math.Min(1, g.CastleFlag+CastleFlagRise)
		}
		g.stateTimer--
		if g.stateTimer <= 0 {
			g.State = StateScoreTick
			g.emit("clear")
		}
		return
	}
	p.Vel.Y = applyGravity(p.Vel.Y, Gravity)
	g.moveX(&p.Pos, p.W, p.H, CastleWalkSpeed)
	landed, _, _ := g.moveY(&p.Pos, p.W, p.H, p.Vel.Y)
	if landed {
		p.Vel.Y = 0
	}
	p.WalkDist += CastleWalkSpeed
	g.updateCamera()

	doorX := float64(g.Level.FlagX + CastleDoorOffset) // castle door centre
	if p.Pos.X+p.W/2 >= doorX || p.Pos.X > float64(g.Level.FlagX+CastleDoorPastX) {
		g.InCastle = true
		g.stateTimer = CastleDwellTicks
	}
}

func (g *Game) updateScoreTick() {
	if g.CastleFlag < 1 {
		g.CastleFlag = math.Min(1, g.CastleFlag+CastleFlagRise)
	}
	if g.Time > 0 {
		d := min(ScoreTickPace, g.Time)
		g.Time -= d
		g.Score += d * TimeBonusPerUnit
		if g.Tick%ScoreTickPace == 0 {
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
	p.Vel.Y = applyGravity(p.Vel.Y, Gravity)
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
	g.Player.Vel.Y = DeathBounceVel
	g.emit("die")
}

// updateCamera tracks the player, which sits CameraAnchor of the viewport
// width from the camera's left edge, clamped to the level's span. A level
// narrower than the viewport (warp rooms, which sit outside the
// clampViewport minimum) centres instead: the room reads as a lit cellar
// in the dark, not a strip pinned to one edge.
const CameraAnchor = 0.35

func (g *Game) updateCamera() {
	if half := float64(g.Level.Width-g.ViewW) / 2; half < 0 {
		g.CameraX = half
		return
	}
	target := g.Player.Pos.X - float64(g.ViewW)*CameraAnchor
	if target < 0 {
		target = 0
	}
	if max := float64(g.Level.Width - g.ViewW); target > max {
		target = max
	}
	g.CameraX = target
}

func (g *Game) loadLevel(i int, power PowerLevel) {
	lvl := instantiate(g.Levels[i])

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

	// A level load starts the visit over: any warp room state from the
	// previous visit goes with it.
	g.savedWorld = nil
	g.roomWorlds = nil
	g.roomTemplate = nil
	g.inRoom = false
	g.pending = nil

	g.spawnEntities(lvl)
	g.Player = newPlayer(lvl.PlayerStart, power)
	g.Paused = false
	g.stateTimer = 0
	g.clearSpawnThreats()
}

// instantiate builds the live, mutable copy of a level template: a fresh
// tile grid (used blocks, broken bricks never corrupt the source) and
// copied spawn lists. loadLevel and warp rooms share it.
func instantiate(src *Level) *Level {
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
	lvl.Warps = append([]Warp(nil), src.Warps...)
	return lvl
}

// spawnEntities builds the live entity sets from a level's spawn lists.
func (g *Game) spawnEntities(lvl *Level) {
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

// awardLadder pays one rung of the shared combo ladder — the airborne
// stomp chain and a sliding shell's kill chain climb the same ladder —
// at a world position: past the last rung, every further kill in the
// chain pays a 1-UP instead. The caller owns the chain counter
// (Player.stompChain advances here on return; a shell's Chain is the
// shell's own bookkeeping).
func (g *Game) awardLadder(chain int, x, y float64) {
	if chain >= len(stompLadder) {
		g.oneUp()
		g.spawnScorePop(x, y, 0, true)
	} else {
		v := stompLadder[chain]
		g.Score += v
		g.spawnScorePop(x, y, v, false)
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
