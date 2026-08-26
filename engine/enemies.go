package engine

import "math"

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
				// Flying koopas hop as they walk: charge on the ground, leap.
				if e.Kind == KindPara && e.State == EnemyWalking {
					e.Timer++
					if e.Timer >= ParaHopEvery {
						e.Timer = 0
						e.Vel.Y = ParaHopVel
					}
				}
			}
			if e.Pos.Y > bottom {
				e.Gone = true
			}
			if vx != 0 {
				e.WalkDist += math.Abs(vx)
			}
		}
	}

	// Moving shells mow down every other enemy they touch, climbing the
	// combo ladder with each consecutive kill.
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
				g.awardShell(b, a.Chain)
				a.Chain++
				g.flipEnemy(b)
			}
		}
	}
}

// flipEnemy knocks an enemy onto its back (killed by a block bump, shell
// or fireball). Scoring belongs to the caller.
func (g *Game) flipEnemy(e *Enemy) {
	if e.State == EnemySquashed || e.State == EnemyFlipped {
		return
	}
	e.State = EnemyFlipped
	e.Vel = Vec{0, -0.28}
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
		if p.Star > 0 {
			// Star power: any contact flips the enemy, no questions asked.
			g.flipEnemy(e)
			g.Score += StompScore
			g.spawnScorePop(e.Pos.X, e.Pos.Y, StompScore, false)
			g.emit("stomp")
			continue
		}
		stomping := p.Vel.Y > 0.02 && (p.Pos.Y+p.H) < (e.Pos.Y+e.H*0.7)
		switch {
		case stomping && e.State == EnemyWalking:
			switch e.Kind {
			case KindGoomba:
				e.State = EnemySquashed
				e.Timer = 30
			case KindPara:
				// Wings clipped: the paratroopa demotes to a walking
				// koopa (a second stomp makes the shell, as usual).
				e.Kind = KindKoopa
			default:
				e.State = EnemyShell
				e.Pos.Y += e.H - GoombaH
				e.H = GoombaH
				e.Vel.X = 0
			}
			g.awardStomp(e.Pos.X, e.Pos.Y)
			g.bouncePlayer()
			g.emit("stomp")
		case stomping && e.State == EnemyShell:
			g.kickShell(e)
			g.bouncePlayer()
		case stomping && e.State == EnemyShellMoving:
			e.State = EnemyShell
			e.Vel.X = 0
			e.Chain = 0
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
	e.Chain = 0
	d := (e.Pos.X + e.W/2) - (g.Player.Pos.X + g.Player.W/2)
	switch {
	case d > 0.1:
		e.Dir = 1
	case d < -0.1:
		e.Dir = -1
	}
	g.emit("kick")
}
