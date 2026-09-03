package engine

import (
	"math"
	"testing"
)

func TestFireBarSpinsBothWays(t *testing.T) {
	a := NewFireBar(2, 5)
	b := NewFireBar(3, 5)
	if a.Speed <= 0 || b.Speed >= 0 {
		t.Fatalf("hub column parity should alternate spin: %v %v", a.Speed, b.Speed)
	}
	// One revolution takes 2π/speed ticks; ball 0 rides at radius ~1.
	p0 := a.BallPos(0, 0)
	quarter := a.BallPos(0, int(math.Pi/(2*math.Abs(a.Speed))))
	if d := math.Hypot(p0.X-quarter.X, p0.Y-quarter.Y); d < 1.0 {
		t.Errorf("ball barely moved in a quarter turn: dist=%f", d)
	}
	rev := int(2 * math.Pi / math.Abs(a.Speed))
	p1 := a.BallPos(0, rev)
	if d := math.Hypot(p0.X-p1.X, p0.Y-p1.Y); d > 0.1 {
		t.Errorf("ball did not complete an orbit: residual=%f", d)
	}
}

func TestFireBarDamagesPlayer(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) {
		b.Fill(20, 11, 20, 12, 'B')
		b.Set(20, 10, 'h')
	})
	g := newGame(t, l)
	g.Player.grow()
	g.Player.Pos = Vec{22.3, 13 - SuperH} // super body spans the sweep annulus
	for g.State == StatePlaying && g.Tick < 600 {
		g.Update(Input{})
	}
	if g.State != StateDying {
		t.Errorf("state = %v after standing in the fire-bar sweep, want dying", g.State)
	}
}

func TestLavaKills(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) {
		b.Fill(30, 13, 31, 14, 'L')
	})
	g := newGame(t, l)
	g.Player.Pos = Vec{30.3, 13 - SmallH}
	for g.State == StatePlaying && g.Tick < 30 {
		g.Update(Input{})
	}
	if g.State != StateDying {
		t.Errorf("state = %v after falling into lava, want dying", g.State)
	}
}

func TestLavaIgnoresZeroOverlapBoundary(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Set(20, 12, 'L') })
	g := newGame(t, l)

	// A sliver of real overlap still burns.
	g.Player.Pos = Vec{19.21, 11.5}
	if !g.touchingLava() {
		t.Error("body overlapping lava by a sliver must burn")
	}
	// Edges that merely kiss a tile boundary have zero overlap with the
	// lava itself and must not burn, horizontally...
	g.Player.Pos = Vec{19.2, 11.5} // right edge exactly at x=20.0
	if g.touchingLava() {
		t.Error("zero-overlap boundary column must not burn")
	}
	// ...and vertically.
	g.Player.Pos = Vec{19.5, 11.0} // feet exactly at y=12.0
	if g.touchingLava() {
		t.Error("zero-overlap boundary row must not burn")
	}
}

func TestParaHops(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Set(20, 12, 'W') })
	g := newGame(t, l)
	if len(g.Enemies) != 1 || g.Enemies[0].Kind != KindPara {
		t.Fatalf("para did not spawn: %+v", g.Enemies)
	}
	spawnY := g.Enemies[0].Pos.Y
	top := spawnY
	for range 300 {
		g.Update(Input{})
		if y := g.Enemies[0].Pos.Y; y < top {
			top = y
		}
	}
	if top > spawnY-0.5 {
		t.Errorf("para never left the ground: top y = %f (spawn %f)", top, spawnY)
	}
}

func TestParaStompDemotesToKoopa(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Set(20, 12, 'W') })
	g := newGame(t, l)
	e := g.Enemies[0]

	stomp := func() {
		g.Player.Pos = Vec{e.Pos.X + 0.1, e.Pos.Y - 1.2}
		g.Player.Vel = Vec{Y: 0.1}
		for range 6 {
			g.Update(Input{})
		}
	}
	stomp()
	if e.Kind != KindKoopa || e.State != EnemyWalking {
		t.Errorf("after first stomp: kind=%v state=%v, want koopa walking", e.Kind, e.State)
	}
	stomp()
	if e.State != EnemyShell {
		t.Errorf("after second stomp: state=%v, want shell", e.State)
	}
	if g.Score < StompScore*2 {
		t.Errorf("stomps did not score: %d", g.Score)
	}
}

func TestWorldTwoThemes(t *testing.T) {
	levels := DefaultLevels()
	if len(levels) != 20 {
		t.Fatalf("default levels = %d, want 20", len(levels))
	}
	for i, want := range []Theme{ThemeOverworld, ThemeUnderground, ThemeOverworld, ThemeCastle,
		ThemeOverworld, ThemeUnderwater, ThemeSky, ThemeCastle,
		ThemeOverworld, ThemeOverworld, ThemeSky, ThemeCastle,
		ThemeOverworld, ThemeUnderground, ThemeSky, ThemeCastle,
		ThemeOverworld, ThemeUnderground, ThemeSky, ThemeCastle} {
		if levels[i].Theme != want {
			t.Errorf("level %d theme = %v, want %v", i, levels[i].Theme, want)
		}
	}
	if levels[7].BarSpawns == nil || levels[11].BarSpawns == nil ||
		levels[15].BarSpawns == nil || levels[19].BarSpawns == nil {
		t.Error("castle levels have no fire bars")
	}
}

