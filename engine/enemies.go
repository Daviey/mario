package engine

// updateEnemies advances every enemy one tick and resolves shell kills.
func (g *Game) updateEnemies() {
	bottom := float64(g.Level.Height) + 2
	for _, e := range g.Enemies {
		if e.Gone {
			continue
		}
		switch e.State {
		case EnemySquashed:
			e.Timer--
			if e.Timer <= 0 {
				e.Gone = true
			}
		case EnemyFlipped:
			e.Vel.Y += Gravity * 1.4
			e.Pos.X += e.Vel.X
			e.Pos.Y += e.Vel.Y
			if e.Pos.Y > bottom {
				e.Gone = true
			}
		default:
			var vx float64
			switch e.State {
			case EnemyWalking:
				vx = float64(e.Dir) * EnemyWalk
			case EnemyShellMoving:
				vx = float64(e.Dir) * ShellSpeed
			}
			e.Vel.Y += Gravity
			if e.Vel.Y > MaxFall {
				e.Vel.Y = MaxFall
			}
			if vx != 0 && g.moveX(&e.Pos, e.W, e.H, vx) {
				e.Dir = -e.Dir
			}
			landed, _, _ := g.moveY(&e.Pos, e.W, e.H, e.Vel.Y)
			if landed {
				e.Vel.Y = 0
			}
			if e.Pos.Y > bottom {
				e.Gone = true
			}
		}
	}

	// Moving shells mow down every other enemy they touch.
	for i := 0; i < len(g.Enemies); i++ {
		a := g.Enemies[i]
		if a.Gone || a.State != EnemyShellMoving {
			continue
		}
		for j := 0; j < len(g.Enemies); j++ {
			if i == j {
				continue
			}
			b := g.Enemies[j]
			if b.Gone || b.State == EnemySquashed || b.State == EnemyFlipped {
				continue
			}
			if overlap(a.Pos.X, a.Pos.Y, a.W, a.H, b.Pos.X, b.Pos.Y, b.W, b.H) {
				g.flipEnemy(b)
			}
		}
	}
}

// flipEnemy knocks an enemy onto its back (killed by a block bump or shell).
func (g *Game) flipEnemy(e *Enemy) {
	if e.State == EnemySquashed || e.State == EnemyFlipped {
		return
	}
	e.State = EnemyFlipped
	e.Vel = Vec{0, -0.28}
	g.Score += StompScore
}

// playerEnemyInteractions resolves stomps, kicks and damage.
func (g *Game) playerEnemyInteractions() {
	p := g.Player
	for _, e := range g.Enemies {
		if e.Gone || e.State == EnemySquashed || e.State == EnemyFlipped {
			continue
		}
		if !overlap(p.Pos.X, p.Pos.Y, p.W, p.H, e.Pos.X, e.Pos.Y, e.W, e.H) {
			continue
		}
		stomping := p.Vel.Y > 0.02 && (p.Pos.Y+p.H) < (e.Pos.Y+e.H*0.7)
		switch {
		case stomping && e.State == EnemyWalking:
			if e.Kind == KindGoomba {
				e.State = EnemySquashed
				e.Timer = 30
			} else {
				e.State = EnemyShell
				e.Pos.Y += e.H - GoombaH
				e.H = GoombaH
				e.Vel.X = 0
			}
			g.Score += StompScore
			g.bouncePlayer()
		case stomping && e.State == EnemyShell:
			g.kickShell(e)
			g.bouncePlayer()
		case stomping && e.State == EnemyShellMoving:
			e.State = EnemyShell
			e.Vel.X = 0
			g.bouncePlayer()
		case e.State == EnemyShell:
			g.kickShell(e)
		default:
			g.hurtPlayer()
		}
	}
}

// bouncePlayer applies the post-stomp bounce; holding jump bounces higher.
func (g *Game) bouncePlayer() {
	p := g.Player
	p.Vel.Y = StompBounce
	if g.curIn.Up {
		p.Vel.Y = JumpVel
	}
}

// kickShell sends an idle shell sliding away from the player.
func (g *Game) kickShell(e *Enemy) {
	e.State = EnemyShellMoving
	d := (e.Pos.X + e.W/2) - (g.Player.Pos.X + g.Player.W/2)
	switch {
	case d > 0.01:
		e.Dir = 1
	case d < -0.01:
		e.Dir = -1
	default:
		e.Dir = g.Player.Facing
	}
}
