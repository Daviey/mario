package ui

// The one-keyboard-one-owner input router: while a leaderboard screen
// captures input, keystrokes must never reach the game mapper.

import (
	"testing"

	"github.com/Daviey/mario/input"
	"github.com/Daviey/mario/render"
)

func TestRouterKeyboardOwnership(t *testing.T) {
	// While the leaderboard UI captures input, keystrokes must never reach
	// the game mapper — typing a name with 'p' in it must not pause, 'r'
	// must not restart. One consumer owns the keyboard at a time.
	m := input.NewMapper()
	ui := NewUI(nil, nil)
	r := NewRouter(m, ui)

	r.Feed([]byte("p"))
	if in := r.Poll(); !in.Pause {
		t.Fatal("during play the mapper must receive keys")
	}

	ui.mu.Lock()
	ui.mode = render.UIEntry
	ui.mu.Unlock()
	r.Feed([]byte("p")) // also a valid name letter
	if in := r.Poll(); in.Pause {
		t.Fatal("key leaked to the mapper during name entry")
	}
	ui.mu.Lock()
	got := string(ui.keys)
	ui.mu.Unlock()
	if got != "p" {
		t.Fatalf("UI captured %q; want %q", got, "p")
	}
}

// With kitty flags 1|2|8 pushed, letters arrive as CSI-u events. The
// byte-oriented UI and the title 'l' trigger must still see plain bytes,
// while the mapper consumes the raw stream natively.
func TestRouterDecodesKittyForUI(t *testing.T) {
	m := input.NewMapper()
	ui := NewUI(nil, nil)
	r := NewRouter(m, ui)

	r.Feed([]byte("\x1b[100;1:1u")) // press 'd'
	if in := r.Poll(); !in.Right {
		t.Fatal("kitty press must reach the mapper during play")
	}
	ui.mu.Lock()
	if noted := string(ui.noted); noted != "d" {
		ui.mu.Unlock()
		t.Fatalf("title trigger noted %q; want %q", noted, "d")
	}
	ui.mu.Unlock()

	ui.mu.Lock()
	ui.mode = render.UIEntry
	ui.mu.Unlock()
	r.Feed([]byte("\x1b[112;1:1u")) // press 'p': a valid name letter
	if in := r.Poll(); in.Pause {
		t.Fatal("kitty key leaked to the mapper during name entry")
	}
	ui.mu.Lock()
	got := string(ui.keys)
	ui.mu.Unlock()
	if got != "p" {
		t.Fatalf("UI captured %q; want %q", got, "p")
	}
	ui.mu.Lock()
	ui.keys = nil // drain: only the release event follows
	ui.mu.Unlock()

	r.Feed([]byte("\x1b[112;1:3u")) // release 'p': no edge, no effect
	ui.mu.Lock()
	got = string(ui.keys)
	ui.mu.Unlock()
	if got != "" {
		t.Fatalf("release event reached the UI as %q", got)
	}
}

func TestRouterClearsHoldsOnCapture(t *testing.T) {
	// A hold going into the game-over ask: the UI takes the keyboard,
	// the mapper stops seeing bytes, and its release — whenever it
	// arrives — routes to the UI alone. Without the capture-edge clear
	// the hold leaks into the next run (a sticky kitty hold forever):
	// the restarted game runs on untouched.
	mapper := input.NewMapper()
	u := NewUI(nil, nil)
	r := NewRouter(mapper, u)

	mapper.Feed([]byte("\x1b[100u")) // kitty 'd': Right held
	if in := mapper.Poll(); !in.Right {
		t.Fatal("setup: Right should be held")
	}

	g := gameOverGame(t)
	tickUntil(t, u, g, 2) // ask screen up; capturing

	// First Feed while captured must clear the mapper's holds.
	r.Feed([]byte("y"))
	for range 3 {
		if in := mapper.Poll(); in.Left || in.Right || in.Up || in.Down || in.Run {
			t.Fatal("entering capture must clear mapper holds")
		}
	}
}

func TestRouterReleaseAllOnRestart(t *testing.T) {
	// The board's 'r' path: even if a hold somehow survived capture, the
	// restart edge drops it — the new run starts from a clean keyboard.
	mapper := input.NewMapper()
	u := NewUI(nil, nil)
	r := NewRouter(mapper, u)

	mapper.Feed([]byte("d")) // legacy Right hold
	mapper.Poll()

	g := gameOverGame(t)
	tickUntil(t, u, g, 2)
	u.FeedKeys([]byte("y\r")) // accept the ask, keep the default name
	tickUntil(t, u, g, 2)     // board screen (nil submit: OFFLINE, done)
	u.FeedKeys([]byte("r"))   // restart from board
	tickUntil(t, u, g, 1)     // the tick drains the key buffer
	if in := r.Poll(); !in.Restart {
		t.Fatal("setup: restart edge expected")
	}
	for range 3 {
		if in := mapper.Poll(); in.Left || in.Right || in.Up || in.Down || in.Run {
			t.Fatal("restart must clear mapper holds")
		}
	}
}
