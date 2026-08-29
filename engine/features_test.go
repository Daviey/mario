package engine

import (
	"math"
	"testing"
)

// Tests for the gameplay/feel feature set: stomp combos, checkpoints,
// piranha plants, fire flower + fireballs, hurry mode, world cards and
// the daily generator.

func TestStompComboLadderPaysAndResets(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) {
		b.Set(30, 12, 'G')
		b.Set(40, 12, 'G')
	})
	g := newGame(t, l)
	if len(g.Enemies) != 2 {
		t.Fatalf("setup: enemies=%d", len(g.Enemies))
	}

	// A falling player stomping two enemies without landing climbs the
	// ladder: 100 then 200.
	stomp := func(x float64) {
		g.Player.Pos = Vec{x - 0.4, 11.2}
		g.Player.Vel = Vec{Y: 0.06}
		g.Player.Grounded = false
		g.playerEnemyInteractions()
	}
	stomp(30.5)
	if g.Score != 100 {
		t.Fatalf("first stomp score = %d, want 100", g.Score)
	}
	stomp(40.5)
	if g.Score != 300 {
		t.Fatalf("chained stomp score = %d, want 300 (ladder 100+200)", g.Score)
	}

	// Landing ends the chain: the next stomp pays the ladder's first rung.
	g.Player.Pos = Vec{50, float64(GroundTop) - g.Player.H}
	g.Player.Vel = Vec{}
	g.updatePlayer(Input{}) // lands on the ground: chain resets
	l2 := buildLevel(t, 60, func(b *Builder) { b.Set(52, 12, 'G') })
	g2 := newGame(t, l2)
	g2.Player.stompChain = 3 // mid-ladder, then land
	g2.Player.Pos = Vec{51.6, 11.2}
	g2.Player.Vel = Vec{Y: 0.06}
	g2.Player.Grounded = false
	g2.playerEnemyInteractions()
	if want := stompLadder[3]; g2.Score != want {
		t.Fatalf("mid-ladder stomp = %d, want %d", g2.Score, want)
	}
	g2.Player.Pos = Vec{55, float64(GroundTop) - g2.Player.H}
	g2.Player.Vel = Vec{}
	g2.updatePlayer(Input{}) // land: chain resets
	if g2.Player.stompChain != 0 {
		t.Fatal("landing did not reset the stomp chain")
	}
}

func TestStompChainEndAwardsOneUp(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	g.Player.stompChain = len(stompLadder)
	lives := g.Lives
	g.awardStomp(1, 1)
	if g.Lives != lives+1 {
		t.Errorf("past-ladder stomp did not award 1-UP: %d -> %d", lives, g.Lives)
	}
	if !eventsContain(g.Events, "oneup") {
		t.Errorf("events = %v, want oneup", g.Events)
	}
}

func TestCheckpointRespawn(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) {
		b.Ground(30, 59)
		b.Set(35, 12, 'G') // hazards only past the checkpoint
	})
	if l.CheckpointX < 30 {
		t.Fatalf("checkpoint %f, want past the pit at 30", l.CheckpointX)
	}
	g := newGame(t, l)
	// Walk past the checkpoint, then die by timeout.
	g.Player.Pos.X = l.CheckpointX + 1
	g.Update(Input{})
	if g.checkpoint < 0 {
		t.Fatal("crossing the checkpoint did not arm it")
	}
	g.Time = 1
	run(g, TicksPerTimeUnit+2, Input{})
	if g.State != StateDying {
		t.Fatalf("state = %v, want dying", g.State)
	}
	run(g, DyingTicks, Input{})
	g.Update(Input{AnyKey: true}) // skip the card
	if g.State != StatePlaying {
		t.Fatalf("state = %v, want playing", g.State)
	}
	if g.Player.Pos.X < l.CheckpointX-0.5 {
		t.Errorf("respawned at %f, want the checkpoint %f", g.Player.Pos.X, l.CheckpointX)
	}
}

func TestPlantMercyAndDamage(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) {
		b.Pipe(20, 2)
		b.Plant(20, 2)
	})
	g := newGame(t, l)
	if len(g.Plants) != 1 {
		t.Fatalf("plants = %d, want 1", len(g.Plants))
	}
	// Standing on the pipe keeps the plant down (mercy).
	g.Player.Pos = Vec{20.2, 11 - SmallH}
	for range 400 {
		g.Update(Input{})
		if g.Plants[0].State != PlantHidden {
			t.Fatal("plant rose while the player stood on the pipe")
		}
	}
	// A risen plant hurts on contact: a small player dies.
	g.Plants[0].State = PlantUp
	g.Plants[0].Timer = PlantUpTicks
	g.Plants[0].Pos.Y = g.Plants[0].BaseY - PlantH
	g.Player.Pos = Vec{20.6, g.Plants[0].Pos.Y + 0.1}
	g.Player.Vel = Vec{}
	g.Player.Invincible = 0
	for range 3 {
		g.Update(Input{})
	}
	if g.State != StateDying {
		t.Errorf("small player touched a risen plant: state=%v power=%v", g.State, g.Player.Power)
	}
}

