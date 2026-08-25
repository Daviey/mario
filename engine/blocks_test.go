package engine

import (
	"math"
	"testing"
)

// bumpUnder places the player directly under a tile at (tx, ty) and jumps
// until that tile is bumped (used/broken or bump animation starts).
func bumpUnder(t *testing.T, g *Game, tx, ty int) {
	t.Helper()
	g.Player.Pos = Vec{float64(tx) + 0.15, 13 - g.Player.H}
	g.Player.Vel = Vec{}
	run(g, 1, Input{}) // settle grounded state
	for i := 0; i < 40; i++ {
		at := g.Level.At(tx, ty)
		if at == Used || at == Empty || g.BumpActive(tx, ty) {
			return
		}
		g.Update(Input{Up: true})
	}
}
func TestQuestionBlockGivesCoin(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Set(10, 9, '?') })
	g := newGame(t, l)
	bumpUnder(t, g, 10, 9)

	if got := g.Level.At(10, 9); got != Used {
		t.Fatalf("tile = %v, want Used", got)
	}
	if g.CoinCount != 1 || g.Score != CoinScore {
		t.Errorf("coins=%d score=%d, want 1/%d", g.CoinCount, g.Score, CoinScore)
	}
	// A coin pop particle was spawned.
	found := false
	for _, p := range g.Particles {
		if p.Kind == ParticleCoin {
			found = true
		}
	}
	if !found {
		t.Error("no coin pop particle")
	}
	if !g.BumpActive(10, 9) {
		t.Error("bump animation not active")
	}
}

func TestUsedBlockGivesNothing(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Set(10, 9, '?') })
	g := newGame(t, l)
	bumpUnder(t, g, 10, 9) // first bump spends it
	before := g.CoinCount
	bumpUnder(t, g, 10, 9) // second bump
	if g.CoinCount != before {
		t.Errorf("used block gave another coin (%d -> %d)", before, g.CoinCount)
	}
}

func TestMushroomBlockSpawnsAndEmerges(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Set(10, 9, 'U') })
	g := newGame(t, l)
	bumpUnder(t, g, 10, 9)

	if len(g.Mushrooms) != 1 {
		t.Fatalf("mushrooms = %d, want 1", len(g.Mushrooms))
	}
	m := g.Mushrooms[0]
	// The spawn tick itself already advances the emerge countdown once.
	if m.Emerge < MushroomEmergeTicks-1 || m.Emerge > MushroomEmergeTicks {
		t.Errorf("emerge = %d, want %d..%d", m.Emerge, MushroomEmergeTicks-1, MushroomEmergeTicks)
	}
	y0 := m.Pos.Y
	run(g, 10, Input{})
	if m.Pos.Y >= y0 {
		t.Errorf("mushroom did not rise during emerge: %f -> %f", y0, m.Pos.Y)
	}
	run(g, MushroomEmergeTicks, Input{})
	if m.Emerge != 0 {
		t.Fatalf("mushroom still emerging: %d", m.Emerge)
	}
	x0 := m.Pos.X
	run(g, 30, Input{})
	if m.Pos.X <= x0 {
		t.Errorf("mushroom did not walk after emerging: %f -> %f", x0, m.Pos.X)
	}
}

func TestMushroomCollectGrows(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Set(10, 9, 'U') })
	g := newGame(t, l)
	bumpUnder(t, g, 10, 9)
	m := g.Mushrooms[0]
	run(g, MushroomEmergeTicks+10, Input{})

	// Teleport the player onto the mushroom.
	g.Player.Pos = Vec{m.Pos.X, m.Pos.Y - g.Player.H}
	run(g, 1, Input{})
	if !m.Gone {
		t.Fatal("mushroom not collected")
	}
	if !g.Player.Super {
		t.Error("player did not grow")
	}
	if g.Player.W != SuperW || g.Player.H != SuperH {
		t.Errorf("size = %fx%f, want super", g.Player.W, g.Player.H)
	}
	if g.Score != MushroomScore {
		t.Errorf("score = %d, want %d", g.Score, MushroomScore)
	}
}

func TestMushroomWhileSuperOnlyScores(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Set(10, 9, 'U') })
	g := newGame(t, l)
	g.Player.Super = true
	g.Player.W, g.Player.H = SuperW, SuperH
	bumpUnder(t, g, 10, 9)
	m := g.Mushrooms[0]
	g.Player.Pos = Vec{m.Pos.X, m.Pos.Y - g.Player.H}
	run(g, 1, Input{})
	if !m.Gone || !g.Player.Super {
		t.Fatal("mushroom collection failed while super")
	}
}

