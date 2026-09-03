package engine

import "testing"

// gameOverAt drives a real run out of lives on the given level index
// and parks the game on the game-over screen.
func gameOverAt(t *testing.T, g *Game, idx int) {
	t.Helper()
	g.loadLevel(idx, PowerSmall)
	g.State = StatePlaying
	g.Lives = 1 // one life left: a single death ends it
	g.Update(Input{})
	g.Update(Input{Suicide: true})
	for g.State == StateDying {
		g.Update(Input{})
	}
	if g.State != StateGameOver {
		t.Fatalf("setup: state = %v, want game over", g.State)
	}
}

// TestContinueResumesAtWorldStart pins the original's game-over
// continue: held jump plus any key resumes the quest at the current
// world's first level — score zeroed, three lives back, world card.
func TestContinueResumesAtWorldStart(t *testing.T) {
	g := NewGame(DefaultLevels(), 40, LevelHeight)
	gameOverAt(t, g, 5) // die in 2-2
	if got := g.LevelName(); got != "2-2" {
		t.Fatalf("setup: died in %q, want 2-2", got)
	}
	g.Update(Input{}) // settle prevIn
	g.Update(Input{Up: true, AnyKey: true})
	if g.State != StateWorldCard {
		t.Fatalf("after continue: state = %v, want the world card", g.State)
	}
	if got := g.LevelName(); got != "2-1" {
		t.Fatalf("continue landed on %q, want the world's first level 2-1", got)
	}
	if g.Lives != StartLives || g.Score != 0 || g.CoinCount != 0 {
		t.Fatalf("continue state: lives=%d score=%d coins=%d, want %d/0/0", g.Lives, g.Score, g.CoinCount, StartLives)
	}
}

// TestContinueNeedsHeldJump: a bare any-key press at game over must
// NOT continue (the hold is the original's A-button contract) — the
// state stays for the host's title flow.
func TestContinueNeedsHeldJump(t *testing.T) {
	g := NewGame(DefaultLevels(), 40, LevelHeight)
	gameOverAt(t, g, 2)
	g.Update(Input{})
	g.Update(Input{AnyKey: true})
	if g.State != StateGameOver {
		t.Fatalf("bare any-key: state = %v, want still game over", g.State)
	}
}