func TestFireflowerBlockHonorsPowerState(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Set(20, 9, 'f') })

	bump := func(power PowerLevel) *Game {
		g := newGame(t, l)
		if power >= PowerSuper {
			g.Player.grow()
		}
		// Jump into the block from below.
		g.Player.Pos = Vec{19.7, 12 - (g.Player.H - SmallH)}
		g.Player.Vel = Vec{Y: -0.5}
		for range 12 {
			g.Update(Input{})
		}
		return g
	}

	small := bump(PowerSmall)
	if len(small.Mushrooms) != 1 || len(small.FireFlowers) != 0 {
		t.Fatalf("small bump: mushrooms=%d flowers=%d, want 1/0", len(small.Mushrooms), len(small.FireFlowers))
	}
	super := bump(PowerSuper)
	if len(super.FireFlowers) != 1 {
		t.Fatalf("super bump: flowers=%d, want 1", len(super.FireFlowers))
	}
	for _, f := range super.FireFlowers {
		f.Emerge = 0
		f.Pos = super.Player.Pos
	}
	super.Update(Input{})
	if super.Player.Power != PowerFire {
		t.Fatalf("power = %v, want fire after flower", super.Player.Power)
	}
}

func TestFireballThrowEdgeAndCap(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	g.Player.Power = PowerFire
	g.Player.Pos = Vec{30, 12}
	g.Update(Input{Run: true}) // rising edge throws
	if n := g.aliveFireballs(); n != 1 {
		t.Fatalf("fireballs after first edge = %d, want 1", n)
	}
	if !eventsContain(g.Events, "fire") {
		t.Errorf("events = %v after throw, want fire", g.Events)
	}
	g.Update(Input{Run: true}) // holding does not rethrow
	if n := g.aliveFireballs(); n != 1 {
		t.Fatalf("hold rethrew: %d fireballs", n)
	}
	g.Update(Input{}) // release
	g.Update(Input{Run: true})
	if n := g.aliveFireballs(); n != 2 {
		t.Fatalf("fireballs = %d, want 2", n)
	}
	g.Update(Input{})
	g.Update(Input{Run: true})
	if n := g.aliveFireballs(); n != 2 {
		t.Fatalf("fireball cap not enforced: %d", n)
	}
}

func TestCheatModeLiftsFireballCap(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	g.Player.Power = PowerFire
	g.Player.Pos = Vec{30, 12}
	g.Cheats = true
	// Three rising edges with the two oldest still in flight: the cap
	// must not clamp at MaxFireballs while cheats are on.
	for range 3 {
		g.Update(Input{})          // release
		g.Update(Input{Run: true}) // rising edge throws
	}
	if n := g.aliveFireballs(); n != 3 {
		t.Fatalf("cheat fireballs = %d, want 3 (cap lifted)", n)
	}
	// Cheats off again: the ordinary cap reasserts on the next throw
	// (three are still in flight, so no fourth can leave).
	g.Cheats = false
	g.Update(Input{})
	g.Update(Input{Run: true})
	if n := g.aliveFireballs(); n != 3 {
		t.Fatalf("cap must reapply with cheats off: %d fireballs", n)
	}
}

func TestFireballKillsEnemy(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Set(26, 12, 'G') })
	g := newGame(t, l)
	g.Player.Power = PowerFire
	g.Player.Pos = Vec{20, 12}
	g.throwFireball()
	fb := g.Fireballs[0]
	fb.Vel = Vec{X: FireballSpeed, Y: 0}
	for range 120 {
		g.Update(Input{})
		if fb.Gone {
			break
		}
	}
	killed := fb.Gone && (len(g.Enemies) == 0 || g.Enemies[0].State == EnemyFlipped)
	if !killed {
		t.Fatalf("fireball did not kill: gone=%v enemies=%d state=%v", fb.Gone, len(g.Enemies), g.Enemies[0].State)
	}
	if g.Score < StompScore {
		t.Errorf("score = %d, fireball kill unpaid", g.Score)
	}
}

func TestHurryTransition(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	g.Time = HurryTime + 1
	g.Tick = 0
	run(g, TicksPerTimeUnit+2, Input{})
	if !g.Hurry {
		t.Fatal("hurry not armed after time crossed the threshold")
	}
	if g.HurryT <= 0 {
		t.Error("hurry flash not primed")
	}
}

