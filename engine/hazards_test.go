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
	if len(levels) != 7 {
		t.Fatalf("default levels = %d, want 7", len(levels))
	}
	for i, want := range []Theme{ThemeOverworld, ThemeUnderground, ThemeOverworld, ThemeOverworld,
		ThemeUnderground, ThemeSky, ThemeCastle} {
		if levels[i].Theme != want {
			t.Errorf("level %d theme = %v, want %v", i, levels[i].Theme, want)
		}
	}
	if levels[6].BarSpawns == nil {
		t.Error("castle level has no fire bars")
	}
}
