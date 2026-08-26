package engine

import "testing"

// jumpBump lifts the standing player at x into the block above.
func jumpBump(t *testing.T, g *Game, x float64) {
	t.Helper()
	g.Update(Input{}) // release any held jump so the next press is an edge
	g.Player.Pos = Vec{x, 13 - SmallH}
	g.Player.Vel = Vec{}
	for range 60 {
		g.Update(Input{Up: true})
		if g.State != StatePlaying {
			t.Fatalf("state = %v during jump", g.State)
		}
	}
}

func TestStarBlockGrantsStarPower(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Set(10, 9, 'S') })
	g := newGame(t, l)
	jumpBump(t, g, 10.1)
	if g.Level.At(10, 9) != Used {
		t.Fatal("star block was not bumped")
	}
	// The star emerges and bounces away; chase it down.
	for range 400 {
		g.Update(Input{Right: true, Run: true})
		if g.Player.Star > 0 {
			break
		}
	}
	if g.Player.Star <= 0 {
		t.Fatalf("star never collected (star=%d, pos=%v)", g.Player.Star, g.Player.Pos)
	}
	if g.Score < StarScore {
		t.Errorf("star collection score = %d, want >= %d", g.Score, StarScore)
	}
	if g.Player.Star > StarTicks {
		t.Errorf("star ticks = %d, want <= %d", g.Player.Star, StarTicks)
	}
}

func TestStarKillsOnTouchAndProtects(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Set(20, 12, 'G') })
	g := newGame(t, l)
	g.Player.Star = StarTicks
	g.Player.Pos = Vec{19.6, 13 - SmallH}
	g.Player.grow()
	for range 10 {
		g.Update(Input{Right: true})
	}
	e := g.Enemies[0]
	if e.State != EnemyFlipped {
		t.Errorf("starred contact: enemy state = %v, want flipped", e.State)
	}
	if g.State != StatePlaying || g.Lives != StartLives {
		t.Errorf("starred player took damage: state=%v lives=%d", g.State, g.Lives)
	}
	if g.Player.Power != PowerSuper {
		t.Errorf("starred player shrank: power=%v", g.Player.Power)
	}
}

func TestStarEatsPlantsAndFireBars(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) {
		b.Pipe(20, 3)
		b.Plant(20, 3)
		b.Fill(40, 11, 40, 12, 'B')
		b.Set(40, 10, 'h')
	})
	g := newGame(t, l)
	g.Player.Star = StarTicks

	// Plant: wait it out at a distance until it rises, then walk in.
	g.Player.Pos = Vec{27, 13 - SmallH}
	for range 220 {
		g.Update(Input{})
	}
	if g.Plants[0].State == PlantHidden {
		t.Fatal("plant never rose while the player stood away")
	}
	g.Player.Pos = Vec{20.9, 9.4} // drop onto the risen plant at pipe-top height
	gone := false
	for range 30 {
		g.Update(Input{})
		if len(g.Plants) == 0 || g.Plants[0].Gone {
			gone = true
			break
		}
	}
	if !gone {
		t.Fatal("starred contact did not kill the plant")
	}

	// Fire bar: stand in the sweep; star power must absorb it.
	g.Player.Star = StarTicks
	g.Player.Pos = Vec{42.3, 13 - SuperH}
	for range 600 {
		g.Update(Input{})
	}
	if g.State != StatePlaying {
		t.Errorf("starred player died to the fire bar: state=%v", g.State)
	}
}

func TestStarExpires(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	g.Player.Star = 1
	g.Update(Input{})
	if g.Player.Star != 0 {
		t.Errorf("star = %d after one tick, want 0", g.Player.Star)
	}
}

func TestStarWalksFastAndBounces(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	g.Mushrooms = append(g.Mushrooms, &Mushroom{Pos: Vec{20, 13 - MushroomH}, Dir: 1, Kind: MushStar})
	bounced := false
	for range 120 {
		g.Update(Input{})
		m := g.Mushrooms[0]
		if m.Vel.Y < 0 || m.Pos.Y < 13-MushroomH-0.05 {
			bounced = true
		}
		if m.Pos.X < 20 {
			t.Fatalf("star drifted backwards: %v", m.Pos)
		}
		if m.Gone {
			t.Fatal("star vanished without being collected")
		}
	}
	if !bounced {
		t.Error("star never bounced off the ground")
	}
}

func TestHiddenBlocksBumpOnlyFromBelow(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) {
		b.Set(10, 9, 'H')
		b.Set(20, 9, '1')
	})
	g := newGame(t, l)

	// Falling onto a hidden block passes straight through.
	g.Player.Pos = Vec{10.1, 3}
	for range 200 {
		g.Update(Input{})
	}
	if g.Level.At(10, 9) != HiddenCoin {
		t.Fatal("hidden coin block triggered from above")
	}
	if !g.Player.Grounded || g.Player.Pos.Y+SmallH < 12.9 {
		t.Fatalf("player did not fall through: grounded=%v y=%v", g.Player.Grounded, g.Player.Pos.Y)
	}

	// Rising into it pays the coin and leaves a used block.
	g.CoinCount = 0
	jumpBump(t, g, 10.1)
	if g.Level.At(10, 9) != Used {
		t.Fatal("hidden coin block not triggered from below")
	}
	if g.CoinCount != 1 {
		t.Errorf("coins = %d, want 1", g.CoinCount)
	}

	// The hidden 1-UP pays a life.
	jumpBump(t, g, 20.1)
	if g.Level.At(20, 9) != Used {
		t.Fatal("hidden 1-UP block not triggered")
	}
	for range 300 {
		g.Update(Input{Right: true, Run: true})
		if g.Lives != StartLives {
			break
		}
	}
	if g.Lives != StartLives+1 {
		t.Errorf("lives = %d after catching the 1-UP, want %d", g.Lives, StartLives+1)
	}
}
