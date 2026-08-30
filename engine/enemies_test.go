package engine

import "testing"

// dropPlayer places the player in the air above a target x column.
func dropPlayer(g *Game, x, y float64) {
	g.Player.Pos = Vec{x, y}
	g.Player.Vel = Vec{}
	g.Player.Grounded = false
}

func TestGoombaWalksLeft(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Set(20, 12, 'G') })
	g := newGame(t, l)
	e := g.Enemies[0]
	x0 := e.Pos.X
	run(g, 30, Input{})
	if e.Pos.X >= x0 {
		t.Errorf("goomba did not walk left: %f -> %f", x0, e.Pos.X)
	}
	if !approx(e.Pos.Y, 13-GoombaH) {
		t.Errorf("goomba not grounded: y=%f", e.Pos.Y)
	}
}

func TestGoombaTurnsAtWall(t *testing.T) {
	// Goomba at 20 walking left hits a pipe at 14-15.
	l := buildLevel(t, 60, func(b *Builder) {
		b.Set(20, 12, 'G')
		b.Pipe(14, 2)
	})
	g := newGame(t, l)
	e := g.Enemies[0]
	for range 600 {
		g.Update(Input{})
		if e.Dir == 1 {
			break
		}
	}
	if e.Dir != 1 {
		t.Fatalf("goomba never reversed at wall (x=%f)", e.Pos.X)
	}
	if e.Pos.X < 16-0.001 { // pipe occupies [14,16)
		t.Errorf("goomba penetrated pipe: left=%f", e.Pos.X)
	}
}

func TestGoombaFallsInPitAndIsRemoved(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) {
		b.Set(20, 12, 'G')
		b.Fill(16, GroundTop, 24, LevelHeight-1, ' ') // pit under its path
	})
	g := newGame(t, l)
	run(g, 600, Input{})
	if len(g.Enemies) != 0 {
		t.Errorf("goomba not removed after falling in pit: %d left", len(g.Enemies))
	}
}

func TestStompGoomba(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Set(20, 12, 'G') })
	g := newGame(t, l)
	e := g.Enemies[0]
	dropPlayer(g, 20, 8) // above the goomba
	for range 60 {
		if e.State != EnemyWalking {
			break
		}
		g.Update(Input{})
	}
	if e.State != EnemySquashed {
		t.Fatalf("enemy state = %v, want squashed", e.State)
	}
	if g.Score != StompScore {
		t.Errorf("score = %d, want %d", g.Score, StompScore)
	}
	if g.Player.Vel.Y >= 0 {
		t.Errorf("player did not bounce: vy=%f", g.Player.Vel.Y)
	}
	run(g, 40, Input{})
	if !e.Gone {
		t.Error("squashed goomba not removed after animation")
	}
}

func TestStompBounceHigherWithJumpHeld(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Set(20, 12, 'G') })
	g := newGame(t, l)
	dropPlayer(g, 20, 8)
	minVy := 0.0
	for range 60 {
		g.Update(Input{Up: true})
		if vy := g.Player.Vel.Y; vy < minVy {
			minVy = vy
		}
	}
	if minVy > JumpVel+0.05 {
		t.Errorf("held-jump stomp bounce = %f, want full ~%f", minVy, JumpVel)
	}
}

func TestSideHitSmallPlayerDies(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Set(20, 12, 'G') })
	g := newGame(t, l)
	g.Player.Pos = Vec{18.5, 13 - SmallH}
	run(g, 90, Input{Right: true})
	if g.State != StateDying {
		t.Fatalf("state = %v, want dying", g.State)
	}
	if g.Lives != StartLives-1 {
		t.Errorf("lives = %d", g.Lives)
	}
}

func TestSideHitSuperPlayerShrinks(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Set(20, 12, 'G') })
	g := newGame(t, l)
	g.Player.grow()
	g.Player.W, g.Player.H = SuperW, SuperH
	g.Player.Pos = Vec{18.5, 13 - SuperH}
	run(g, 120, Input{Right: true})
	if g.Player.Power < PowerSuper && g.State == StatePlaying {
		// Shrunk and survived.
		if g.Player.H != SmallH {
			t.Errorf("height = %f, want small", g.Player.H)
		}
		if g.Player.Invincible == 0 {
			t.Error("no invincibility granted after shrink")
		}
		return
	}
	t.Fatalf("super player died or stayed super: state=%v power=%v", g.State, g.Player.Power)
}