func TestPodobooKillsEvenWithStar(t *testing.T) {
	// Rises on its hash phase and kills on touch — star included.
	for _, star := range []int{0, 600} {
		g := newGame(t, buildLevel(t, 60))
		pd := newPodoboo(10, 12)
		g.Podoboos = append(g.Podoboos, pd)
		g.Player.Star = star
		g.Player.Pos = Vec{10.2, GroundTop - SmallH}
		g.Player.Vel = Vec{}
		dead := false
		for i := range 2 * (150 + 60) {
			g.Update(Input{})
			g.updatePodoboos()
			if g.State == StateDying {
				dead = true
				break
			}
			_ = i
		}
		if !dead {
			t.Fatalf("podoboo never killed the player (star=%d)", star)
		}
		if g.Lives != StartLives-1 {
			t.Errorf("lives = %d (star=%d)", g.Lives, star)
		}
	}
}

func TestPodobooRestsBelowSurfaceBetweenLeaps(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	pd := newPodoboo(10, 12)
	g.Podoboos = append(g.Podoboos, pd)
	rose := false
	for range 400 {
		g.Tick++
		g.updatePodoboos()
		if pd.Pos.Y < pd.BaseY {
			rose = true
		}
	}
	if !rose {
		t.Error("podoboo never rose out of the lava")
	}
	if pd.Pos.Y > pd.BaseY+PodobooRestDrop+0.01 {
		t.Errorf("podoboo sank past its rest depth: y=%f", pd.Pos.Y)
	}
}

func TestLeapingCheepSpawnerAndStomp(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	g.Level.CheepLeaping = true
	g.Level.CheepStopX = 40
	g.Player.Pos.X = 30
	g.Level.Set(30, g.Level.Height-1, Empty) // leapers only rise through open fall
	g.Player.Invincible = 1 << 30            // spawner smoke: ignore contact
	spawned := 0
	for range 300 {
		g.Tick++
		g.updateCheeps()
		if n := g.aliveCheeps(true); n > spawned {
			spawned = n
		}
		if n := g.aliveCheeps(true); n > CheepLeapCap {
			t.Fatalf("leaping cheeps over cap: %d", n)
		}
	}
	if spawned == 0 {
		t.Fatal("leap spawner never fired")
	}

	// A directly placed leaper is stompable mid-air for the flat 200.
	g = newGame(t, buildLevel(t, 60))
	c := &Cheep{Pos: Vec{20, 10}, Vel: Vec{CheepLeapVelX, 0.1}, W: CheepW, H: CheepH, Red: true, Leaping: true}
	g.Cheeps = append(g.Cheeps, c)
	dropPlayer(g, 20, 9.2) // feet land inside the cheep's stomp window
	g.Player.Vel.Y = 0.2
	for range 3 {
		g.updateCheeps()
	}
	if !c.Gone {
		t.Fatal("leaping cheep was not stomped")
	}
	if g.Score != CheepScore {
		t.Errorf("score = %d, want %d", g.Score, CheepScore)
	}
	if g.Player.Vel.Y >= 0 {
		t.Error("player did not bounce off the stomp")
	}
}

func TestCheepFireballAndSwimContact(t *testing.T) {
	// Fireball clears any cheep for the flat 200.
	g := newGame(t, buildLevel(t, 60))
	c := &Cheep{Pos: Vec{20, 8}, Vel: Vec{-CheepRedSpeed, 0}, W: CheepW, H: CheepH, Red: true}
	g.Cheeps = append(g.Cheeps, c)
	g.Fireballs = append(g.Fireballs, &Fireball{Pos: Vec{20.1, 8.1}, Vel: Vec{FireballSpeed, 0}})
	g.updateCheeps()
	if !c.Gone || !g.Fireballs[0].Gone {
		t.Fatal("fireball did not clear the cheep")
	}
	if g.Score != CheepScore {
		t.Errorf("score = %d, want %d", g.Score, CheepScore)
	}

	// A swimmer's contact hurts even from above (no stomp underwater).
	g = newGame(t, buildLevel(t, 60))
	g.Level.Underwater = true
	c = &Cheep{Pos: Vec{20, 12}, Vel: Vec{-CheepRedSpeed, 0}, W: CheepW, H: CheepH, Red: true}
	g.Cheeps = append(g.Cheeps, c)
	dropPlayer(g, 20, 12.1) // overlapping the swimmer: contact, never a stomp
	g.Player.Vel.Y = 0.2
	for range 3 {
		g.updateCheeps()
	}
	if g.State != StateDying {
		t.Fatalf("state = %v, want dying (swim cheep contact is never a stomp)", g.State)
	}
}

func TestBlooberLungesTowardPlayer(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	b := newBloober(Vec{20, 8})
	g.Bloopers = append(g.Bloopers, b)
	minY := b.Pos.Y
	for range 300 {
		g.updateBloopers()
		if b.Pos.Y < minY {
			minY = b.Pos.Y
		}
	}
	if minY >= 8 {
		t.Error("bloober never lunged upward")
	}
	if b.Pos.X >= 20 {
		t.Errorf("bloober did not home toward the player: x=%f", b.Pos.X)
	}
}

func TestBlooberFireballAndStar(t *testing.T) {
	g := newGame(t, buildLevel(t, 60))
	b := newBloober(Vec{20, 8})
	g.Bloopers = append(g.Bloopers, b)
	g.Fireballs = append(g.Fireballs, &Fireball{Pos: Vec{20.1, 8.3}, Vel: Vec{FireballSpeed, 0}})
	g.updateBloopers()
	if !b.Gone {
		t.Fatal("fireball did not clear the bloober")
	}
	if g.Score != BlooberScore {
		t.Errorf("score = %d, want %d", g.Score, BlooberScore)
	}

	g = newGame(t, buildLevel(t, 60))
	b = newBloober(Vec{2.1, 12})
	g.Bloopers = append(g.Bloopers, b)
	g.Player.Star = 600
	g.updateBloopers()
	if !b.Gone {
		t.Fatal("star did not clear the bloober")
	}
	if g.Score != BlooberScore {
		t.Errorf("score = %d, want %d", g.Score, BlooberScore)
	}
}
