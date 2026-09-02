package engine

import "testing"

// The boss arena every test plays on: a 15-row castle slice with a lava
// pool spanned by a seven-plank bridge, one bowser spawn on the bridge
// and the axe on the ground past the pool. Ground rows are 13-14, so the
// bridge row is 13 and a 'Z' on row 12 stands on top of the planks.
//
//	12   M           Z      x          player 2, bowser 14, axe 21
//	13 ##########bbbbbbbL##########    bridge 10-16 over the pool
//	14 ##########LLLLLLLL##########    lava 10-17
//
// Bowser patrols [HomeX-BowserPatrol, HomeX] = [10.5, 14] and its fire
// breaths fly left, so a player standing at x >= 18 fights fireball-only
// and never trades contact damage.
func bossRows() []string {
	rows := make([]string, 12) // open air; ParseLevel pads to the widest row
	rows = append(rows,
		"  M           Z      x      ",
		"##########bbbbbbbL##########",
		"##########LLLLLLLL##########",
	)
	return rows
}

// mustBossLevel parses the boss arena.
func mustBossLevel(t *testing.T) *Level {
	t.Helper()
	return mustParse(t, bossRows()...)
}

// stepUntil runs Update with in until cond holds or the tick budget is
// spent; it reports whether cond held. cond is checked before each tick,
// so the state observed on return is exactly the state the previous
// Update produced (g.Events included).
func stepUntil(g *Game, maxTicks int, in Input, cond func() bool) bool {
	for range maxTicks {
		if cond() {
			return true
		}
		g.Update(in)
	}
	return cond()
}

// bridgeCols lists the column of every bridge tile in the level, scanned
// row-major (so already left-to-right within the single bridge row).
func bridgeCols(l *Level) []int {
	var cols []int
	for y := range l.Height {
		for x := range l.Width {
			if l.At(x, y) == TileBridge {
				cols = append(cols, x)
			}
		}
	}
	return cols
}

// harvest records this tick's transient sound events into seen —
// g.Events is wiped at the top of every Update, so a multi-phase poll
// must collect them as it steps: the bowser can sink mid-sweep, ticks
// before a sink-specific poll first runs (and finds its condition
// already true with a since-overwritten event slice).
func harvest(g *Game, seen map[string]bool) {
	for _, ev := range g.Events {
		seen[ev] = true
	}
}

// stepHarvest is stepUntil that harvests events every tick it steps.
func stepHarvest(g *Game, maxTicks int, cond func() bool, seen map[string]bool) bool {
	for range maxTicks {
		if cond() {
			return true
		}
		g.Update(Input{})
		harvest(g, seen)
	}
	return cond()
}

func TestBowserFireballKill(t *testing.T) {
	g := newGame(t, mustBossLevel(t))
	g.Player.fireUp()
	// Stand right of the pool facing the bridge and duel from there.
	g.Player.Pos = Vec{18.5, float64(GroundTop) - g.Player.H}
	g.Player.Facing = -1
	b := g.Bowsers[0]

	// Pulse the run key: every other tick is a rising edge, so a fresh
	// fireball leaves the moment the two-alive cap allows it.
	pulse := false
	for i := 0; i < 2400 && !b.Flipped; i++ {
		pulse = !pulse
		g.Update(Input{Run: pulse})
	}
	if !b.Flipped {
		t.Fatalf("fireballs never killed bowser (hp=%d state=%v pos=%v)", b.HP, b.State, b.Pos)
	}
	if b.State != BowserFalling {
		t.Errorf("killed bowser state = %v, want falling", b.State)
	}
	if g.Score != BowserScore {
		t.Errorf("kill score = %d, want exactly %d", g.Score, BowserScore)
	}
	if !eventsContain(g.Events, "bowserdie") {
		t.Error("fireball kill did not emit bowserdie")
	}
	if g.Player.Power != PowerFire || g.State != StatePlaying || g.Lives != StartLives {
		t.Errorf("the shooter was hurt during the duel (power=%v state=%v lives=%d)",
			g.Player.Power, g.State, g.Lives)
	}
	// The corpse falls through the still-standing planks (falling skips
	// collision) into the lava below and leaves the world.
	if !stepUntil(g, 400, Input{}, func() bool { return b.Gone }) {
		t.Fatalf("killed bowser never left the world (state=%v pos=%v)", b.State, b.Pos)
	}
	if g.Score < BowserScore {
		t.Errorf("score after the lava drop = %d, want at least %d", g.Score, BowserScore)
	}
}

