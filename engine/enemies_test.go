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

// Both powered states pay the same side-hit toll: shrink to small with
// the feet planted and invincibility granted — fire armour buys nothing
// extra against a plain contact hit.
func TestSideHitPoweredPlayerShrinks(t *testing.T) {
	for _, power := range []PowerLevel{PowerSuper, PowerFire} {
		l := buildLevel(t, 60, func(b *Builder) { b.Set(20, 12, 'G') })
		g := newGame(t, l)
		if power == PowerFire {
			g.Player.fireUp()
		} else {
			g.Player.grow()
		}
		g.Player.W, g.Player.H = SuperW, SuperH
		g.Player.Pos = Vec{18.5, 13 - SuperH}
		run(g, 120, Input{Right: true})
		if g.State != StatePlaying || g.Player.Power != PowerSmall {
			t.Fatalf("power %v: state=%v power=%v, want shrunk and playing",
				power, g.State, g.Player.Power)
		}
		if g.Player.H != SmallH {
			t.Errorf("power %v: height = %f, want small", power, g.Player.H)
		}
		if g.Player.Invincible == 0 {
			t.Errorf("power %v: no invincibility granted after shrink", power)
		}
	}
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
	e.Chain = 5             // mid-ladder: the stop must zero the kill chain
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
	if e.Chain != 0 {
		t.Errorf("chain = %d after stomp-stop, want 0", e.Chain)
	}
	// Re-kick: the shell slides again with a fresh chain, so its next
	// victim is paid from the ladder's first rung, not the stale one.
	g.kickShell(e)
	if e.State != EnemyShellMoving || e.Chain != 0 {
		t.Errorf("re-kick: state=%v chain=%d, want moving with chain 0", e.State, e.Chain)
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

func TestBuzzyFireproof(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	e := newBuzzy(Vec{20, GroundTop - KoopaH})
	g.Enemies = append(g.Enemies, e)
	fb := &Fireball{Pos: Vec{20.1, 12.5}, Vel: Vec{FireballSpeed, 0}}
	g.Fireballs = append(g.Fireballs, fb)
	run(g, 1, Input{})
	if !fb.Gone { // cleanup drops the corpse; assert on the pointer
		t.Error("fireball was not absorbed by the fireproof shell")
	}
	if e.State != EnemyWalking || e.Kind != KindBuzzy {
		t.Errorf("buzzy affected by the fireball: state=%v kind=%v", e.State, e.Kind)
	}
	if g.Score != 0 {
		t.Errorf("absorb paid score: %d", g.Score)
	}
}

func TestBuzzyStompMakesShell(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	e := newBuzzy(Vec{20, GroundTop - KoopaH})
	g.Enemies = append(g.Enemies, e)
	dropPlayer(g, 20, 8)
	for range 60 {
		if e.State != EnemyWalking {
			break
		}
		g.Update(Input{})
	}
	if e.State != EnemyShell {
		t.Fatalf("buzzy state = %v, want shell", e.State)
	}
	if e.H != GoombaH {
		t.Errorf("buzzy shell box H = %f, want %f", e.H, GoombaH)
	}
	if g.Score != StompScore {
		t.Errorf("score = %d, want %d", g.Score, StompScore)
	}
}

func TestRedKoopaTurnsAtLedge(t *testing.T) {
	// The pit spans 24-30; ground holds to col 23. A red koopa on the
	// ledge walking right must turn at the edge and stay up; a green
	// one marches off and falls.
	mk := func(red bool) *Game {
		l := buildLevel(t, 60, func(b *Builder) {
			b.Fill(24, GroundTop, 30, LevelHeight-1, ' ')
		})
		g := newGame(t, l)
		var e *Enemy
		if red {
			e = newKoopaRed(Vec{20, GroundTop - KoopaH})
		} else {
			e = newKoopa(Vec{20, GroundTop - KoopaH})
		}
		e.Dir = 1 // walk toward the pit
		g.Enemies = append(g.Enemies, e)
		return g
	}

	g := mk(true)
	e := g.Enemies[len(g.Enemies)-1]
	flipped := false
	for range 900 {
		g.Update(Input{})
		if e.Dir == -1 {
			flipped = true
		}
		if e.Pos.Y > GroundTop-KoopaH+0.05 {
			t.Fatalf("red koopa walked off the ledge: y=%f", e.Pos.Y)
		}
		if e.Gone {
			t.Fatal("red koopa fell out of the world")
		}
	}
	if !flipped {
		t.Error("red koopa never turned at the ledge")
	}

	gg := mk(false)
	ge := gg.Enemies[len(gg.Enemies)-1]
	for i := 0; i < 900 && !ge.Gone; i++ {
		gg.Update(Input{})
	}
	if !ge.Gone {
		t.Error("green koopa did not fall into the pit")
	}
}

func TestRedParaBobsAroundBaseY(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	e := newParaRed(Vec{20, 9})
	g.Enemies = append(g.Enemies, e)
	minY, maxY := e.Pos.Y, e.Pos.Y
	x0 := e.Pos.X
	for range 1200 {
		g.Update(Input{})
		if e.Pos.Y < minY {
			minY = e.Pos.Y
		}
		if e.Pos.Y > maxY {
			maxY = e.Pos.Y
		}
	}
	if minY < 9-ParaRedRange-0.1 || maxY > 9+ParaRedRange+0.1 {
		t.Errorf("red para escaped its band: y in [%f,%f], want within %.1f±%f",
			minY, maxY, 9.0, ParaRedRange+0.1)
	}
	if minY > 9-ParaRedRange+0.1 || maxY < 9+ParaRedRange-0.1 {
		t.Errorf("red para did not use its full band: y in [%f,%f]", minY, maxY)
	}
	if e.Pos.X != x0 {
		t.Errorf("red para drifted horizontally: %f -> %f", x0, e.Pos.X)
	}
}

func TestRedParaStompDemotesToRedKoopa(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	e := newParaRed(Vec{20, 9})
	g.Enemies = append(g.Enemies, e)
	dropPlayer(g, 20, 5)
	for range 120 {
		if e.Kind != KindPara {
			break
		}
		g.Update(Input{})
	}
	if e.Kind != KindKoopa {
		t.Fatalf("red para kind = %v, want demoted koopa", e.Kind)
	}
	if !e.Red {
		t.Error("demoted red para lost its Red flag")
	}
}

func TestUnderwaterContactNeverStomps(t *testing.T) {
	g := newGame(t, buildLevel(t, 60, func(b *Builder) { b.Set(20, 12, 'G') }))
	g.Level.Underwater = true
	e := g.Enemies[0]
	// Feet inside the goomba's box while falling: on land a stomp, in
	// water a hit. Direct overlap keeps the test independent of the
	// slow swim fall.
	dropPlayer(g, 20, 11.4)
	g.Player.Vel.Y = 0.2
	run(g, 10, Input{})
	if g.State != StateDying {
		t.Fatalf("state = %v, want dying (underwater contact always hurts)", g.State)
	}
	if e.State != EnemyWalking {
		t.Errorf("goomba was stomped underwater: state=%v", e.State)
	}
}

func TestUnderwaterStarStillKills(t *testing.T) {
	g := newGame(t, buildLevel(t, 60, func(b *Builder) { b.Set(20, 12, 'G') }))
	g.Level.Underwater = true
	e := g.Enemies[0]
	g.Player.Star = 600
	g.Player.Pos = Vec{19.2, 12}
	run(g, 5, Input{Right: true})
	if e.State != EnemyFlipped {
		t.Errorf("star did not kill underwater: state=%v", e.State)
	}
	if g.State != StatePlaying {
		t.Errorf("state = %v, want playing", g.State)
	}
}

func TestHammerBroPatrolsAndThrows(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	b := newHammerBro(Vec{20, GroundTop - HammerBroH})
	g.HammerBros = append(g.HammerBros, b)
	minX, maxX := b.Pos.X, b.Pos.X
	for range 400 {
		g.updateHammerBros()
		g.updateHammers()
		if b.Pos.X < minX {
			minX = b.Pos.X
		}
		if b.Pos.X > maxX {
			maxX = b.Pos.X
		}
	}
	if minX < 20-HammerBroPatrol-0.01 || maxX > 20+HammerBroPatrol+0.01 {
		t.Errorf("hammer bro escaped his patrol: x in [%f,%f]", minX, maxX)
	}
	if len(g.Hammers) == 0 {
		t.Error("hammer bro never threw a hammer")
	}
	for _, h := range g.Hammers {
		if h.Vel.X == 0 && h.Vel.Y == 0 {
			t.Error("hammer spawned without velocity")
		}
	}
}

func TestHammerBroStompKill(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	b := newHammerBro(Vec{20, GroundTop - HammerBroH})
	g.HammerBros = append(g.HammerBros, b)
	dropPlayer(g, 20, GroundTop-HammerBroH-0.8) // feet inside the bro's stomp window
	g.Player.Vel.Y = 0.1
	for range 3 {
		g.updateHammerBros()
	}
	if b.State != broDead {
		t.Fatalf("hammer bro state = %v, want dead", b.State)
	}
	if g.Score != HammerBroScore {
		t.Errorf("score = %d, want %d", g.Score, HammerBroScore)
	}
	if g.Player.Vel.Y >= 0 {
		t.Error("player did not bounce off the stomp")
	}
}

func TestShellKillsHammerBro(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	b := newHammerBro(Vec{20, GroundTop - HammerBroH})
	g.HammerBros = append(g.HammerBros, b)
	sh := newKoopa(Vec{18, GroundTop - KoopaH})
	sh.State = EnemyShellMoving
	sh.Dir = 1
	g.Enemies = append(g.Enemies, sh)
	for i := 0; i < 20 && b.State != broDead; i++ {
		g.updateEnemies() // the shell must actually slide into him
		g.updateHammerBros()
	}
	if b.State != broDead {
		t.Fatalf("hammer bro state = %v, want dead by shell", b.State)
	}
	if g.Score != HammerBroScore {
		t.Errorf("score = %d, want %d", g.Score, HammerBroScore)
	}
}

func TestHammerHurtsAndStarSwats(t *testing.T) {
	// Plain contact hurts: a small player dies.
	g := newGame(t, buildLevel(t, 60))
	g.Hammers = append(g.Hammers, &Hammer{Pos: Vec{g.Player.Pos.X, 12}, Vel: Vec{HammerThrowVelX, HammerThrowVelY}})
	for range 2 {
		g.updateHammers()
	}
	if g.State != StateDying {
		t.Fatalf("state = %v, want dying (hammer contact)", g.State)
	}

	// Star power swats it for the flat 100 instead.
	g = newGame(t, buildLevel(t, 60))
	g.Player.Star = 600
	h := &Hammer{Pos: Vec{g.Player.Pos.X, 12}, Vel: Vec{HammerThrowVelX, HammerThrowVelY}}
	g.Hammers = append(g.Hammers, h)
	for range 2 {
		g.updateHammers()
	}
	if !h.Gone {
		t.Error("star did not swat the hammer")
	}
	if g.Score != HammerScore {
		t.Errorf("score = %d, want %d", g.Score, HammerScore)
	}
}
