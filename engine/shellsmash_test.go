package engine

import "testing"

// SMB1 fidelity: a kicked shell sliding into a plain brick smashes it
// and keeps going (the shell-bowling fantasy — 3-2's gauntlet is built
// around it). Solid anything-else still reverses the shell, and the
// smash is the super player's hitBlock brick path sideways: brick
// score, debris, the brick event, and enemies standing on the broken
// brick flip.

// kickedShell drops a koopa already slid into its shell at x, heading
// dir, standing on the ground like the mow-down fixture.
func kickedShell(g *Game, x float64, dir int) *Enemy {
	e := newKoopa(Vec{x, 13 - GoombaH})
	e.State = EnemyShellMoving
	e.Dir = dir
	e.H = GoombaH
	g.Enemies = append(g.Enemies, e)
	return e
}

func TestShellSmashesBricksAndContinues(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) {
		b.Set(20, 12, 'B')
		b.Set(22, 12, 'B')
	})
	g := newGame(t, l)
	shell := kickedShell(g, 15, 1)
	heardBrick, sawDebris := false, false
	for t := 0; t < 300 && shell.Pos.X < 25; t++ {
		g.Update(Input{})
		if eventsContain(g.Events, "brick") {
			heardBrick = true
		}
		if len(g.Particles) > 0 {
			sawDebris = true
		}
	}
	if shell.Pos.X < 25 {
		t.Fatalf("shell never cleared the rubble: x=%.2f", shell.Pos.X)
	}
	for _, tx := range []int{20, 22} {
		if got := g.Level.At(tx, 12); got != Empty {
			t.Errorf("brick at (%d,12) = %v, want smashed to Empty", tx, got)
		}
	}
	if shell.Dir != 1 {
		t.Errorf("shell reversed (dir=%d) after smashing through", shell.Dir)
	}
	if g.Score != 2*BrickScore {
		t.Errorf("score = %d, want %d (two bricks)", g.Score, 2*BrickScore)
	}
	if !heardBrick {
		t.Error("no brick event emitted")
	}
	if !sawDebris {
		t.Error("no debris particles spawned")
	}
}

func TestShellReversesOffSolidWall(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) {
		b.Pipe(20, 2)
	})
	g := newGame(t, l)
	shell := kickedShell(g, 15, 1)
	for range 200 {
		g.Update(Input{})
		if shell.Dir == -1 {
			break
		}
	}
	if shell.Dir != -1 {
		t.Fatal("shell never reversed off the pipe")
	}
	if got := g.Level.At(20, 12); got == Empty {
		t.Error("pipe tile destroyed by a shell; only bricks smash")
	}
}

func TestShellSmashFlipsEnemiesOnBrick(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) {
		b.Set(20, 12, 'B')
	})
	g := newGame(t, l)
	victim := newGoomba(Vec{20.05, 12 - GoombaH})
	g.Enemies = append(g.Enemies, victim)
	shell := kickedShell(g, 15, 1)
	for t := 0; t < 300 && shell.Pos.X < 22; t++ {
		g.Update(Input{})
	}
	if got := g.Level.At(20, 12); got != Empty {
		t.Fatalf("brick = %v, want smashed", got)
	}
	if victim.State != EnemyFlipped {
		t.Errorf("enemy on the smashed brick = %v, want flipped", victim.State)
	}
	if g.Score != BrickScore+StompScore {
		t.Errorf("score = %d, want %d (brick + flip)", g.Score, BrickScore+StompScore)
	}
}