func TestSmallBumpsBrickWithoutBreaking(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Set(10, 9, 'B') })
	g := newGame(t, l)
	bumpUnder(t, g, 10, 9)
	if got := g.Level.At(10, 9); got != Brick {
		t.Errorf("small player broke a brick: tile = %v", got)
	}
	if !g.BumpActive(10, 9) {
		t.Error("brick should still bump for a small player")
	}
}

func TestSuperBreaksBrick(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Set(10, 9, 'B') })
	g := newGame(t, l)
	g.Player.Super = true
	g.Player.W, g.Player.H = SuperW, SuperH
	bumpUnder(t, g, 10, 9)

	if got := g.Level.At(10, 9); got != Empty {
		t.Fatalf("super player did not break brick: tile = %v", got)
	}
	if g.Score != BrickScore {
		t.Errorf("score = %d, want %d", g.Score, BrickScore)
	}
	debris := 0
	for _, p := range g.Particles {
		if p.Kind == ParticleDebris {
			debris++
		}
	}
	if debris != 4 {
		t.Errorf("debris particles = %d, want 4", debris)
	}
}

func TestBumpAnimationDecays(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Set(10, 9, '?') })
	g := newGame(t, l)
	bumpUnder(t, g, 10, 9)
	if !g.BumpActive(10, 9) {
		t.Fatal("precondition: bump active")
	}
	run(g, 10, Input{})
	if g.BumpActive(10, 9) {
		t.Error("bump animation did not decay")
	}
}

func TestBumpFlipsEnemyAbove(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) {
		b.Set(10, 9, '?')
		b.Set(10, 8, 'G')
	})
	g := newGame(t, l)
	e := g.Enemies[0]
	// Stand the goomba on top of the block.
	e.Pos = Vec{10.05, 9 - GoombaH}
	bumpUnder(t, g, 10, 9)

	if e.State != EnemyFlipped {
		t.Errorf("enemy state = %v, want flipped", e.State)
	}
	if g.Score != CoinScore+StompScore {
		t.Errorf("score = %d, want coin+flip", g.Score)
	}
}

func TestCoinItemCollect(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Coins(10, 20) })
	g := newGame(t, l)
	if len(g.CoinItems) != 1 {
		t.Fatalf("coins = %d, want 1", len(g.CoinItems))
	}
	g.Player.Pos = Vec{20, 13 - SmallH}
	run(g, 10, Input{Up: true}) // jump through the coin's tile
	if g.CoinCount != 1 || g.Score != CoinScore {
		t.Errorf("coins=%d score=%d, want 1/%d", g.CoinCount, g.Score, CoinScore)
	}
	if len(g.CoinItems) != 0 {
		t.Errorf("collected coin not cleaned up: %d remain", len(g.CoinItems))
	}
}

func TestHundredCoinsExtraLife(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	g.CoinCount = 99
	g.addCoin()
	if g.CoinCount != 0 {
		t.Errorf("coins = %d, want wrap to 0", g.CoinCount)
	}
	if g.Lives != StartLives+1 {
		t.Errorf("lives = %d, want %d", g.Lives, StartLives+1)
	}
}

func TestCeilingPicksWithBestOverlap(t *testing.T) {
	// Two ceiling tiles; player is offset so the right one overlaps more.
	l := buildLevel(t, 60, func(b *Builder) {
		b.Set(10, 9, 'B')
		b.Set(11, 9, '?')
	})
	g := newGame(t, l)
	g.Player.Pos = Vec{10.8, 13 - SmallH} // overlaps the right tile more
	g.Player.Vel = Vec{}
	run(g, 1, Input{})
	for i := 0; i < 40 && g.Level.At(11, 9) == Question; i++ {
		g.Update(Input{Up: true})
	}
	if got := g.Level.At(11, 9); got != Used {
		t.Errorf("best-overlap tile not bumped: right = %v", got)
	}
	if got := g.Level.At(10, 9); got != Brick {
		t.Errorf("lesser-overlap tile should be untouched: %v", got)
	}
}

func TestApproxHelper(t *testing.T) {
	if !approx(1.0, 1.0+1e-9) || approx(1.0, 1.1) {
		t.Error("approx misbehaves")
	}
	if math.Abs(0) != 0 {
		t.Error("sanity")
	}
}