func TestInvinciblePreventsDamage(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Set(20, 12, 'G') })
	g := newGame(t, l)
	g.Player.grow()
	g.Player.W, g.Player.H = SuperW, SuperH
	g.Player.Pos = Vec{18.5, 13 - SuperH}
	// Damage once, then stay overlapped while invincible.
	run(g, 60, Input{Right: true})
	if g.Player.Power >= PowerSuper || g.State != StatePlaying {
		t.Fatalf("setup failed: power=%v state=%v", g.Player.Power, g.State)
	}
	// Second contact while invincible must not kill.
	run(g, 60, Input{Right: true})
	if g.State != StatePlaying {
		t.Errorf("invincible player died: state=%v", g.State)
	}
}

func TestKoopaStompMakesShell(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Set(20, 12, 'K') })
	g := newGame(t, l)
	e := g.Enemies[0]
	dropPlayer(g, 20, 8)
	for range 60 {
		if e.State != EnemyWalking {
			break
		}
		g.Update(Input{})
	}
	if e.State != EnemyShell {
		t.Fatalf("state = %v, want shell", e.State)
	}
	if e.H != GoombaH {
		t.Errorf("shell height = %f", e.H)
	}
	if !approx(e.Pos.Y+e.H, 13) {
		t.Errorf("shell feet moved: bottom=%f, want 13", e.Pos.Y+e.H)
	}
}

func TestKickShellFromSide(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Set(20, 12, 'K') })
	g := newGame(t, l)
	e := g.Enemies[0]
	e.State = EnemyShell
	e.H = GoombaH
	e.Pos.Y = 13 - GoombaH
	g.Player.Pos = Vec{18, 13 - SmallH}
	run(g, 90, Input{Right: true})
	if e.State != EnemyShellMoving {
		t.Fatalf("state = %v, want moving shell", e.State)
	}
	if e.Dir != 1 {
		t.Errorf("shell kicked wrong way: dir=%d (player is left of shell)", e.Dir)
	}
	if g.State != StatePlaying {
		t.Errorf("player hurt by idle shell kick: state=%v", g.State)
	}
}

func TestKickShellDeadzoneKeepsDirection(t *testing.T) {
	g := newGame(t, buildLevel(t, 60, func(b *Builder) { b.Set(20, 12, 'K') }))
	e := g.Enemies[0]
	e.State = EnemyShell
	e.H = GoombaH
	e.Pos = Vec{20, 13 - GoombaH}
	e.Dir = -1
	// Centres within KickDeadzone: the kick sends the shell sliding but
	// keeps its stale facing instead of flipping it.
	g.Player.Pos = Vec{20, 13 - SmallH}
	g.kickShell(e)
	if e.State != EnemyShellMoving {
		t.Fatalf("state = %v, want moving shell", e.State)
	}
	if e.Dir != -1 {
		t.Errorf("deadzone kick flipped dir to %d, want stale -1", e.Dir)
	}
	// Centres clearly apart: the kick aims the shell away from the player.
	e.State = EnemyShell
	g.Player.Pos = Vec{18, 13 - SmallH}
	g.kickShell(e)
	if e.Dir != 1 {
		t.Errorf("kick from the left should aim the shell right, dir=%d", e.Dir)
	}
}

func TestStompMovingShellStopsIt(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Set(20, 12, 'K') })
	g := newGame(t, l)
	e := g.Enemies[0]
	e.State = EnemyShellMoving
	e.H = GoombaH
	e.Dir = -1
	e.Pos.Y = 13 - GoombaH
	dropPlayer(g, 20, 11.2) // close enough to catch the moving shell
	for range 60 {
		if e.State != EnemyShellMoving {
			break
		}
		g.Update(Input{})
	}
	if e.State != EnemyShell {
		t.Fatalf("state = %v, want idle shell", e.State)
	}
	if g.Player.Vel.Y >= 0 {
		t.Errorf("no stomp bounce: %f", g.Player.Vel.Y)
	}
}

func TestMovingShellHurtsPlayer(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Set(20, 12, 'K') })
	g := newGame(t, l)
	e := g.Enemies[0]
	e.State = EnemyShellMoving
	e.H = GoombaH
	e.Dir = -1
	e.Pos.Y = 13 - GoombaH
	g.Player.Pos = Vec{17, 13 - SmallH}
	run(g, 120, Input{})
	if g.State == StatePlaying {
		t.Error("moving shell did not hurt the player")
	}
}

