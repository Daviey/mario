package engine

import "math"

// Pipe travel, SMB bonus-room style: standing on an enterable pipe mouth
// and pressing Down sinks the player into the pipe, swaps the live world
// to the warp's destination and rises back out of the destination pipe.
// Both beats are input-free animations (StatePipeIn / StatePipeOut), so
// the game clock holds while they play — an animation the player cannot
// abort must never be able to time him out.
//
// A warp with JumpTo > 0 (the warp zones, contract S11) skips the room
// machinery entirely: the run jumps ahead to that level and the player
// rises out of the ground at its player start.

// Warp pacing: the sink and the rise each cover two tiles (a small player
// plus the pipe lip; a super player exactly fits) at a fixed speed.
const (
	PipeWarpTicks = 36         // ~0.6s per beat at 60 Hz
	PipeSinkSpeed = 1.0 / 18.0 // tiles per tick: 36 ticks × 1/18 = 2 tiles
)

// worldState is the live, mutable half of one side of a warp: everything
// that must be set aside while the player visits the other side and put
// back exactly on return. The player himself is shared — power, score
// and coin count travel through the pipe with him.
type worldState struct {
	level      *Level
	enemies    []*Enemy
	plants     []*Plant
	bars       []FireBar
	coins      []*CoinItem
	mushrooms  []*Mushroom
	flowers    []*FireFlower
	fireballs  []*Fireball
	bowsers    []*Bowser
	bossFires  []*BossFire
	particles  []*Particle
	bumps      map[int]int
	checkpoint float64

	// SMB1-fidelity sets (contract S4): the castle and water hazards and
	// the rideable platforms swap worlds with everything else, so a room
	// visit neither freezes nor resets the main level's podoboos.
	lifts      []*Lift
	springs    []*Spring
	podoboos   []*Podoboo
	cheeps     []*Cheep
	bloopers   []*Bloober
	hammerBros []*HammerBro
	hammers    []*Hammer
}

// stashWorld captures the current live world.
func (g *Game) stashWorld() *worldState {
	return &worldState{
		level:      g.Level,
		enemies:    g.Enemies,
		plants:     g.Plants,
		bars:       g.FireBars,
		coins:      g.CoinItems,
		mushrooms:  g.Mushrooms,
		flowers:    g.FireFlowers,
		fireballs:  g.Fireballs,
		bowsers:    g.Bowsers,
		bossFires:  g.BossFires,
		particles:  g.Particles,
		bumps:      g.bumps,
		checkpoint: g.checkpoint,

		lifts:      g.Lifts,
		springs:    g.Springs,
		podoboos:   g.Podoboos,
		cheeps:     g.Cheeps,
		bloopers:   g.Bloopers,
		hammerBros: g.HammerBros,
		hammers:    g.Hammers,
	}
}

// applyWorld makes a stashed world live again.
func (g *Game) applyWorld(w *worldState) {
	g.Level = w.level
	g.Enemies = w.enemies
	g.Plants = w.plants
	g.FireBars = w.bars
	g.CoinItems = w.coins
	g.Mushrooms = w.mushrooms
	g.FireFlowers = w.flowers
	g.Fireballs = w.fireballs
	g.Bowsers = w.bowsers
	g.BossFires = w.bossFires
	g.Particles = w.particles
	g.bumps = w.bumps
	g.checkpoint = w.checkpoint

	g.Lifts = w.lifts
	g.Springs = w.springs
	g.Podoboos = w.podoboos
	g.Cheeps = w.cheeps
	g.Bloopers = w.bloopers
	g.HammerBros = w.hammerBros
	g.Hammers = w.hammers
}

// warpUnder returns the warp whose pipe mouth the player stands on, if
// any. Feet must rest exactly on the mouth row (standing ON the pipe,
// not walking past it) and the player's centre must be over the
// two-tile mouth.
func (g *Game) warpUnder(p *Player) *Warp {
	feet := int(math.Round(p.Pos.Y + p.H))
	cx := p.Pos.X + p.W/2
	for i := range g.Level.Warps {
		w := &g.Level.Warps[i]
		if feet == w.Top && cx >= float64(w.X)+0.2 && cx <= float64(w.X+2)-0.2 {
			return w
		}
	}
	return nil
}

// beginPipeIn starts the sink into the warp's pipe.
func (g *Game) beginPipeIn(w *Warp) {
	p := g.Player
	p.Pos.X = float64(w.X+1) - p.W/2 // centred on the mouth
	p.Vel = Vec{}
	p.Grounded = false
	g.State = StatePipeIn
	g.stateTimer = PipeWarpTicks
	g.pending = w
	g.emit("pipe")
}

// updatePipeIn sinks the player; on the last tick it swaps the world to
// the warp's destination and starts the rise.
func (g *Game) updatePipeIn() {
	g.Player.Pos.Y += PipeSinkSpeed
	g.stateTimer--
	if g.stateTimer > 0 {
		return
	}
	w := g.pending
	g.pending = nil
	g.performWarp(w)
}

