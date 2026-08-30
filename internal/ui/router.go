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
	mapper    *input.Mapper
	ui        *UI
	plain     *input.PlainDecoder
	capturing bool // last-seen capture state, to catch the transition
}

func NewRouter(mapper *input.Mapper, ui *UI) *Router {
	return &Router{mapper: mapper, ui: ui, plain: input.NewPlainDecoder()}
}

// feed is called from the input reader goroutine.
func (r *Router) Feed(b []byte) {
	if len(b) == 0 {
		return
	}
	// Entering capture (game-over ask, name entry, board): from here
	// bytes flow to the UI alone, so the mapper never sees the releases
	// for keys still held at that moment. Drop them now — a leaked hold
	// survives into the next run (a sticky kitty hold forever, a legacy
	// one for its grace window) and the restarted game runs on untouched.
	if cap := r.ui.capturing(); cap != r.capturing {
		r.capturing = cap
		if cap {
			r.mapper.ReleaseAll()
		}
	}
	if r.capturing {
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
	// A truncated escape wedged in the plain decoder would swallow
	// every later UI trigger byte; age it out with the tick clock.
	r.plain.FlushStale()
	in := r.mapper.Poll()
	if r.ui.takeRestart() {
		in.Restart = true
		// The board held the keyboard since capture began: anything
		// the mapper still holds from before is a phantom by now.
		r.mapper.ReleaseAll()
	}
	return in
}

// TakeDailyAtTitle reports a pending title-screen 'd' (daily challenge)
// trigger. Runs on the game goroutine before Poll, so the same keypress
// cannot both start the daily and dismiss the title as a movement key.
func (r *Router) TakeDailyAtTitle(g *engine.Game) bool {
	return r.ui.TakeDailyAtTitle(g)
}

// UITick advances the leaderboard UI and returns its render snapshot.
func (r *Router) UITick(g *engine.Game) *render.ScoreUI { return r.ui.Tick(g) }

// QuitRequested reports whether the UI asked to leave the game.
func (r *Router) QuitRequested() bool { return r.ui.quitRequested() }
