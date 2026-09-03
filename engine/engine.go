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
// renumbered — that would rewrite every existing recordings. In play it
// is the pipe-travel key: its rising edge while standing on an enterable
// pipe mouth sinks the player in (warp.go). There is still no crouch.
type Input struct {
	Left, Right, Up, Down, Run            bool
	Quit, Pause, Restart, Suicide, AnyKey bool
}

// State is the high level game state.
type State int

// The run lifecycle: Title → WorldCard → Playing, then on level clear
// FlagSlide → WalkCastle → (a castle with a retainer: Retainer, the
// toad/princess "thank you" beat) → ScoreTick → the next level's
// WorldCard (or Win after the last); the boss arena ends Playing
// through BridgeFall (axe grab → bridge collapse → boss in the lava)
// into that same castle walk; a death detours through Dying → WorldCard
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
	StateBridgeFall
	StateRetainer
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
	case StateBridgeFall:
		return "bridge-fall"
	case StateRetainer:
		return "retainer"
	}
	return "unknown"
}

// Run pacing: tick rate, clock and lives, plus the fixed beat lengths of
// the non-playing interstitials.
const (
	TicksPerSecond    = 60
	TicksPerTimeUnit  = 24 // one game-time unit ticks down every 24 frames
	StartTime         = 300
	StartLives        = 3
	DyingTicks        = 180 // death freeze-frame + arc + beat
	DeathFreezeTicks  = 30  // held still before the arc (the classic beat)
	WorldCardTicks    = 90  // "WORLD 1-2 x3" interstitial
	CastleDwellTicks  = 45  // door-entry pause before the score countdown
	RetainerTicks     = 150 // toad/princess cutscene hold before the score countdown
	HurryTime         = 100 // HUD turns red below this
	HurryFlashTicks   = 120 // "HURRY!" flash duration once time crosses HurryTime
	ExtraLifeCoins    = 100
	ScoreTickPace     = 2   // time units cashed into score per countdown beat; also the tick-sound period
	FireworksScore    = 500 // per burst when the cleared timer's last digit is 1, 3 or 6
	RespawnGraceTicks = 90  // no leaping-cheep spawns right after a respawn (one card's worth)
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
	Bowsers      []*Bowser
	BossFires    []*BossFire
	Particles    []*Particle

	// SMB1-fidelity entity sets (contract S4): the castle and water
	// hazards plus the rideable platforms. All are rebuilt by
	// loadLevel/spawnEntities from the level's spawn lists.
	Bloopers   []*Bloober
	Cheeps     []*Cheep
	Podoboos   []*Podoboo
	HammerBros []*HammerBro
	Hammers    []*Hammer
	Lifts      []*Lift
	Springs    []*Spring

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
	Hurry      bool    // time crossed HurryTime: HUD turns red
	HurryT     int     // "HURRY!" flash countdown
	FlagDrop   float64 // 0 pennant at the top of the pole, 1 at the base
	CastleFlag float64 // 0..1 castle victory-flag rise after door entry
	InCastle   bool    // the player has walked into the castle door

	RetainerShown bool     // this level's toad/princess cutscene has played
	respawnGrace  int      // ticks of leaper-spawn silence after a respawn
	Events        []string // sound events emitted this tick, consumed by the host

	levelIndex int
	stateTimer int
	checkpoint float64 // tile X of the mid-level respawn point; -1 unreached
	flagTopY   float64 // pole-slide start height (drives FlagDrop)
	fireworks  bool    // flag-slide fireworks already awarded this slide
	bumps      map[int]int
	coinBricks map[int]*coinBrick // live multi-coin bricks, keyed by tile index
	vine       *Vine              // the live beanstalk (nil until one sprouts)
	bridgeCols []int              // bridge columns left to sweep in StateBridgeFall
	ceilBuf    []int              // moveY rising-collision column scratch, reused per tick
	prevIn     Input              // previous tick's input, for rising/falling edges
	curIn      Input              // current tick's input (stomp bounce reads held jump)

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
	case StateBridgeFall:
		g.updateBridgeFall()
		g.updateParticles()
		g.decayBumps()
	case StateRetainer:
		g.updateRetainer(in.AnyKey)
		g.updateParticles()
		g.decayBumps()
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
	// Dying inside a warp room: the checkpoint that counts is the main
	// level's — the room's own is an artifact of its fallback column.
	cp := g.checkpoint
	if g.inRoom && g.savedWorld != nil {
		cp = g.savedWorld.checkpoint
	}
	g.loadLevel(g.levelIndex, PowerSmall)
	if cp >= 0 {
		g.checkpoint = cp
		g.Player.Pos.X = cp
	}
	g.clearSpawnThreats()
	g.respawnGrace = RespawnGraceTicks
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
	// The boss is a threat too: a respawn must never materialize inside
	// his patrol box.
	g.Bowsers = filter(g.Bowsers, func(b *Bowser) bool {
		return !overlap(b.Pos.X, b.Pos.Y, b.W, b.H, p.Pos.X, p.Pos.Y, p.W, p.H)
	})
	// The SMB1-fidelity hazards: discrete entities (bloobers, hammer
	// bros, live hammers, stray cheeps) get the goomba treatment. A
	// podoboo is periodic, not discrete — removing it would silently
	// edit the level — but the picker's arc-band guard keeps checkpoint
	// columns out of its lane, so overlap here means a hand-authored
	// start marker inside a pool: drop it rather than loop the player.
	g.Bloopers = filter(g.Bloopers, func(b *Bloober) bool {
		return !overlap(b.Pos.X, b.Pos.Y, b.W, b.H, p.Pos.X, p.Pos.Y, p.W, p.H)
	})
	g.HammerBros = filter(g.HammerBros, func(b *HammerBro) bool {
		return !overlap(b.Pos.X, b.Pos.Y, b.W, b.H, p.Pos.X, p.Pos.Y, p.W, p.H)
	})
	g.Hammers = filter(g.Hammers, func(h *Hammer) bool {
		return !overlap(h.Pos.X, h.Pos.Y, HammerW, HammerH, p.Pos.X, p.Pos.Y, p.W, p.H)
	})
	g.Cheeps = filter(g.Cheeps, func(c *Cheep) bool {
		return !overlap(c.Pos.X, c.Pos.Y, c.W, c.H, p.Pos.X, p.Pos.Y, p.W, p.H)
	})
	g.Podoboos = filter(g.Podoboos, func(o *Podoboo) bool {
		return !overlap(o.Pos.X, o.Pos.Y, o.W, o.H, p.Pos.X, p.Pos.Y, p.W, p.H)
	})
}