func TestShellKillsGoomba(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) {
		b.Set(10, 12, 'K')
		b.Set(20, 12, 'G')
	})
	g := newGame(t, l)
	shell, victim := g.Enemies[0], g.Enemies[1]
	shell.State = EnemyShellMoving
	shell.Dir = 1
	shell.H = GoombaH
	shell.Pos.Y = 13 - GoombaH
	victim.Pos = Vec{20, 13 - GoombaH}

	run(g, 300, Input{})
	if victim.State != EnemyFlipped {
		t.Errorf("goomba state = %v, want flipped", victim.State)
	}
	if g.Score != StompScore {
		t.Errorf("score = %d, want %d", g.Score, StompScore)
	}
}

func TestShellBouncesOffWall(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) {
		b.Set(20, 12, 'K')
		b.Pipe(30, 2)
	})
	g := newGame(t, l)
	e := g.Enemies[0]
	e.State = EnemyShellMoving
	e.H = GoombaH
	e.Dir = 1
	e.Pos.Y = 13 - GoombaH
	for range 600 {
		if e.Dir != 1 {
			break
		}
		g.Update(Input{})
	}
	if e.Dir != -1 {
		t.Fatalf("shell never bounced off pipe (x=%f)", e.Pos.X)
	}
}

func TestShellFallsInPit(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) {
		b.Set(20, 12, 'K')
		b.Fill(22, GroundTop, 30, LevelHeight-1, ' ')
	})
	g := newGame(t, l)
	e := g.Enemies[0]
	e.State = EnemyShellMoving
	e.H = GoombaH
	e.Dir = 1
	e.Pos.Y = 13 - GoombaH
	run(g, 300, Input{})
	if !e.Gone {
		t.Error("shell not removed after falling in pit")
	}
}

func TestFlipEnemyIsIdempotent(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	e := newGoomba(Vec{5, 5})
	g.Enemies = append(g.Enemies, e)
	g.flipEnemy(e)
	g.flipEnemy(e)
	if e.State != EnemyFlipped {
		t.Fatalf("state = %v, want flipped", e.State)
	}
	if g.Score != 0 {
		t.Errorf("flip scored without a caller awarding it: %d", g.Score)
	}
}

func TestShellMowDownReachesOneUp(t *testing.T) {
	// Eleven goombas in the shell's path: the combo ladder has ten
	// rungs, so the eleventh consecutive kill must pay a 1-UP.
	l := buildLevel(t, 200, func(b *Builder) {
		b.Set(10, 12, 'K')
		for i := range 11 {
			b.Set(34+3*i, 12, 'G')
		}
	})
	g := newGame(t, l)
	// Goombas load before koopas, so find the shell by species.
	var shell *Enemy
	for _, e := range g.Enemies {
		if e.Kind == KindKoopa {
			shell = e
		}
	}
	shell.State = EnemyShellMoving
	shell.Dir = 1
	shell.H = GoombaH
	shell.Pos.Y = 13 - GoombaH
	// Flipped victims fall out of the world and are filtered out of
	// g.Enemies, so the mow count is the missing victims.
	mowed := func() int {
		alive := 0
		for _, e := range g.Enemies {
			if e != shell {
				alive++
			}
		}
		return 11 - alive
	}
	// The shell meets the farthest goomba well inside 400 ticks; running
	// longer would let it bounce off the right wall and come back for
	// the player, whose death respawns the level and resets the count.
	for range 400 {
		if mowed() == 11 {
			break
		}
		g.Update(Input{})
	}
	if got := mowed(); got != 11 {
		t.Fatalf("shell mowed %d of 11 goombas", got)
	}
	if g.Lives != StartLives+1 {
		t.Errorf("lives = %d, want %d (past the ladder end pays a 1-UP)", g.Lives, StartLives+1)
	}
}

func TestEnemyUpdateIgnoresGone(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	e := newGoomba(Vec{20, 12})
	e.Gone = true
	g.Enemies = []*Enemy{e}
	run(g, 10, Input{})
	if e.Pos.X != 20 {
		t.Error("gone enemy moved")
	}
	g.cleanup()
	if len(g.Enemies) != 0 {
		t.Error("cleanup kept gone enemy")
	}
}
