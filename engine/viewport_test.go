package engine

import (
	"fmt"
	"testing"
)

// The viewport is a camera and rendering concern only. If a mid-game
// resize ever perturbed the simulation, replays recorded at one window
// size would stop verifying at another — this test is the tripwire.
func TestSetViewportLeavesSimulationIdentical(t *testing.T) {
	l := buildLevel(t, 120, func(b *Builder) {
		b.Set(20, 9, 'G')
		b.Set(30, 5, 'c')
		b.Set(45, 9, 'K')
		b.Set(60, 5, 'c')
		b.Set(75, 9, 'G')
	})
	snap := func(g *Game) string {
		sb := fmt.Sprintf("s=%d c=%d l=%d t=%d st=%v p=(%.4f,%.4f,%.4f,%.4f) pw=%v",
			g.Score, g.CoinCount, g.Lives, g.Time, g.State,
			g.Player.Pos.X, g.Player.Pos.Y, g.Player.Vel.X, g.Player.Vel.Y, g.Player.Power)
		for _, e := range g.Enemies {
			sb += fmt.Sprintf("|e(%.3f,%.3f,%v,%d)", e.Pos.X, e.Pos.Y, e.State, e.Dir)
		}
		return sb
	}
	fixed := NewGame([]*Level{l}, 40, LevelHeight)
	churn := NewGame([]*Level{l}, 40, LevelHeight)
	fixed.State = StatePlaying
	churn.State = StatePlaying

	input := func(i int) Input {
		in := Input{Right: true}
		if ph := i % 90; ph >= 30 && ph < 34 {
			in.Up = true
		}
		return in
	}

	for i := range 1200 {
		if i%17 == 0 { // wild viewport churn on one side only
			w, h := 16, 4
			if (i/17)%2 == 1 {
				w, h = 60, LevelHeight
			}
			churn.SetViewport(w, h)
		}
		in := input(i)
		fixed.Update(in)
		churn.Update(in)
	}
	if a, b := snap(fixed), snap(churn); a != b {
		t.Fatalf("resize changed the simulation:\nfixed: %s\nchurn: %s", a, b)
	}
}

func TestSetViewportClamps(t *testing.T) {
	g := newGame(t, buildLevel(t, 200))
	g.SetViewport(300, 300)
	if g.ViewW != 200 || g.ViewH != LevelHeight {
		t.Fatalf("viewport = %dx%d, want level-bounded 200x%d", g.ViewW, g.ViewH, LevelHeight)
	}
	g.SetViewport(0, 0)
	if g.ViewW != 200 || g.ViewH != LevelHeight {
		t.Fatalf("zero resize moved viewport to %dx%d", g.ViewW, g.ViewH)
	}
	g.SetViewport(30, 8)
	if g.ViewW != 30 || g.ViewH != 8 {
		t.Fatalf("viewport = %dx%d, want 30x8", g.ViewW, g.ViewH)
	}
	// The camera re-clamps to the new width on the next update.
	g.Player.Pos = Vec{X: 190, Y: 13 - SmallH}
	g.Update(Input{})
	if max := float64(200 - 30); g.CameraX > max+1e-9 {
		t.Fatalf("camera %f exceeds new clamp %f", g.CameraX, max)
	}
}