func TestAxeBridgeCollapseEndsLevel(t *testing.T) {
	l := mustBossLevel(t)

	// Frozen parse contract: a solid bridge over the lava, one axe
	// marker, one bowser with its feet planted on the bridge row.
	if !TileBridge.Solid() {
		t.Fatal("TileBridge must be solid")
	}
	if l.FlagX != -1 || l.AxeX != 21 || l.AxeY != 12 || l.GoalX() != 21 {
		t.Fatalf("goal geometry: flag=%d axe=(%d,%d) goalX=%d, want flagless axe (21,12) goalX 21",
			l.FlagX, l.AxeX, l.AxeY, l.GoalX())
	}
	wantSpawn := Vec{14, 12 + 1 - BowserH}
	if len(l.BowserSpawns) != 1 || l.BowserSpawns[0] != wantSpawn {
		t.Fatalf("bowser spawns = %v, want [%v]", l.BowserSpawns, wantSpawn)
	}
	if cols := bridgeCols(l); len(cols) != 7 || cols[0] != 10 || cols[len(cols)-1] != 16 {
		t.Fatalf("bridge columns = %v, want the span 10..16", cols)
	}
	if l.At(17, 13) != Lava {
		t.Errorf("pool right edge (17,13) = %v, want lava", l.At(17, 13))
	}

	g := newGame(t, l)
	g.Player.Pos = Vec{19.5, float64(GroundTop) - g.Player.H}

	// Walk into the axe.
	if !stepUntil(g, 600, Input{Right: true}, func() bool { return g.State == StateBridgeFall }) {
		t.Fatalf("walking into the axe never started the collapse (state=%v pos=%v)",
			g.State, g.Player.Pos)
	}
	if !eventsContain(g.Events, "axe") {
		t.Error("axe grab did not emit axe")
	}
	b := g.Bowsers[0]

	// The bridge sweeps away one plank column at a time, leftmost first,
	// harvesting sound events as it goes: the bowser sinks mid-sweep,
	// and g.Events is wiped every tick.
	seen := map[string]bool{}
	for next := 10; len(bridgeCols(g.Level)) > 0; next++ {
		before := len(bridgeCols(g.Level))
		if !stepHarvest(g, BridgeSweepTicks+10, func() bool {
			return len(bridgeCols(g.Level)) < before
		}, seen) {
			t.Fatalf("bridge sweep stalled with %d planks left", before)
		}
		if got := g.Level.At(next, 13); got != Empty {
			t.Errorf("sweep emptied column %d out of order (tile %v), want the leftmost plank first",
				next, got)
		}
	}

	// Bowser drops into the pool, pays once and sinks out of the world.
	if !stepHarvest(g, 300, func() bool { return b.State == BowserSinking || b.Gone }, seen) {
		t.Fatalf("bowser never sank into the lava (state=%v pos=%v)", b.State, b.Pos)
	}
	if !seen["bowserdie"] {
		t.Error("lava death did not emit bowserdie")
	}
	if !stepUntil(g, 800, Input{}, func() bool { return g.InCastle }) {
		t.Fatalf("the player never reached the castle door (pos=%v)", g.Player.Pos)
	}
	if !stepUntil(g, CastleDwellTicks+200, Input{}, func() bool { return g.State == StateScoreTick }) {
		t.Fatalf("door dwell never became the score countdown (state=%v)", g.State)
	}
	g.Time = 20 // cash the remaining time quickly
	if !stepUntil(g, 100, Input{}, func() bool { return g.State == StateWin }) {
		t.Fatalf("single-level game never reached the win screen (state=%v time=%d)",
			g.State, g.Time)
	}
}

