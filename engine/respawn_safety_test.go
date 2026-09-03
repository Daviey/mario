package engine

import (
	"math"
	"testing"
)

// respawnAt drives a real death-and-respawn on one level and returns the
// game parked in Playing at the checkpoint (card consumed).
func respawnAt(t *testing.T, l *Level) *Game {
	t.Helper()
	g := NewGame([]*Level{l}, 40, LevelHeight)
	g.Update(Input{Up: true}) // title
	for g.State != StatePlaying {
		g.Update(Input{})
	}
	g.Player.Pos.X = l.CheckpointX + 2
	for range 5 {
		g.Update(Input{})
	}
	g.Update(Input{Suicide: true})
	for g.State == StateDying {
		g.Update(Input{})
	}
	if g.State == StateGameOver {
		t.Fatalf("%s: game over on the first death", l.Name)
	}
	for g.State == StateWorldCard {
		g.Update(Input{})
	}
	if abs(int(g.Player.Pos.X-l.CheckpointX)) > 2 {
		t.Fatalf("%s: respawned at %.1f, checkpoint %.0f", l.Name, g.Player.Pos.X, l.CheckpointX)
	}
	return g
}

// TestRespawnIdleIsSafe: after a real respawn, a player who has not
// touched the controls must survive one full card-window of play —
// nothing the level resets to (walkers, hazards, spawners) may reach
// the column in that time. The 2026-09-02 loop class: 2-4/3-1 walkers
// parked a walk-in outside the old static berth killed idle AND
// escaping players within 39 ticks; 2-3's leapers targeted the respawn
// column the moment the card lifted (now silenced by the grace window).
func TestRespawnIdleIsSafe(t *testing.T) {
	for _, l := range DefaultLevels() {
		g := respawnAt(t, l)
		for tick := 0; tick < WorldCardTicks; tick++ {
			g.Update(Input{})
			if g.State == StateDying || g.State == StateGameOver {
				t.Errorf("%s: idle player died %d ticks after respawn (x=%.1f)", l.Name, tick, g.Player.Pos.X)
				break
			}
		}
	}
}

// TestCheckpointSkipsWalkIn: a walker just outside the old static berth
// reaches the column in ~40 ticks — the picker must widen past it.
func TestCheckpointSkipsWalkIn(t *testing.T) {
	l := buildLevel(t, 120)
	cp := l.CheckpointX
	l.KoopaSpawns = append(l.KoopaSpawns, Vec{cp + SmallW + KoopaW + 0.1, l.PlayerStart.Y + 1 - KoopaH})
	l.computeCheckpoint()
	if l.spawnThreatNear(l.CheckpointX, GroundTop) {
		t.Fatalf("checkpoint %.0f still inside a walker's walk-in berth", l.CheckpointX)
	}
	if cp == l.CheckpointX {
		t.Fatalf("picker did not move off the walk-in column (cp=%.0f)", cp)
	}
}

// TestCheckpointSkipsPodobooLane: a podoboo marker over a ground column
// makes that column's full arc band lethal at some phase.
func TestCheckpointSkipsPodobooLane(t *testing.T) {
	l := buildLevel(t, 120, func(b *Builder) {
		// Force the picker's candidate: clear other columns past half
		// width except one with a podoboo lane above it.
	})
	cp := int(l.CheckpointX)
	l.PodobooSpawns = append(l.PodobooSpawns, Vec{float64(cp), float64(GroundTop)})
	l.computeCheckpoint()
	for _, s := range l.PodobooSpawns {
		if abs(int(s.X)-int(l.CheckpointX)) == 0 {
			t.Fatalf("checkpoint %.0f sits in podoboo lane %.0f", l.CheckpointX, s.X)
		}
	}
}

