package engine

import "math"

// updatePlayer applies one tick of player control, physics and collision.
func (g *Game) updatePlayer(in Input) {
	p := g.Player

	// Horizontal control.
	accel, maxV := WalkAccel, MaxWalk
	if in.Run {
		accel, maxV = RunAccel, MaxRun
	}
	if !p.Grounded {
		accel *= AirFactor
	}
	dir := 0
	if in.Left {
		dir--
	}
	if in.Right {
		dir++
	}
	switch {
	case dir > 0:
		p.Vel.X += accel
		p.Facing = 1
	case dir < 0:
		p.Vel.X -= accel
		p.Facing = -1
	default:
		if p.Grounded {
			switch {
			case p.Vel.X > Friction:
				p.Vel.X -= Friction
			case p.Vel.X < -Friction:
				p.Vel.X += Friction
			default:
				p.Vel.X = 0
			}
		}
	}
	if p.Vel.X > maxV {
		p.Vel.X = maxV
	}
	if p.Vel.X < -maxV {
		p.Vel.X = -maxV
	}

	// Jumping: edge detection, jump buffer and coyote time.
	jumpPressed := in.Up && !g.prevIn.Up
	jumpReleased := !in.Up && g.prevIn.Up
	if jumpPressed {
		p.jumpBuffer = JumpBufferTicks
	} else if p.jumpBuffer > 0 {
		p.jumpBuffer--
	}
	if p.Grounded {
		p.groundTimer = 0
	} else {
		p.groundTimer++
	}
	if p.jumpBuffer > 0 && p.groundTimer <= CoyoteTicks {
		p.Vel.Y = JumpVel
		p.jumping = true
		p.Grounded = false
		p.jumpBuffer = 0
		p.groundTimer = CoyoteTicks + 1 // consume coyote so the jump cannot re-fire
	}
	if jumpReleased && p.jumping && p.Vel.Y < JumpCut {
		p.Vel.Y = JumpCut
	}
	if p.Vel.Y > 0 {
		p.jumping = false
	}
	if p.Invincible > 0 {
		p.Invincible--
	}

	// Gravity and integration.
	p.Vel.Y += Gravity
	if p.Vel.Y > MaxFall {
		p.Vel.Y = MaxFall
	}
	if g.moveX(&p.Pos, p.W, p.H, p.Vel.X) {
		p.Vel.X = 0
	}
	landed, ceilTy, ceilCols := g.moveY(&p.Pos, p.W, p.H, p.Vel.Y)
	p.Grounded = landed
	if landed {
		p.Vel.Y = 0
	} else if ceilTy >= 0 && len(ceilCols) > 0 {
		p.Vel.Y = 0
		best, bestOv := -1, -1.0
		for _, tx := range ceilCols {
			if ov := horizontalOverlap(p.Pos.X, p.W, float64(tx)); ov > bestOv {
				bestOv, best = ov, tx
			}
		}
		if best >= 0 {
			g.hitBlock(best, ceilTy, p)
		}
	}
}

// hitBlock resolves a player head-bump against the tile at (tx, ty).
func (g *Game) hitBlock(tx, ty int, p *Player) {
	idx := ty*g.Level.Width + tx
	switch g.Level.At(tx, ty) {
	case Question:
		g.Level.Set(tx, ty, Used)
		g.bumps[idx] = 8
		g.addCoin()
		g.spawnCoinPop(float64(tx)+0.35, float64(ty))
	case QuestionMush:
		g.Level.Set(tx, ty, Used)
		g.bumps[idx] = 8
		g.Mushrooms = append(g.Mushrooms, &Mushroom{
			Pos:    Vec{float64(tx) + 0.05, float64(ty) - 0.05},
			Dir:    1,
			Emerge: MushroomEmergeTicks,
		})
	case Brick:
		if p.Super {
			g.Level.Set(tx, ty, Empty)
			g.Score += BrickScore
			g.spawnDebris(tx, ty)
		} else {
			g.bumps[idx] = 8
		}
	default:
		return
	}
	// Enemies standing on the bumped block get flipped.
	for _, e := range g.Enemies {
		if e.Gone || e.State == EnemySquashed || e.State == EnemyFlipped {
			continue
		}
		if math.Abs((e.Pos.Y+e.H)-float64(ty)) < 0.2 && horizontalOverlap(e.Pos.X, e.W, float64(tx)) > 0 {
			g.flipEnemy(e)
		}
	}
}

// hurtPlayer applies enemy contact damage: shrink when super, die when small.
func (g *Game) hurtPlayer() {
	p := g.Player
	if p.Invincible > 0 {
		return
	}
	if p.Super {
		p.shrink()
		p.Invincible = InvincibleTicks
	} else {
		g.kill()
	}
}