// updatePlaying runs one live-play tick. The ordering is load-bearing and
// must not be reshuffled: clock → suicide → fireballs → player physics →
// checkpoint → flag/axe grab → enemies/plants/bars → bowsers/bossfires →
// lifts/springs/podoboos/cheeps/bloopers/hammer bros/hammers → combats →
// lava → pickups → particles → camera → pit → cleanup. The early returns
// are not optional style — a kill (time-out, suicide, lava, fall-out) or
// a goal grab (flag, axe) invalidates every later step, which would
// otherwise read and mutate a world the run has already left (dead
// players must not collect coins; a goal grab ends combat mid-stride).
// Player physics itself can kill or clear the level, hence its immediate
// re-check before the world moves.
func (g *Game) updatePlaying(in Input) {
	g.curIn = in
	if g.respawnGrace > 0 {
		g.respawnGrace--
	}

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

	// The beanstalk: a rising Up press grabs a live stalk, SMB
	// ladder-style, and consumes the tick — the press must not also
	// jump the player.
	if g.tryGrabVine(in) {
		return
	}

	g.updatePlayer(in)
	if g.State != StatePlaying {
		return
	}

	// The stalk grows with the world; a fall out of a drop-exit room
	// (the Coin Heaven's open right edge) returns to the main level.
	g.updateVine()
	if g.inRoom && g.Level.DropExitX > 0 && g.Player.Pos.Y > float64(g.Level.Height) {
		g.exitRoomFall()
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

	// The axe is the boss arena's goal: touching it collapses the bridge.
	if g.Level.AxeX >= 0 && overlap(g.Player.Pos.X, g.Player.Pos.Y, g.Player.W, g.Player.H,
		float64(g.Level.AxeX), float64(g.Level.AxeY), 1, 1) {
		g.grabAxe()
		return
	}
	g.updateEnemies()
	g.updatePlants()
	g.updateFireBars()
	g.updateBowsers()
	g.updateBossFires()
	g.updateLifts()
	g.updateSprings()
	g.updatePodoboos()
	g.updateCheeps()
	g.updateBloopers()
	g.updateHammerBros()
	g.updateHammers()
	g.playerEnemyInteractions()
	g.playerBowserInteractions()
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
	// Feet on the ground: the classic timer-digit fireworks, then hop off
	// towards the castle.
	g.awardFireworks()
	g.State = StateWalkCastle
	p.Pos.X = float64(g.Level.FlagX) + 0.8
	p.Vel = Vec{X: CastleWalkSpeed, Y: CastleHopVel}
	p.Facing = 1
}

// awardFireworks pays the flagpole fireworks (contract S12): when the
// cleared timer's last digit is 1, 3 or 6, three sparkle bursts go off
// above the castle at FireworksScore each — once per slide, a pure
// function of the timer state.
func (g *Game) awardFireworks() {
	if g.fireworks {
		return
	}
	switch g.Time % 10 {
	case 1, 3, 6:
	default:
		return
	}
	g.fireworks = true
	// The bursts sit above the castle roof (the castle spans the goal's
	// right-hand columns on rows 9..12 — render.castleRect).
	base := float64(g.Level.GoalX())
	for i, by := range [...]float64{6, 4.8, 6.3} {
		bx := base + 3.5 + float64(i)*1.5
		for _, o := range [...]Vec{{0, -0.3}, {0.3, 0}, {-0.3, 0}, {0.2, -0.18}, {-0.2, -0.18}, {0, 0.12}} {
			g.spawnSparkle(bx+o.X, by+o.Y)
		}
		g.Score += FireworksScore
		g.spawnScorePop(bx, by-0.6, FireworksScore, false)
	}
}

// Castle-door geometry, in tile columns relative to the level's goal
// (flagpole, or the axe in the boss arena): the walk-to-castle sequence
// ends when the player's centre reaches the door column, with a
// force-entry column as the overshoot fallback.
// render.castleRect (render/camera.go) derives its footprint FROM these
// constants (door centre = two tiles inside the castle's left edge; the
// castle's last column = CastleDoorPastX) — changing either const moves
// the drawn castle with the door, no manual sync.
const (
	CastleDoorOffset = 5 // door centre column, relative to GoalX()
	CastleDoorPastX  = 7 // force door entry past this column, relative to GoalX()
)

func (g *Game) updateWalkCastle() {
	p := g.Player
	if g.InCastle {
		if g.CastleFlag < 1 {
			g.CastleFlag = math.Min(1, g.CastleFlag+CastleFlagRise)
		}
		g.stateTimer--
		if g.stateTimer <= 0 {
			if g.Level.Retainer != 0 && !g.RetainerShown {
				// The castle holds a retainer: the toad (or princess)
				// "thank you" beat plays before the score countdown,
				// once per visit of this level.
				g.State = StateRetainer
				g.stateTimer = RetainerTicks
				g.RetainerShown = true
			} else {
				g.State = StateScoreTick
				g.emit("clear")
			}
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

	doorX := float64(g.Level.GoalX() + CastleDoorOffset) // castle door centre
	if p.Pos.X+p.W/2 >= doorX || p.Pos.X > float64(g.Level.GoalX()+CastleDoorPastX) {
		g.InCastle = true
		g.stateTimer = CastleDwellTicks
	}
}

// updateRetainer plays the castle cutscene (contract S5): the player
// auto-walks to a halt beside the retainer (toad or princess, at
// RetainerAt.X-1.5) and the beat holds until he arrives, the timer runs
// out or any key is pressed — then the score countdown takes over, as
// after any other castle. The world card the countdown leads to is
// entered from StateScoreTick exactly as before, so run recording and
// replay arming are untouched.
func (g *Game) updateRetainer(anyKey bool) {
	p := g.Player
	stop := g.Level.RetainerAt.X - 1.5
	if p.Pos.X < stop {
		p.Vel.Y = applyGravity(p.Vel.Y, Gravity)
		g.moveX(&p.Pos, p.W, p.H, CastleWalkSpeed)
		landed, _, _ := g.moveY(&p.Pos, p.W, p.H, p.Vel.Y)
		if landed {
			p.Vel.Y = 0
		}
		p.WalkDist += CastleWalkSpeed
		g.updateCamera()
	}
	g.stateTimer--
	if p.Pos.X >= stop || g.stateTimer <= 0 || anyKey {
		g.State = StateScoreTick
		g.emit("clear")
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
	g.Player.Climbing = false
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
	// The timer comes from the level itself when it declares one (SMB1:
	// 400 for ground/underground/underwater, 300 for athletic and
	// castle); otherwise the classic default.
	g.Time = lvl.Time
	if g.Time <= 0 {
		g.Time = StartTime
	}
	g.CameraX = 0
	g.bumps = map[int]int{}
	g.coinBricks = seedCoinBricks(lvl)
	g.vine = nil
	g.checkpoint = -1
	g.Hurry = false
	g.HurryT = 0
	g.FlagDrop = 0
	g.CastleFlag = 0
	g.InCastle = false
	g.RetainerShown = false
	g.fireworks = false

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
		AxeX:        src.AxeX,
		AxeY:        src.AxeY,
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
	lvl.BowserSpawns = append([]Vec(nil), src.BowserSpawns...)
	lvl.Warps = append([]Warp(nil), src.Warps...)
	// SMB1-fidelity fields (contract S1): flags, hazard spawners, the
	// retainer and the boss's disguise all travel with the copy.
	lvl.Time = src.Time
	lvl.Night = src.Night
	lvl.Underwater = src.Underwater
	lvl.CheepLeaping = src.CheepLeaping
	lvl.CheepStopX = src.CheepStopX
	lvl.Currents = append([]CurrentZone(nil), src.Currents...)
	lvl.PodobooSpawns = append([]Vec(nil), src.PodobooSpawns...)
	lvl.BlooberSpawns = append([]Vec(nil), src.BlooberSpawns...)
	lvl.HammerSpawns = append([]Vec(nil), src.HammerSpawns...)
	lvl.BuzzySpawns = append([]Vec(nil), src.BuzzySpawns...)
	lvl.KoopaRedSpawns = append([]Vec(nil), src.KoopaRedSpawns...)
	lvl.ParaRedSpawns = append([]Vec(nil), src.ParaRedSpawns...)
	lvl.Retainer = src.Retainer
	lvl.RetainerAt = src.RetainerAt
	lvl.BowserDisguise = src.BowserDisguise
	lvl.LiftSpawns = append([]LiftSpawn(nil), src.LiftSpawns...)
	lvl.SpringSpawns = append([]Vec(nil), src.SpringSpawns...)
	// The beanstalk's room pointer and the room's drop-exit column are
	// template wiring, not tile data — they must travel with the copy
	// or the live level sprouts a bare stalk (VineRoom nil).
	lvl.VineRoom = src.VineRoom
	lvl.DropExitX = src.DropExitX
	return lvl
}

// spawnEntities builds the live entity sets from a level's spawn lists.
func (g *Game) spawnEntities(lvl *Level) {
	g.Enemies = buildEnemies(lvl)
	g.Plants = nil
	for _, s := range lvl.PlantSpawns {
		g.Plants = append(g.Plants, newPlant(s))
	}
	g.FireBars = append([]FireBar(nil), lvl.BarSpawns...)
	g.CoinItems = nil
	for _, s := range lvl.CoinSpawns {
		g.CoinItems = append(g.CoinItems, &CoinItem{Pos: s})
	}
	g.Bowsers = buildBowsers(lvl)
	g.BossFires = nil
	g.bridgeCols = nil
	g.Mushrooms = nil
	g.FireFlowers = nil
	g.Fireballs = nil
	g.Particles = nil

	// SMB1-fidelity sets: the castle and water hazards, the
	// spawner-driven cheeps (empty until their spawners fire) and the
	// rideable platforms (lifts.go, spring.go).
	g.Podoboos = buildPodoboos(lvl)
	g.Bloopers = buildBloopers(lvl)
	g.HammerBros = buildHammerBros(lvl)
	g.Cheeps = nil
	g.Hammers = nil
	g.Lifts = buildLifts(lvl)
	g.Springs = buildSprings(lvl)
}

// buildEnemies turns a level's enemy spawn lists into the live set, in
// authoring order (goombas, koopas, paratroopas, then the SMB1-fidelity
// walkers). spawnEntities and the warp-room builder share it so a room
// fields exactly the same menagerie.
func buildEnemies(lvl *Level) []*Enemy {
	var es []*Enemy
	for _, s := range lvl.GoombaSpawns {
		es = append(es, newGoomba(s))
	}
	for _, s := range lvl.KoopaSpawns {
		es = append(es, newKoopa(s))
	}
	for _, s := range lvl.ParaSpawns {
		es = append(es, newPara(s))
	}
	for _, s := range lvl.BuzzySpawns {
		es = append(es, newBuzzy(s))
	}
	for _, s := range lvl.KoopaRedSpawns {
		es = append(es, newKoopaRed(s))
	}
	for _, s := range lvl.ParaRedSpawns {
		es = append(es, newParaRed(s))
	}
	return es
}

// buildBowsers fields the boss spawns with the level's disguise wired in
// (contract S7: every castle boss in worlds 1-3 is an impostor whose true
// form Level.BowserDisguise names).
func buildBowsers(lvl *Level) []*Bowser {
	var bs []*Bowser
	for _, s := range lvl.BowserSpawns {
		b := newBowser(s)
		b.Disguise = lvl.BowserDisguise
		bs = append(bs, b)
	}
	return bs
}

// buildPodoboos, buildBloopers and buildHammerBros field the castle and
// water hazards from a level's spawn lists; spawnEntities and the
// warp-room builder share them.
func buildPodoboos(lvl *Level) []*Podoboo {
	var ps []*Podoboo
	for _, s := range lvl.PodobooSpawns {
		ps = append(ps, newPodoboo(s.X, int(s.Y)))
	}
	return ps
}

func buildBloopers(lvl *Level) []*Bloober {
	var bs []*Bloober
	for _, s := range lvl.BlooberSpawns {
		bs = append(bs, newBloober(s))
	}
	return bs
}

func buildHammerBros(lvl *Level) []*HammerBro {
	var hs []*HammerBro
	for _, s := range lvl.HammerSpawns {
		hs = append(hs, newHammerBro(s))
	}
	return hs
}

// buildSprings fields the springboards from a level's spawn list.
func buildSprings(lvl *Level) []*Spring {
	var ss []*Spring
	for _, s := range lvl.SpringSpawns {
		ss = append(ss, &Spring{X: s.X, Y: s.Y})
	}
	return ss
}

func (g *Game) cleanup() {
	g.Enemies = filter(g.Enemies, func(e *Enemy) bool { return !e.Gone })
	g.CoinItems = filter(g.CoinItems, func(c *CoinItem) bool { return !c.Gone })
	g.Mushrooms = filter(g.Mushrooms, func(m *Mushroom) bool { return !m.Gone })
	g.FireFlowers = filter(g.FireFlowers, func(f *FireFlower) bool { return !f.Gone })
	g.Fireballs = filter(g.Fireballs, func(f *Fireball) bool { return !f.Gone })
	g.Plants = filter(g.Plants, func(p *Plant) bool { return !p.Gone })
	g.Bowsers = filter(g.Bowsers, func(b *Bowser) bool { return !b.Gone })
	g.BossFires = filter(g.BossFires, func(f *BossFire) bool { return !f.Gone })
	g.Podoboos = filter(g.Podoboos, func(p *Podoboo) bool { return !p.Gone })
	g.Cheeps = filter(g.Cheeps, func(c *Cheep) bool { return !c.Gone })
	g.Bloopers = filter(g.Bloopers, func(b *Bloober) bool { return !b.Gone })
	g.HammerBros = filter(g.HammerBros, func(h *HammerBro) bool { return !h.Gone })
	g.Hammers = filter(g.Hammers, func(h *Hammer) bool { return !h.Gone })
	g.Lifts = filter(g.Lifts, func(l *Lift) bool { return !l.Gone })
}

func (g *Game) decayBumps() {
	for k, v := range g.bumps {
		if v <= 1 {
			delete(g.bumps, k)
		} else {
			g.bumps[k] = v - 1
		}
	}
	// Multi-coin windows close here too: the brick spends to Used when
	// the first bump's clock runs out, coins or not.
	for k, cb := range g.coinBricks {
		if cb.timer == 0 {
			continue
		}
		cb.timer--
		if cb.timer <= 0 {
			g.Level.Set(k%g.Level.Width, k/g.Level.Width, Used)
			delete(g.coinBricks, k)
		}
	}
}

// coinBrick is the live state of one multi-coin brick.
type coinBrick struct {
	coins int
	timer int // 0 = window not open yet; the first bump opens it
}

// seedCoinBricks arms every multi-coin brick of a freshly loaded level.
func seedCoinBricks(l *Level) map[int]*coinBrick {
	var m map[int]*coinBrick
	for i, t := range l.Tiles {
		if t != BrickCoin {
			continue
		}
		if m == nil {
			m = map[int]*coinBrick{}
		}
		m[i] = &coinBrick{coins: MultiCoinCount}
	}
	return m
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
