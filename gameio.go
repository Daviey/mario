package main

// gameIO routes raw input bytes: while the leaderboard UI captures input
// (name entry, prompts), bytes never reach the game mapper — so typing
// letters can never trigger game keys (e.g. 'r' restarting the game).

import (
	"mario/engine"
	"mario/input"
	"mario/render"
)

type gameIO struct {
	mapper *input.Mapper
	ui     *scoreUI
}

func newGameIO(mapper *input.Mapper, ui *scoreUI) *gameIO {
	return &gameIO{mapper: mapper, ui: ui}
}

// feed is called from the input reader goroutine.
func (io *gameIO) feed(b []byte) {
	if len(b) == 0 {
		return
	}
	if io.ui.capturing() {
		io.ui.feedKeys(b)
		return
	}
	io.ui.note(b) // for the title-screen 'l' trigger
	io.mapper.Feed(b)
}

// poll returns this tick's game input.
func (io *gameIO) poll() engine.Input { return io.mapper.Poll() }

// uiTick advances the leaderboard UI and returns its render snapshot.
func (io *gameIO) uiTick(g *engine.Game) *render.ScoreUI { return io.ui.tick(g) }

// quitRequested reports whether the UI asked to leave the game.
func (io *gameIO) quitRequested() bool { return io.ui.quitRequested() }
