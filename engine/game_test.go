package engine

import "testing"

func TestTitleStartsOnAnyKey(t *testing.T) {
	g := NewGame([]*Level{buildLevel(t, 60)}, 40, LevelHeight)
	if g.State != StateTitle {
		t.Fatalf("state = %v, want title", g.State)
	}
	g.Update(Input{})
	if g.State != StateTitle {
		t.Fatal("title dismissed without input")
	}
	g.Update(Input{AnyKey: true})
	if g.State != StatePlaying {
		t.Errorf("state = %v, want playing", g.State)
	}
	// Movement keys also start the game.
	g2 := NewGame([]*Level{buildLevel(t, 60)}, 40, LevelHeight)
	g2.Update(Input{Right: true})
	if g2.State != StatePlaying {
		t.Errorf("state = %v after Right", g2.State)
	}
}

func TestPauseFreezes(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	g.Update(Input{Pause: true})
	if !g.Paused {
		t.Fatal("pause edge did not toggle pause")
	}
	before := snapshot(g)
	run(g, 100, Input{})
	if after := snapshot(g); after != before {
		t.Errorf("state changed while paused:\n%s\n%s", before, after)
	}
	g.Update(Input{Pause: true})
	if g.Paused {
		t.Error("second pause edge did not unpause")
	}
}

func TestPauseDoesNotConsumeTime(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	g.Update(Input{Pause: true})
	t0 := g.Time
	run(g, 1000, Input{})
	if g.Time != t0 {
		t.Errorf("time ticked while paused: %d -> %d", t0, g.Time)
	}
}

func TestTimerExpiryKills(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	g.Time = 2
	run(g, 2*TicksPerTimeUnit+5, Input{})
	if g.State != StateDying {
		t.Fatalf("state = %v, want dying on time out", g.State)
	}
	if g.Time != 0 {
		t.Errorf("time = %d, want clamped to 0", g.Time)
	}
}

func TestFlagClearsLevelWithBonus(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Flag(40) })
	g := newGame(t, l)
	g.Player.Pos = Vec{37, 13 - SmallH}
	run(g, 60, Input{Right: true})
	if g.State != StateLevelClear {
		t.Fatalf("state = %v, want level clear (x=%f)", g.State, g.Player.Pos.X)
	}
	if want := g.Time * TimeBonusPerUnit; g.Score != want {
		t.Errorf("score = %d, want time bonus %d (time=%d)", g.Score, want, g.Time)
	}
}

func TestLevelClearAdvancesToNextLevel(t *testing.T) {
	levels := []*Level{
		buildLevel(t, 60, func(b *Builder) { b.Flag(40) }),
		buildLevel(t, 60, func(b *Builder) { b.Flag(40) }),
	}
	g := NewGame(levels, 40, LevelHeight)
	g.State = StatePlaying
	g.Player.Pos = Vec{37, 13 - SmallH}
	run(g, 60, Input{Right: true})
	if g.State != StateLevelClear {
		t.Fatalf("state = %v", g.State)
	}
	run(g, ClearTicks, Input{})
	if g.State != StatePlaying || g.LevelIndex() != 1 {
		t.Errorf("state=%v level=%d, want playing level 1", g.State, g.LevelIndex())
	}
}

func TestLastLevelFlagWins(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Flag(40) })
	g := newGame(t, l)
	g.Player.Pos = Vec{37, 13 - SmallH}
	run(g, 60, Input{Right: true})
	run(g, ClearTicks, Input{})
	if g.State != StateWin {
		t.Errorf("state = %v, want win", g.State)
	}
}

func TestDeathRespawnsLevel(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Set(20, 12, 'G') })
	g := newGame(t, l)
	// Stomp the goomba for score.
	e := g.Enemies[0]
	dropPlayer(g, 20, 8)
	for i := 0; i < 60 && e.State == EnemyWalking; i++ {
		g.Update(Input{})
	}
	run(g, 40, Input{})
	if len(g.Enemies) != 0 || g.Score != StompScore {
		t.Fatalf("setup failed: enemies=%d score=%d", len(g.Enemies), g.Score)
	}

	// Die via timeout, then let the respawn delay elapse.
	g.Time = 1
	run(g, TicksPerTimeUnit+2, Input{})
	if g.State != StateDying {
		t.Fatalf("state = %v", g.State)
	}
	run(g, DyingTicks, Input{})
	if g.State != StatePlaying {
		t.Fatalf("state = %v after respawn delay", g.State)
	}
	// Level fully reset: goomba back; score preserved; player small again.
	if len(g.Enemies) != 1 {
		t.Errorf("enemies = %d, want respawned goomba", len(g.Enemies))
	}
	if g.Score != StompScore || g.CoinCount != 0 {
		t.Errorf("score reset on death: %d/%d", g.Score, g.CoinCount)
	}
	if g.Player.Super {
		t.Error("player should respawn small")
	}
}

