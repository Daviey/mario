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
	for i := 0; i < 600; i++ {
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
	for i := 0; i < 60 && e.State == EnemyWalking; i++ {
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
	for i := 0; i < 60; i++ {
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
	g.Player.Super = true
	g.Player.W, g.Player.H = SuperW, SuperH
	g.Player.Pos = Vec{18.5, 13 - SuperH}
	run(g, 120, Input{Right: true})
	if !g.Player.Super && g.State == StatePlaying {
		// Shrunk and survived.
		if g.Player.H != SmallH {
			t.Errorf("height = %f, want small", g.Player.H)
		}
		if g.Player.Invincible == 0 {
			t.Error("no invincibility granted after shrink")
		}
		return
	}
	t.Fatalf("super player died or stayed super: state=%v super=%v", g.State, g.Player.Super)
}

func TestInvinciblePreventsDamage(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Set(20, 12, 'G') })
	g := newGame(t, l)
	g.Player.Super = true
	g.Player.W, g.Player.H = SuperW, SuperH
	g.Player.Pos = Vec{18.5, 13 - SuperH}
	// Damage once, then stay overlapped while invincible.
	run(g, 60, Input{Right: true})
	if g.Player.Super || g.State != StatePlaying {
		t.Fatalf("setup failed: super=%v state=%v", g.Player.Super, g.State)
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
	for i := 0; i < 60 && e.State == EnemyWalking; i++ {
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

func TestStompMovingShellStopsIt(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Set(20, 12, 'K') })
	g := newGame(t, l)
	e := g.Enemies[0]
	e.State = EnemyShellMoving
	e.H = GoombaH
	e.Dir = -1
	e.Pos.Y = 13 - GoombaH
	dropPlayer(g, 20, 11.2) // close enough to catch the moving shell
	for i := 0; i < 60 && e.State == EnemyShellMoving; i++ {
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
	for i := 0; i < 600 && e.Dir == 1; i++ {
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
	if g.Score != StompScore {
		t.Errorf("double flip scored twice: %d", g.Score)
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