func TestWorldCardShowsDailyName(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	g.Daily = true
	if got := g.CardName(); got[:5] != "DAILY" {
		t.Errorf("CardName = %q, want DAILY prefix", got)
	}
	g.Daily = false
	if got := g.CardName(); got[:5] != "WORLD" {
		t.Errorf("CardName = %q, want WORLD prefix", got)
	}
}

func TestSoundEventsFire(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	g.Update(Input{Up: true})
	if !eventsContain(g.Events, "jump") {
		t.Errorf("events = %v after jump, want jump", g.Events)
	}
	g.Time = 1
	sawDie := false
	for range TicksPerTimeUnit + 2 {
		g.Update(Input{})
		sawDie = sawDie || eventsContain(g.Events, "die")
	}
	if !sawDie {
		t.Error("no die event emitted on death")
	}
}

func eventsContain(events []string, want string) bool {
	for _, e := range events {
		if e == want {
			return true
		}
	}
	return false
}

func TestDailyLevelDeterministic(t *testing.T) {
	a := DailyLevel(20260826)
	b := DailyLevel(20260826)
	if a.Width != b.Width {
		t.Fatalf("width %d != %d", a.Width, b.Width)
	}
	for i := range a.Tiles {
		if a.Tiles[i] != b.Tiles[i] {
			t.Fatalf("tile %d differs: %v != %v", i, a.Tiles[i], b.Tiles[i])
		}
	}
	if a.Name != b.Name || len(a.PlantSpawns) != len(b.PlantSpawns) {
		t.Error("metadata diverged for identical seeds")
	}
	// Same play, same result: identical scripted runs must score alike.
	play := func() (int, State) {
		g := NewGame([]*Level{DailyLevel(42)}, 40, LevelHeight)
		g.State = StatePlaying
		for t := range 4000 {
			g.Update(Input{Right: true, Run: true, Up: t%40 < 18})
			if g.State == StateGameOver || g.State == StateWin {
				break
			}
		}
		return g.Score, g.State
	}
	s1, st1 := play()
	s2, st2 := play()
	if s1 != s2 || st1 != st2 {
		t.Errorf("daily runs diverged: %d/%v vs %d/%v", s1, st1, s2, st2)
	}
}

func TestDailyLevelsSolvableShape(t *testing.T) {
	for seed := range 30 {
		l := DailyLevel(uint64(1e6 + seed))
		if l.FlagX <= 0 {
			t.Fatalf("seed %d: no flag", seed)
		}
		// Pits at most 4 wide (a full-speed jump covers ~5).
		ground := l.Height - 2
		pit := 0
		for x := range l.Width {
			if !l.At(x, ground).Solid() {
				pit++
				if pit > 4 {
					t.Fatalf("seed %d: pit wider than 4 at x=%d", seed, x)
				}
			} else {
				pit = 0
			}
		}
		// Every plant sits on a pipe mouth.
		for _, p := range l.PlantSpawns {
			if l.At(int(math.Round(p.X-0.65)), int(p.Y)) != Pipe {
				t.Fatalf("seed %d: plant at %v not on a pipe", seed, p)
			}
		}
		// Player start and flag stand on solid ground.
		if !l.At(int(l.PlayerStart.X), ground).Solid() {
			t.Fatalf("seed %d: player start floats", seed)
		}
		if !l.At(l.FlagX, ground).Solid() {
			t.Fatalf("seed %d: flag floats", seed)
		}
	}
}

func TestUndergroundThemeAndCeiling(t *testing.T) {
	levels := DefaultLevels()
	if levels[1].Theme != ThemeUnderground {
		t.Fatal("1-2 is not the underground theme")
	}
	for x := range levels[1].Width {
		if levels[1].At(x, 0) != Brick {
			t.Fatalf("underground ceiling gap at x=%d", x)
		}
	}
}

func TestAttractDemoRoundTrip(t *testing.T) {
	g := NewGame(DefaultLevels(), 40, LevelHeight)
	g.BeginDemo()
	if !g.Demo || g.State != StatePlaying {
		t.Fatalf("demo: state=%v demo=%v", g.State, g.Demo)
	}
	run(g, 600, Input{Right: true}) // the demo loop ignores real input
	g.EndDemo()
	if g.Demo || g.State != StateTitle {
		t.Fatalf("end demo: state=%v demo=%v", g.State, g.Demo)
	}
	if g.Lives != StartLives {
		t.Errorf("lives %d after demo, want reset", g.Lives)
	}
}
