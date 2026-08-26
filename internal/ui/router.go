package ui

// Router routes raw input bytes: while the leaderboard UI captures input
// (name entry, prompts), bytes never reach the game mapper — so typing
// letters can never trigger game keys (e.g. 'r' restarting the game).

import (
	"github.com/Daviey/mario/engine"
	"github.com/Daviey/mario/input"
	"github.com/Daviey/mario/render"
)

type Router struct {
	mapper *input.Mapper
	ui     *UI
	plain  *input.PlainDecoder
}

func NewRouter(mapper *input.Mapper, ui *UI) *Router {
	return &Router{mapper: mapper, ui: ui, plain: input.NewPlainDecoder()}
}

// feed is called from the input reader goroutine.
func (r *Router) Feed(b []byte) {
	if len(b) == 0 {
		return
	}
	if r.ui.capturing() {
		r.ui.FeedKeys(r.plain.Feed(b))
		return
	}
	r.ui.note(r.plain.Feed(b)) // plain bytes: the 'l' trigger sees CSI-u keys too
	r.mapper.Feed(b)           // the mapper speaks the kitty protocol natively
}

// poll returns this tick's game input. A restart requested from the
// board screen is injected as the same edge a mapped 'r' press would
// produce — the native and wasm loops both go through here.
func (r *Router) Poll() engine.Input {
	in := r.mapper.Poll()
	if r.ui.takeRestart() {
		in.Restart = true
	}
	return in
}

// UITick advances the leaderboard UI and returns its render snapshot.
func (r *Router) UITick(g *engine.Game) *render.ScoreUI { return r.ui.Tick(g) }

// QuitRequested reports whether the UI asked to leave the game.
func (r *Router) QuitRequested() bool { return r.ui.quitRequested() }