// TestRespawnClearsNewHazards: the belt-and-braces wipe must cover the
// fidelity hazard slices, not just walkers and bowsers.
func TestRespawnClearsNewHazards(t *testing.T) {
	for _, plant := range []struct {
		name string
		fn   func(g *Game, x, y float64)
	}{
		{"bloober", func(g *Game, x, y float64) { g.Bloopers = append(g.Bloopers, newBloober(Vec{x, y})) }},
		{"hammerbro", func(g *Game, x, y float64) { g.HammerBros = append(g.HammerBros, newHammerBro(Vec{x, y})) }},
		{"podoboo", func(g *Game, x, y float64) { g.Podoboos = append(g.Podoboos, newPodoboo(x, GroundTop)) }},
	} {
		l := buildLevel(t, 120)
		g := NewGame([]*Level{l}, 40, LevelHeight)
		g.Update(Input{Up: true})
		for g.State != StatePlaying {
			g.Update(Input{})
		}
		g.Player.Pos.X = l.CheckpointX + 2
		for range 5 {
			g.Update(Input{})
		}
		plant.fn(g, g.Player.Pos.X, g.Player.Pos.Y)
		g.Update(Input{Suicide: true})
		for g.State == StateDying || g.State == StateWorldCard {
			g.Update(Input{})
		}
		px, py := g.Player.Pos.X, g.Player.Pos.Y
		for _, b := range g.Bloopers {
			if !b.Gone && overlap(b.Pos.X, b.Pos.Y, b.W, b.H, px, py, SmallW, SmallH) {
				t.Errorf("%s: bloober survived the respawn wipe", plant.name)
			}
		}
		for _, b := range g.HammerBros {
			if !b.Gone && overlap(b.Pos.X, b.Pos.Y, b.W, b.H, px, py, SmallW, SmallH) {
				t.Errorf("%s: hammer bro survived the respawn wipe", plant.name)
			}
		}
		for _, o := range g.Podoboos {
			if !o.Gone && overlap(o.Pos.X, o.Pos.Y, o.W, o.H, px, py, SmallW, SmallH) {
				t.Errorf("%s: podoboo survived the respawn wipe", plant.name)
			}
		}
	}
}

// TestRespawnGraceSilencesBlasters: the card-window after a respawn
// must spawn no bullets at all — a fresh respawner standing inside a
// cannon's span (forced checkpoint, bypassing the picker) would take a
// muzzle-point shot with no input to dodge with (2026-09-03: the
// blaster debut predates the grace gate by one commit).
func TestRespawnGraceSilencesBlasters(t *testing.T) {
	l := buildLevel(t, 80, func(b *Builder) {
		b.Fill(28, 12, 29, 12, 'N') // the muzzle spans the checkpoint
	})
	l.CheckpointX = 30 // hand-forced: the picker would refuse this column
	g := respawnAt(t, l)
	if g.respawnGrace <= 0 {
		t.Fatal("setup: the grace window is already consumed")
	}
	// Deterministic, not cadence-lucky: park the clock one tick before
	// this cannon's firing tick (the phase is a pure function of its
	// muzzle column), so the shot WOULD leave on the next Update.
	phase := int(bowserHash(int(g.Level.BlasterSpawns[0].X), 3))
	for (g.Tick+1+phase)%BulletFireEvery != 0 {
		g.Tick++
	}
	g.Update(Input{})
	if len(g.Bullets) != 0 {
		t.Fatal("blaster fired inside the respawn grace window")
	}
	if g.State == StateDying || g.State == StateGameOver {
		t.Fatal("idle respawner died beside the cannon inside the grace")
	}
	// Past the grace, the same alignment speaks again — the silence is
	// a window, not a broken spawner.
	for g.respawnGrace > 0 {
		g.Update(Input{})
	}
	for (g.Tick+1+phase)%BulletFireEvery != 0 {
		g.Tick++
	}
	g.Update(Input{})
	if len(g.Bullets) == 0 {
		t.Fatal("blasters stayed silent after the grace window expired")
	}
}

// TestCheckpointAvoidsBlasterColumns: the auto-checkpoint must keep a
// ground-row cannon's card-window of bullet travel clear (the span
// mirrors BulletCardSpan; high shelf cannons are exempt).
func TestCheckpointAvoidsBlasterColumns(t *testing.T) {
	l := buildLevel(t, 120, func(b *Builder) {
		b.Fill(56, 12, 57, 12, 'N') // ground row, dead centre
		b.Fill(64, 9, 65, 9, 'B')   // a shelf...
		b.Fill(64, 8, 65, 8, 'N')   // ...whose cannon fires high
	})
	mid := l.CheckpointX
	for _, s := range l.BlasterSpawns {
		high := s.Y+0.85 <= float64(GroundTop)-SmallH
		if high {
			continue
		}
		if math.Abs(s.X-(mid+SmallW/2)) < BulletCardSpan+SmallW {
			t.Fatalf("checkpoint %.0f sits inside the ground cannon at %.1f's flight span", mid, s.X)
		}
	}
}