func TestBowserContactHurtsNeverStomps(t *testing.T) {
	for _, tc := range []struct {
		name  string
		super bool
	}{
		{"small dies", false},
		{"super shrinks", true},
	} {
		g := newGame(t, mustBossLevel(t))
		if tc.super {
			g.Player.grow()
		}
		// Drop the player onto Bowser from above, mid-fall: landing on
		// the boss must hurt, never bounce.
		g.Player.Pos = Vec{14.5, float64(GroundTop) - g.Player.H - 1.5}
		g.Player.Vel = Vec{0, 0.2}
		g.Player.Grounded = false
		b := g.Bowsers[0]
		g.Update(Input{})

		if b.HP != BowserFireHP || b.Flipped || b.Gone {
			t.Errorf("%s: contact damaged bowser (hp=%d flipped=%v gone=%v), want untouched",
				tc.name, b.HP, b.Flipped, b.Gone)
		}
		if !tc.super {
			if g.State != StateDying || g.Lives != StartLives-1 {
				t.Errorf("%s: state=%v lives=%d, want dying at %d lives (a stomp would have bounced instead)",
					tc.name, g.State, g.Lives, StartLives-1)
			}
			continue
		}
		if g.Player.Power != PowerSmall || g.Player.Invincible <= 0 {
			t.Errorf("%s: power=%v invincible=%d, want shrunk to small and blinking",
				tc.name, g.Player.Power, g.Player.Invincible)
		}
		if g.State != StatePlaying || g.Lives != StartLives {
			t.Errorf("%s: super player died anyway (state=%v lives=%d)", tc.name, g.State, g.Lives)
		}
	}
}

func TestStarKillsBowser(t *testing.T) {
	g := newGame(t, mustBossLevel(t))
	g.Player.Star = StarTicks
	g.Player.Pos = Vec{14.5, float64(GroundTop) - g.Player.H}
	b := g.Bowsers[0]

	g.Update(Input{})

	if !b.Flipped || b.State != BowserFalling {
		t.Errorf("starred contact: bowser flipped=%v state=%v, want flipped and falling",
			b.Flipped, b.State)
	}
	if g.Score != BowserScore {
		t.Errorf("star kill score = %d, want exactly %d", g.Score, BowserScore)
	}
	if !eventsContain(g.Events, "bowserdie") {
		t.Error("star kill did not emit bowserdie")
	}
	if g.State != StatePlaying || g.Lives != StartLives {
		t.Errorf("starred player took damage: state=%v lives=%d", g.State, g.Lives)
	}
}

func TestBossFireHurtsPlayer(t *testing.T) {
	for _, tc := range []struct {
		name  string
		star  bool
		super bool
	}{
		{"small dies", false, false},
		{"super shrinks", false, true},
		{"star immune", true, false},
	} {
		g := newGame(t, mustBossLevel(t))
		if tc.super {
			g.Player.grow()
		}
		if tc.star {
			g.Player.Star = StarTicks
		}
		// A breath parked on the player's chest.
		g.BossFires = append(g.BossFires, &BossFire{
			Pos:   Vec{g.Player.Pos.X, g.Player.Pos.Y + 0.2},
			Vel:   Vec{-BowserFireSpeed, 0},
			BaseY: g.Player.Pos.Y + 0.2,
			Life:  BossFireLife,
		})
		breath := g.BossFires[0]
		g.Update(Input{})

		switch {
		case tc.star:
			if g.State != StatePlaying || g.Lives != StartLives {
				t.Errorf("%s: starred player burned (state=%v lives=%d)", tc.name, g.State, g.Lives)
			}
			if breath.Gone {
				t.Errorf("%s: the breath should persist through a starred player", tc.name)
			}
		case tc.super:
			if g.Player.Power != PowerSmall || g.Player.Invincible <= 0 {
				t.Errorf("%s: power=%v invincible=%d, want shrunk to small and blinking",
					tc.name, g.Player.Power, g.Player.Invincible)
			}
			if g.State != StatePlaying || g.Lives != StartLives {
				t.Errorf("%s: super player died to a breath (state=%v lives=%d)", tc.name, g.State, g.Lives)
			}
		default:
			if g.State != StateDying || g.Lives != StartLives-1 {
				t.Errorf("%s: state=%v lives=%d, want dying at %d lives", tc.name, g.State, g.Lives, StartLives-1)
			}
		}
	}
}

