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
	case dir != 0:
		p.Vel.X += float64(dir) * accel
		p.Facing = dir
	case p.Grounded:
		if p.Vel.X > Friction {
			p.Vel.X -= Friction
		} else if p.Vel.X < -Friction {
			p.Vel.X += Friction
		} else {
			p.Vel.X = 0
		}
	}
	// Turning against ground motion reads as a skid to the renderer.
	p.Skidding = p.Grounded && dir != 0 &&
		((dir > 0 && p.Vel.X < -0.01) || (dir < 0 && p.Vel.X > 0.01))
	if p.Vel.X > maxV {
		p.Vel.X = maxV
	}
	if p.Vel.X < -maxV {
		p.Vel.X = -maxV
	}
	// Leg cycle advances with distance covered on the ground.
	if p.Grounded {
		p.WalkDist += math.Abs(p.Vel.X)
	}
	// Skids kick up dust behind the direction of travel.
	if p.Skidding && g.Tick%4 == 0 {
		g.spawnDustPuff(p.Pos.X+p.W/2+float64(p.Facing)*0.3, p.Pos.Y+p.H-0.05)
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
		p.StretchT = 6
		g.spawnDustPuff(p.Pos.X+p.W/2-0.15, p.Pos.Y+p.H)
		g.spawnDustPuff(p.Pos.X+p.W/2+0.15, p.Pos.Y+p.H)
		g.emit("jump")
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
	if p.Star > 0 {
		p.Star--
	}
	if p.StretchT > 0 {
		p.StretchT--
	}
	if p.SquashT > 0 {
		p.SquashT--
	}

	// Gravity and integration.
	rising := p.Vel.Y < -0.01
	p.Vel.Y += Gravity
	if g.moveX(&p.Pos, p.W, p.H, p.Vel.X) {
		p.Vel.X = 0
	}
	landed, ceilTy, ceilCols := g.moveY(&p.Pos, p.W, p.H, p.Vel.Y)
	p.Grounded = landed
	if landed {
		fall := p.Vel.Y
		p.Vel.Y = 0
		p.stompChain = 0 // the stomp combo ends when feet touch ground
		if fall > 0.12 { // hard landing: squash pose and twin dust puffs
			p.SquashT = 6
			g.spawnDustPuff(p.Pos.X+p.W/2-0.2, p.Pos.Y+p.H)
			g.spawnDustPuff(p.Pos.X+p.W/2+0.2, p.Pos.Y+p.H)
		}
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
	if rising {
		g.bumpHidden(p)
	}
}

// bumpHidden resolves a rising head-bump against invisible blocks: hidden
// blocks are not solid — bodies pass through them every other way — but a
// head rising into their cell from below triggers them, classic style.
func (g *Game) bumpHidden(p *Player) {
	headRow := int(math.Floor(p.Pos.Y + skin))
	x0 := int(math.Floor(p.Pos.X + skin))
	x1 := int(math.Floor(p.Pos.X + p.W - skin))
	for tx := x0; tx <= x1; tx++ {
		t := g.Level.At(tx, headRow)
		if t != HiddenCoin && t != HiddenLife {
			continue
		}
		if horizontalOverlap(p.Pos.X, p.W, float64(tx)) < 0.15 {
			continue
		}
		p.Pos.Y = float64(headRow) + 1
		p.Vel.Y = 0
		g.hitBlock(tx, headRow, p)
		return
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
		g.emit("bump")
	case QuestionMush:
		g.Level.Set(tx, ty, Used)
		g.bumps[idx] = 8
		g.Mushrooms = append(g.Mushrooms, &Mushroom{
			Pos:    Vec{float64(tx) + 0.05, float64(ty) - 0.05},
			Dir:    1,
			Emerge: MushroomEmergeTicks,
		})
		g.emit("bump")
	case QuestionFire:
		g.Level.Set(tx, ty, Used)
		g.bumps[idx] = 8
		// SMB rule: a small player gets a mushroom, a powered one the flower.
		if p.Power == PowerSmall {
			g.Mushrooms = append(g.Mushrooms, &Mushroom{
				Pos:    Vec{float64(tx) + 0.05, float64(ty) - 0.05},
				Dir:    1,
				Emerge: MushroomEmergeTicks,
			})
		} else {
			g.FireFlowers = append(g.FireFlowers, &FireFlower{
				Pos:    Vec{float64(tx) + 0.05, float64(ty) - 0.05},
				Emerge: FlowerEmergeTicks,
			})
		}
		g.emit("bump")
	case QuestionStar:
		g.Level.Set(tx, ty, Used)
		g.bumps[idx] = 8
		g.Mushrooms = append(g.Mushrooms, &Mushroom{
			Pos:    Vec{float64(tx) + 0.05, float64(ty) - 0.05},
			Dir:    1,
			Emerge: MushroomEmergeTicks,
			Kind:   MushStar,
		})
		g.emit("bump")
	case HiddenCoin:
		g.Level.Set(tx, ty, Used)
		g.bumps[idx] = 8
		g.addCoin()
		g.spawnCoinPop(float64(tx)+0.35, float64(ty))
		g.emit("bump")
	case HiddenLife:
		g.Level.Set(tx, ty, Used)
		g.bumps[idx] = 8
		g.Mushrooms = append(g.Mushrooms, &Mushroom{
			Pos:    Vec{float64(tx) + 0.05, float64(ty) - 0.05},
			Dir:    1,
			Emerge: MushroomEmergeTicks,
			Kind:   MushLife,
		})
		g.emit("bump")
	case Brick:
		if p.Power >= PowerSuper {
			g.Level.Set(tx, ty, Empty)
			g.Score += BrickScore
			g.spawnDebris(tx, ty)
			g.emit("brick")
		} else {
			g.bumps[idx] = 8
			g.emit("bump")
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
			g.Score += StompScore
			g.spawnScorePop(e.Pos.X, e.Pos.Y, StompScore, false)
		}
	}
}

// hurtPlayer applies enemy contact damage: fire and super shrink to small,
// small dies. Star power absorbs everything.
func (g *Game) hurtPlayer() {
	p := g.Player
	if p.Invincible > 0 || p.Star > 0 {
		return
	}
	if p.Power >= PowerSuper {
		p.shrink()
		p.Invincible = InvincibleTicks
	} else {
		g.kill()
	}
}
