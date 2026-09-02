package engine

import "math"

// updateEnemies advances every enemy one tick and resolves shell kills.
// Red-koopa ledge probing and buzzy fireball absorption run here too
// (enemyGrounded / absorbFireballsOnBuzzy below).
func (g *Game) updateEnemies() {
	g.absorbFireballsOnBuzzy()
	bottom := float64(g.Level.Height) + 2
	shellSliding := false // set when any live enemy is a sliding shell
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
			// Already dead: falls at 1.4x gravity, deliberately
			// unclamped so the corpse accelerates out of the world.
			e.Vel.Y += Gravity * 1.4
			e.Pos.X += e.Vel.X
			e.Pos.Y += e.Vel.Y
			if e.Pos.Y > bottom {
				e.Gone = true
			}
		default:
			// Red paratroopas fly in place: no gravity, no walking,
			// just the vertical bob around BaseY. Timer carries the
			// flight direction sign (it is the hop charge it
			// replaces), reversed at the BaseY±ParaRedRange ends.
			if e.Kind == KindPara && e.Red {
				if e.Pos.Y <= e.BaseY-ParaRedRange {
					e.Timer = 1
				}
				if e.Pos.Y >= e.BaseY+ParaRedRange {
					e.Timer = -1
				}
				e.Pos.Y += float64(e.Timer) * ParaRedFlyVel
				e.WalkDist += ParaRedFlyVel
				continue
			}
			var vx float64
			switch e.State {
			case EnemyWalking:
				vx = float64(e.Dir) * EnemyWalk
				// A red koopa never walks off a ledge: grounded,
				// it probes the ground ahead of its leading foot
				// and turns before the drop (green koopas march
				// straight off).
				if e.Red && g.enemyGrounded(e) {
					ax := e.Pos.X + e.W/2 + float64(e.Dir)*(e.W/2+0.3)
					ty := int(math.Floor(e.Pos.Y + e.H + 0.5))
					if !g.solidAt(int(math.Floor(ax)), ty) {
						e.Dir = -e.Dir
					}
				}
			case EnemyShellMoving:
				vx = float64(e.Dir) * ShellSpeed
				shellSliding = true
			}
			e.Vel.Y = applyGravity(e.Vel.Y, Gravity)
			if vx != 0 && g.moveX(&e.Pos, e.W, e.H, vx) && !g.shellSmashBricks(e, vx) {
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
	// combo ladder with each consecutive kill. Skipped outright on ticks
	// with no sliding shell: the outer loop would visit no acting shell,
	// so the sweep is dead weight — identical visits and outcomes.
	if !shellSliding {
		return
	}
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
				g.awardLadder(a.Chain, b.Pos.X, b.Pos.Y)
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
	e.Vel = Vec{0, FlipVel}
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
		// Underwater there is no stomping (S6): star power still kills,
		// every other contact just hurts.
		if g.Level.Underwater {
			g.hurtPlayer()
			continue
		}
		stomping := p.Vel.Y > 0.02 && (p.Pos.Y+p.H) < (e.Pos.Y+e.H*StompBodyFrac)
		switch {
		case stomping && e.State == EnemyWalking:
			switch e.Kind {
			case KindGoomba:
				e.State = EnemySquashed
				e.Timer = SquashHoldTicks
			case KindPara:
				// Wings clipped: the paratroopa demotes to a walking
				// koopa (a second stomp makes the shell, as usual).
				e.Kind = KindKoopa
			default:
				// Koopa and buzzy beetle: the shell keeps the feet
				// planted (the shell box is goomba-sized).
				e.State = EnemyShell
				e.Pos.Y += e.H - GoombaH
				e.H = GoombaH
				e.Vel.X = 0
			}
			g.awardLadder(g.Player.stompChain, e.Pos.X, e.Pos.Y)
			g.Player.stompChain++
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

// shellSmashBricks resolves a sliding shell's wall hit, SMB1 style:
// plain bricks in the shell's body span shatter — paying the brick
// score, throwing debris and flipping enemies standing on top, the
// sideways twin of the super player's hitBlock path — and the shell
// keeps sliding. Any other solid tile reverses it as before (a mixed
// wall breaks its bricks, then reverses off the remainder on the next
// tick). Call with e.Pos already pinned flush against the wall by
// moveX.
func (g *Game) shellSmashBricks(e *Enemy, vx float64) bool {
	if e.State != EnemyShellMoving {
		return false
	}
	var col int
	if vx > 0 {
		col = int(math.Floor(e.Pos.X + e.W + 0.05))
	} else {
		col = int(math.Floor(e.Pos.X - 0.05))
	}
	y0 := int(math.Floor(e.Pos.Y + 0.05))
	y1 := int(math.Floor(e.Pos.Y + e.H - 0.05))
	broke := false
	for ty := y0; ty <= y1; ty++ {
		t := g.Level.At(col, ty)
		if t != Brick && t != BrickCoin {
			continue
		}
		g.Level.Set(col, ty, Empty)
		if t == BrickCoin {
			// The smash must also drop the live entry, or the decay
			// loop later materializes a solid Used block in mid-air
			// where the smashed brick stood (ghost-block regression).
			delete(g.coinBricks, ty*g.Level.Width+col)
		}
		g.Score += BrickScore
		g.spawnDebris(col, ty)
		g.emit("brick")
		g.flipEnemiesOnBlock(col, ty)
		broke = true
	}
	return broke
}

// kickShell sends an idle shell sliding away from the player.
func (g *Game) kickShell(e *Enemy) {
	e.State = EnemyShellMoving
	e.Chain = 0
	d := (e.Pos.X + e.W/2) - (g.Player.Pos.X + g.Player.W/2)
	// Inside the deadzone the kick leaves Dir alone: the shell keeps
	// sliding the way it last faced instead of teleporting away.
	switch {
	case d > KickDeadzone:
		e.Dir = 1
	case d < -KickDeadzone:
		e.Dir = -1
	}
	g.emit("kick")
}

// enemyGrounded reports whether any solid tile sits directly under an
// enemy's feet span — the red koopa's ledge probe needs it.
func (g *Game) enemyGrounded(e *Enemy) bool {
	ty := int(math.Floor(e.Pos.Y + e.H + skin))
	x0 := int(math.Floor(e.Pos.X + skin))
	x1 := int(math.Floor(e.Pos.X + e.W - skin))
	for tx := x0; tx <= x1; tx++ {
		if g.solidAt(tx, ty) {
			return true
		}
	}
	return false
}

// absorbFireballsOnBuzzy kills any fireball touching a live buzzy
// beetle — the shell is fireproof in every state, so the fireball dies
// with no effect and no score (S7). The fireball-vs-enemy loop lives in
// items.go, outside this worker's files, so the absorption runs here one
// step earlier in the tick: updateEnemies precedes updateFireballs, and
// an absorbed fireball is already Gone before that loop could pay the
// kill. (A later guard in items.go would be redundant, not conflicting.)
func (g *Game) absorbFireballsOnBuzzy() {
	for _, e := range g.Enemies {
		if e.Gone || e.Kind != KindBuzzy || e.State == EnemySquashed || e.State == EnemyFlipped {
			continue
		}
		for _, fb := range g.Fireballs {
			if fb.Gone {
				continue
			}
			if overlap(fb.Pos.X, fb.Pos.Y, FireballW, FireballH, e.Pos.X, e.Pos.Y, e.W, e.H) {
				fb.Gone = true
			}
		}
	}
}
