package ui

import (
	"testing"

	"github.com/Daviey/mario/engine"
	"github.com/Daviey/mario/render"
)

// TestTitleIOpensAbout: title 'i' opens the about screen (no fetch, no
// identity churn), and 'i'/ESC close it back to the title.
func TestTitleIOpensAbout(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	ui := NewUI(nil, nil)
	g := engine.NewGame(engine.DefaultLevels(), 20, engine.LevelHeight) // title

	ui.note([]byte("x"))
	if snap := tickUntil(t, ui, g, 1); snap != nil {
		t.Fatal("non-i key must not open the about screen")
	}
	ui.note([]byte("i"))
	snap := tickUntil(t, ui, g, 1)
	if snap == nil || snap.Mode != render.UIAbout {
		t.Fatalf("i should open about, got %+v", snap)
	}
	// Any key still works to leave the title once about closes.
	ui.FeedKeys([]byte("i"))
	if s := tickUntil(t, ui, g, 1); s != nil {
		t.Fatalf("about should close on i: %+v", s)
	}
	// ESC closes too.
	ui.note([]byte("I")) // caps variant opens
	if s := tickUntil(t, ui, g, 1); s == nil || s.Mode != render.UIAbout {
		t.Fatalf("I should open about, got %+v", s)
	}
	ui.FeedKeys([]byte{0x1b})
	if s := tickUntil(t, ui, g, 1); s != nil {
		t.Fatalf("about should close on esc: %+v", s)
	}
}

// TestAboutNotOfferedAfterRun: the about screen is title-only — a game
// over must still ask for submission, never about.
func TestAboutNotOfferedAfterRun(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	ui := NewUI(nil, nil)
	g := engine.NewGame(engine.DefaultLevels(), 20, engine.LevelHeight)
	g.Update(engine.Input{AnyKey: true}) // leave title
	for range 10000 {
		g.Update(engine.Input{})
		if g.State == engine.StateGameOver {
			break
		}
	}
	if g.State != engine.StateGameOver {
		t.Fatalf("expected game over, state %v", g.State)
	}
	snap := ui.Tick(g)
	// A scoreless run prompts nothing; a scored run prompts the ask.
	// Either way, about must never follow a run.
	if snap != nil && snap.Mode == render.UIAbout {
		t.Fatalf("about must never open after a run, got %+v", snap)
	}
}
