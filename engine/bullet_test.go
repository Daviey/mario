package engine

import "testing"

func blasterLevel(t *testing.T) *Game {
	t.Helper()
	l := buildLevel(t, 60, func(b *Builder) {
		b.Fill(20, 12, 21, 12, 'N')
	})
	return newGame(t, l)
}

func TestBlasterFiresTowardPlayer(t *testing.T) {
	// Player to the right: shots leave rightward.
	g := blasterLevel(t)
	g.Player.Pos.X = 30
	g.Player.Vel = Vec{}
	for i := 0; i < BulletFireEvery*2 && len(g.Bullets) == 0; i++ {
		g.Update(Input{})
	}
	if len(g.Bullets) == 0 {
		t.Fatal("blaster never fired with the player in range")
	}
	if v := g.Bullets[0].Vel.X; v <= 0 {
		t.Fatalf("shot velocity X = %v, want rightward (player on the right)", v)
	}

	// Player to the left: shots leave leftward.
	g = blasterLevel(t)
	g.Player.Pos.X = 10
	g.Player.Vel = Vec{}
	for i := 0; i < BulletFireEvery*2 && len(g.Bullets) == 0; i++ {
		g.Update(Input{})
	}
	if len(g.Bullets) == 0 {
		t.Fatal("blaster never fired with the player in range")
	}
	if v := g.Bullets[0].Vel.X; v >= 0 {
		t.Fatalf("shot velocity X = %v, want leftward (player on the left)", v)
	}
}

func TestBlasterSilentWhenPlayerFar(t *testing.T) {
	g := blasterLevel(t)
	g.Player.Pos.X = 2 // the blaster at 20 is out of the 26-tile span
	g.Player.Vel = Vec{}
	run(g, BulletFireEvery*3, Input{})
	if len(g.Bullets) != 0 {
		t.Fatalf("blaster fired at a distant player: %d bullets", len(g.Bullets))
	}
}

func TestBlasterTileIsSolid(t *testing.T) {
	g := blasterLevel(t)
	g.Player.Pos = Vec{20.2, 9}
	g.Player.Vel = Vec{}
	run(g, 60, Input{})
	if !g.Player.Grounded {
		t.Fatal("player did not land on the cannon tiles")
	}
	if got := g.Player.Pos.Y + g.Player.H; got != 12 {
		t.Fatalf("feet at %v, want standing on the cannon top (12)", got)
	}
}

func TestBulletStompKillsAndBounces(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	// A hovering bill: the kill path is the contract, not the chase.
	g.Bullets = []*Bullet{{Pos: Vec{10, 12.15}, Vel: Vec{}}}
	g.Player.Pos = Vec{9.7, 11}
	g.Player.Vel = Vec{}
	score := g.Score
	for i := 0; i < 60 && len(g.Bullets) > 0; i++ {
		g.Update(Input{})
	}
	if len(g.Bullets) != 0 {
		t.Fatal("the stomp never killed the bullet")
	}
	if g.Score != score+BulletScore {
		t.Fatalf("score after stomp = %d, want +%d", g.Score-score, BulletScore)
	}
	if g.Player.Vel.Y >= 0 {
		t.Fatalf("post-stomp velocity Y = %v, want the bounce", g.Player.Vel.Y)
	}
}

func TestBulletHurtsPlayerOnContact(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	g.Bullets = []*Bullet{{Pos: Vec{13, 12.15}, Vel: Vec{-BulletSpeed, 0}}}
	g.Player.Pos = Vec{10, 12}
	g.Player.Vel = Vec{}
	run(g, 60, Input{})
	if g.State != StateDying {
		t.Fatalf("side contact state = %v, want dying", g.State)
	}
}

func TestFireballKillsBullet(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	g.Player.Power = PowerFire
	// Fireball and bill already overlapping: the burn-out is the
	// contract (the bounce makes a chase flaky, not the kill).
	g.Bullets = []*Bullet{{Pos: Vec{10, 12.15}, Vel: Vec{}}}
	g.Fireballs = []*Fireball{{Pos: Vec{10.3, 12.3}, Vel: Vec{FireballSpeed, 0}}}
	score := g.Score
	for i := 0; i < 10 && len(g.Bullets) > 0; i++ {
		g.Update(Input{})
	}
	if len(g.Bullets) != 0 {
		t.Fatal("the fireball never killed the bullet")
	}
	if g.Score != score+BulletScore {
		t.Fatalf("score = %d, want +%d for the burn-out", g.Score-score, BulletScore)
	}
}

func TestStarKillsBullet(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	g.Player.Star = StarTicks
	g.Player.Pos = Vec{10, 12}
	g.Player.Vel = Vec{}
	g.Bullets = []*Bullet{{Pos: Vec{10.1, 12.15}, Vel: Vec{}}}
	g.Update(Input{})
	if len(g.Bullets) != 0 {
		t.Fatal("star contact must kill the bullet")
	}
	if g.State == StateDying {
		t.Fatal("star power must absorb the hit")
	}
}

func TestShellMowsBullet(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	shell := kickedShell(g, 15, 1)
	g.Bullets = []*Bullet{{Pos: Vec{20, 12.15}, Vel: Vec{-BulletSpeed, 0}}}
	for i := 0; i < 120 && len(g.Bullets) > 0; i++ {
		g.Update(Input{})
	}
	if len(g.Bullets) != 0 {
		t.Fatal("the sliding shell never mowed the bullet")
	}
	if shell.Gone || shell.State != EnemyShellMoving {
		t.Fatal("the shell must survive mowing a bullet")
	}
}

func TestBulletIgnoresTerrain(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Fill(15, 12, 15, 12, 'B') })
	g := newGame(t, l)
	g.Bullets = []*Bullet{{Pos: Vec{10, 12.15}, Vel: Vec{BulletSpeed, 0}}}
	run(g, 100, Input{})
	b := g.Bullets[0]
	if b.Gone {
		t.Fatal("bullet retired mid-flight")
	}
	if b.Pos.X <= 16 {
		t.Fatalf("bullet stopped at the brick wall: X = %.2f, want past 16 (bills fly over everything)", b.Pos.X)
	}
}

func TestBulletsSwapWithTheWorld(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	g.Bullets = []*Bullet{{Pos: Vec{10, 12.15}, Vel: Vec{BulletSpeed, 0}}}
	w := g.stashWorld()
	g.Bullets = nil
	g.applyWorld(w)
	if len(g.Bullets) != 1 || g.Bullets[0].Pos.X != 10 {
		t.Fatal("bullets must travel with their world through warp swaps")
	}
}
