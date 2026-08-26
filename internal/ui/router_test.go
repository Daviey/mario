package ui

// The one-keyboard-one-owner input router: while a leaderboard screen
// captures input, keystrokes must never reach the game mapper.

import (
	"testing"

	"mario/input"
	"mario/render"
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