func TestCastleGoalReachable(t *testing.T) {
	// 2-4 ends at the axe, not a flagpole: the completability contract
	// must hold for the boss arena ending.
	if !flagReachable(level7()) {
		t.Error("flagReachable(level7()) = false, want the 2-4 axe reachable from the spawn")
	}
}

func TestBowserDeterminism(t *testing.T) {
	script := make([]Input, 600)
	for i := range script {
		switch {
		case i < 300: // hold position and duel: run-key pulses throw fireballs
			script[i] = Input{Run: i%2 == 0}
		case i == 320:
			script[i] = Input{Up: true}
		case i < 450:
			script[i] = Input{}
		default: // walk left across the bridge into the remains of the fight
			script[i] = Input{Left: true, Run: i%3 == 0}
		}
	}
	prep := func() *Game {
		g := newGame(t, mustBossLevel(t))
		g.Player.fireUp()
		g.Player.Pos = Vec{18.5, float64(GroundTop) - g.Player.H}
		g.Player.Facing = -1
		return g
	}
	a, b := prep(), prep()
	for i := range script {
		a.Update(script[i])
		b.Update(script[i])
	}

	if a.Score != b.Score || a.Time != b.Time || a.Lives != b.Lives || a.State != b.State {
		t.Errorf("runs diverged: score %d/%d time %d/%d lives %d/%d state %v/%v",
			a.Score, b.Score, a.Time, b.Time, a.Lives, b.Lives, a.State, b.State)
	}
	if len(bridgeCols(a.Level)) != len(bridgeCols(b.Level)) {
		t.Errorf("bridge diverged: %d vs %d planks", len(bridgeCols(a.Level)), len(bridgeCols(b.Level)))
	}
	pa, pb := a.Player, b.Player
	if pa.Pos != pb.Pos || pa.Vel != pb.Vel || pa.Power != pb.Power ||
		pa.Star != pb.Star || pa.Invincible != pb.Invincible {
		t.Errorf("players diverged: pos %v/%v vel %v/%v power %v/%v star %d/%d inv %d/%d",
			pa.Pos, pb.Pos, pa.Vel, pb.Vel, pa.Power, pb.Power, pa.Star, pb.Star, pa.Invincible, pb.Invincible)
	}
	if len(a.Bowsers) != len(b.Bowsers) {
		t.Fatalf("bowser count diverged: %d vs %d", len(a.Bowsers), len(b.Bowsers))
	}
	for i := range a.Bowsers {
		ba, bb := a.Bowsers[i], b.Bowsers[i]
		if ba.Pos != bb.Pos || ba.Vel != bb.Vel || ba.HP != bb.HP || ba.State != bb.State ||
			ba.Clock != bb.Clock || ba.Timer != bb.Timer || ba.Flash != bb.Flash ||
			ba.Dir != bb.Dir || ba.Flipped != bb.Flipped || ba.Gone != bb.Gone {
			t.Errorf("bowser %d diverged: pos %v/%v vel %v/%v hp %d/%d state %v/%v clock %d/%d "+
				"timer %d/%d flash %d/%d dir %d/%d flipped %v/%v gone %v/%v",
				i, ba.Pos, bb.Pos, ba.Vel, bb.Vel, ba.HP, bb.HP, ba.State, bb.State,
				ba.Clock, bb.Clock, ba.Timer, bb.Timer, ba.Flash, bb.Flash,
				ba.Dir, bb.Dir, ba.Flipped, bb.Flipped, ba.Gone, bb.Gone)
		}
	}
	if len(a.BossFires) != len(b.BossFires) {
		t.Fatalf("boss fire count diverged: %d vs %d", len(a.BossFires), len(b.BossFires))
	}
	for i := range a.BossFires {
		fa, fb := a.BossFires[i], b.BossFires[i]
		if fa.Pos != fb.Pos || fa.Life != fb.Life || fa.Wave != fb.Wave || fa.Gone != fb.Gone {
			t.Errorf("boss fire %d diverged: pos %v/%v life %d/%d wave %v/%v gone %v/%v",
				i, fa.Pos, fb.Pos, fa.Life, fb.Life, fa.Wave, fb.Wave, fa.Gone, fb.Gone)
		}
	}
}

