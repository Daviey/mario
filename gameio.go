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
	plain  *input.PlainDecoder
}

func newGameIO(mapper *input.Mapper, ui *scoreUI) *gameIO {
	return &gameIO{mapper: mapper, ui: ui, plain: input.NewPlainDecoder()}
}

// feed is called from the input reader goroutine.
func (io *gameIO) feed(b []byte) {
	if len(b) == 0 {
		return
	}
	if io.ui.capturing() {
		io.ui.feedKeys(io.plain.Feed(b))
		return
	}
	io.ui.note(io.plain.Feed(b)) // plain bytes: the 'l' trigger sees CSI-u keys too
	io.mapper.Feed(b)            // the mapper speaks the kitty protocol natively
}

// poll returns this tick's game input. A restart requested from the
// board screen is injected as the same edge a mapped 'r' press would
// produce — the native and wasm loops both go through here.
func (io *gameIO) poll() engine.Input {
	in := io.mapper.Poll()
	if io.ui.takeRestart() {
		in.Restart = true
	}
	return in
}

// uiTick advances the leaderboard UI and returns its render snapshot.
func (io *gameIO) uiTick(g *engine.Game) *render.ScoreUI { return io.ui.tick(g) }

// quitRequested reports whether the UI asked to leave the game.
func (io *gameIO) quitRequested() bool { return io.ui.quitRequested() }