func TestGameOverAndRestart(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	g.Lives = 1
	g.Time = 1
	run(g, TicksPerTimeUnit+2, Input{})
	run(g, DyingTicks, Input{})
	if g.State != StateGameOver {
		t.Fatalf("state = %v, want game over", g.State)
	}
	// Ignored while dead:
	g.Update(Input{AnyKey: true})
	if g.State != StateGameOver {
		t.Fatal("AnyKey restarted from game over")
	}
	g.Score = 999
	g.Update(Input{Restart: true})
	if g.State != StatePlaying || g.LevelIndex() != 0 {
		t.Fatalf("restart failed: %v/%d", g.State, g.LevelIndex())
	}
	if g.Score != 0 || g.Lives != StartLives || g.CoinCount != 0 {
		t.Errorf("restart did not reset: score=%d lives=%d coins=%d", g.Score, g.Lives, g.CoinCount)
	}
}

func TestDyingAnimationFalls(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	g.Time = 1
	run(g, TicksPerTimeUnit+2, Input{})
	y0 := g.Player.Pos.Y
	run(g, DyingTicks, Input{}) // full hop-then-fall arc
	if g.Player.Pos.Y <= y0 {
		t.Errorf("dying player did not fall: %f -> %f", y0, g.Player.Pos.Y)
	}
}

func TestCameraFollowsAndClamps(t *testing.T) {
	g := newGame(t, buildLevel(t, 200))
	if g.CameraX != 0 {
		t.Fatalf("camera starts at %f", g.CameraX)
	}
	g.Player.Pos = Vec{100, 13 - SmallH}
	g.Update(Input{})
	if g.CameraX < 80 || g.CameraX > 90 {
		t.Errorf("camera = %f, want ~86", g.CameraX)
	}
	g.Player.Pos = Vec{194, 13 - SmallH} // flag sits at 195: stop just short
	g.Update(Input{})
	if want := float64(200 - 40); g.CameraX != want {
		t.Errorf("camera = %f, want clamped %f", g.CameraX, want)
	}
	g.Player.Pos = Vec{0, 13 - SmallH}
	g.Update(Input{})
	if g.CameraX != 0 {
		t.Errorf("camera = %f, want 0 at left edge", g.CameraX)
	}
}

func TestScoreAccumulatesAcrossLevels(t *testing.T) {
	mk := func() *Level { return buildLevel(t, 60, func(b *Builder) { b.Flag(40) }) }
	g := NewGame([]*Level{mk(), mk()}, 40, LevelHeight)
	g.State = StatePlaying

	clearOnce := func() {
		g.Tick = 0 // identical tick phase => identical time drain
		g.Time = 300
		g.Player.Pos = Vec{X: 37, Y: 13 - g.Player.H}
		g.Player.Vel = Vec{}
		run(g, 60, Input{Right: true})
		if g.State != StateLevelClear {
			t.Fatalf("did not clear: %v", g.State)
		}
		run(g, ClearTicks, Input{})
	}

	clearOnce()
	first := g.Score
	if first == 0 {
		t.Fatal("no score from first clear")
	}
	clearOnce()
	if g.Score != 2*first {
		t.Errorf("score = %d, want %d (accumulated)", g.Score, 2*first)
	}
}

func TestSuperCarriesAcrossLevelButNotDeath(t *testing.T) {
	mk := func() *Level { return buildLevel(t, 60, func(b *Builder) { b.Flag(40) }) }
	g := NewGame([]*Level{mk(), mk()}, 40, LevelHeight)
	g.State = StatePlaying
	g.Player.Super = true
	g.Player.Pos = Vec{37, 13 - SuperH}
	run(g, 90, Input{Right: true})
	run(g, ClearTicks, Input{})
	if !g.Player.Super {
		t.Error("super lost on level transition")
	}
	g.Time = 1
	run(g, TicksPerTimeUnit+2, Input{})
	run(g, DyingTicks, Input{})
	if g.Player.Super {
		t.Error("super survived death")
	}
}

func TestDeterminism(t *testing.T) {
	play := func() string {
		g := NewGame(DefaultLevels(), 40, LevelHeight)
		g.State = StatePlaying
		for t := 0; t < 3000; t++ {
			in := Input{Right: true, Run: t%3 != 0, Up: t%97 < 22}
			g.Update(in)
			if g.State != StatePlaying {
				break
			}
		}
		return snapshot(g)
	}
	a, b := play(), play()
	if a != b {
		t.Errorf("nondeterministic simulation:\n%s\n%s", a, b)
	}
}

func TestKillOnlyWhilePlaying(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	g.State = StateLevelClear
	g.kill()
	if g.Lives != StartLives {
		t.Error("kill fired during level clear")
	}
}

func TestPitDeathDuringPlayingOnly(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	g.State = StateDying
	g.Player.Pos.Y = 999
	g.Update(Input{})
	if g.Lives != StartLives {
		t.Error("pit check re-killed a dying player")
	}
}