func TestRespawnRestoresBridgeAndBoss(t *testing.T) {
	g := newGame(t, mustBossLevel(t))
	// Park right of the pool and wait for Bowser to breathe once, so the
	// reset assertion covers a live fire. If the mood hash stays quiet,
	// inject one: the respawn contract is the same either way.
	g.Player.Pos = Vec{19, float64(GroundTop) - g.Player.H}
	if !stepUntil(g, 600, Input{}, func() bool { return len(g.BossFires) > 0 }) {
		g.BossFires = append(g.BossFires, &BossFire{
			Pos:   Vec{13, 12.4},
			Vel:   Vec{-BowserFireSpeed, 0},
			BaseY: 12.4,
			Life:  BossFireLife,
		})
		t.Log("bowser never breathed; injected a boss fire to pin the reset")
	}
	if len(g.BossFires) == 0 {
		t.Fatal("no boss fire in flight before the death")
	}

	// Step onto the bridge and walk into Bowser: a mid-fight death.
	g.Player.Pos = Vec{16.2, float64(GroundTop) - g.Player.H}
	if !stepUntil(g, 300, Input{Left: true}, func() bool { return g.State == StateDying }) {
		t.Fatalf("walking into bowser never killed the player (state=%v pos=%v)",
			g.State, g.Player.Pos)
	}
	if !stepUntil(g, DyingTicks+WorldCardTicks+20, Input{}, func() bool { return g.State == StatePlaying }) {
		t.Fatalf("never respawned (state=%v)", g.State)
	}

	// The reload must rebuild the bridge, respawn Bowser fresh at its
	// spawn and clear every boss fire.
	for _, x := range []int{10, 11, 12, 13, 14, 15, 16} {
		if got := g.Level.At(x, 13); got != TileBridge {
			t.Errorf("respawn: bridge tile (%d,13) = %v, want bridge", x, got)
		}
	}
	if len(g.Bowsers) != 1 {
		t.Fatalf("respawn: %d bowsers, want 1", len(g.Bowsers))
	}
	b := g.Bowsers[0]
	want := Vec{14, 12 + 1 - BowserH}
	if b.Pos != want || b.HP != BowserFireHP || b.State != BowserIdle || b.Flipped || b.Gone {
		t.Errorf("respawned bowser: pos %v hp %d state %v flipped=%v gone=%v, want pos %v hp %d idle",
			b.Pos, b.HP, b.State, b.Flipped, b.Gone, want, BowserFireHP)
	}
	if len(g.BossFires) != 0 {
		t.Errorf("respawn: %d boss fires survived, want 0", len(g.BossFires))
	}
}