// performWarp swaps the live world to the warp's destination and starts
// rising out of the destination pipe. Entering a room stashes the main
// world; leaving it restores that stash and re-stashes the room so a
// re-entry within the same level visit finds it exactly as it was left
// (collected coins stay collected — the original behaves the same).
//
// A warp-zone warp (JumpTo > 0, contract S11) instead skips the run
// ahead to that level index — the 1-2 warp room's World 2 / World 3
// pipes — preserving the player's power, and rises him out of the ground
// at the target level's player start.
func (g *Game) performWarp(w *Warp) {
	if w.JumpTo > 0 {
		idx := w.JumpTo
		if idx > len(g.Levels)-1 {
			idx = len(g.Levels) - 1 // authoring guard: clamp, never panic
		}
		power := g.Player.Power
		g.loadLevel(idx, power)
		g.beginPipeOutAt(g.Level.PlayerStart)
		return
	}
	if w.Dest == nil {
		g.roomWorlds[g.roomTemplate] = g.stashWorld()
		g.applyWorld(g.savedWorld)
		g.savedWorld = nil
		g.roomTemplate = nil
		g.inRoom = false
	} else {
		if !g.inRoom {
			g.savedWorld = g.stashWorld()
		} else {
			g.roomWorlds[g.roomTemplate] = g.stashWorld()
		}
		g.roomTemplate = w.Dest
		g.applyWorld(g.roomFor(w.Dest))
		g.inRoom = true
	}
	g.beginPipeOut(w.DestX, w.DestTop)
}

// roomFor returns the live world of a room template, building it on
// first entry this run.
func (g *Game) roomFor(t *Level) *worldState {
	if w, ok := g.roomWorlds[t]; ok {
		return w
	}
	lvl := instantiate(t)
	w := &worldState{
		level:      lvl,
		bumps:      map[int]int{},
		checkpoint: -1,
	}
	w.enemies = buildEnemies(lvl)
	w.plants = nil
	for _, s := range lvl.PlantSpawns {
		w.plants = append(w.plants, newPlant(s))
	}
	w.bars = append([]FireBar(nil), lvl.BarSpawns...)
	for _, s := range lvl.CoinSpawns {
		w.coins = append(w.coins, &CoinItem{Pos: s})
	}
	w.bowsers = buildBowsers(lvl)
	w.bossFires = nil
	// SMB1-fidelity sets, so a room fields the same menagerie the main
	// level would (and the spawner-driven ones start empty).
	w.podoboos = buildPodoboos(lvl)
	w.bloopers = buildBloopers(lvl)
	w.hammerBros = buildHammerBros(lvl)
	w.cheeps = nil
	w.hammers = nil
	w.lifts = buildLifts(lvl)
	w.springs = buildSprings(lvl)
	if g.roomWorlds == nil {
		g.roomWorlds = map[*Level]*worldState{}
	}
	g.roomWorlds[t] = w
	return w
}

// beginPipeOut starts the rise out of the pipe at (x, top): the player
// begins fully inside the pipe and climbs to standing on the mouth.
func (g *Game) beginPipeOut(x, top int) {
	p := g.Player
	p.Pos.X = float64(x+1) - p.W/2
	p.Pos.Y = float64(top+2) - p.H // feet two tiles down: inside the pipe
	p.Vel = Vec{}
	p.Grounded = false
	g.State = StatePipeOut
	g.stateTimer = PipeWarpTicks
	g.pipeTop = top
	g.emit("pipe")
	g.updateCamera()
}

// beginPipeOutAt starts the rise to standing at an arbitrary spawn point
// (the level-skip warp rises at the target level's player start): the
// player begins two tiles under the surface — inside the ground, pipe
// style — and climbs out to stand exactly where a fresh load would put
// him, honouring his current size.
func (g *Game) beginPipeOutAt(start Vec) {
	p := g.Player
	standY := start.Y - (p.H - SmallH) // newPlayer's size adjustment
	p.Pos = Vec{start.X, standY + 2}
	p.Vel = Vec{}
	p.Grounded = false
	g.State = StatePipeOut
	g.stateTimer = PipeWarpTicks
	g.pipeTop = int(math.Round(standY + p.H)) // the standing row for the snap
	g.emit("pipe")
	g.updateCamera()
}

// updatePipeOut rises; on the last tick the player snaps to standing on
// the mouth and play resumes.
func (g *Game) updatePipeOut() {
	g.Player.Pos.Y -= PipeSinkSpeed
	g.stateTimer--
	if g.stateTimer > 0 {
		return
	}
	p := g.Player
	p.Pos.Y = float64(g.pipeTop) - p.H
	p.Vel = Vec{}
	p.Grounded = true
	g.State = StatePlaying
	g.updateCamera()
}
