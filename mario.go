// Package mario is an importable terminal Mario-style platformer.
//
// The game is fully deterministic — identical input sequences reproduce
// identical games — and renders as square terminal pixels (see the render
// package). Embed it in any Go program as an easter egg:
//
//	app := mario.New(nil)          // default levels and viewport
//	defer app.SaveCalibration()    // persist input learning (best-effort)
//	go pumpInput(app)              // feed raw key bytes via app.Feed
//	app.Run(render.NewStream(os.Stdout, render.NewPalette(true)))
//
// The package wires together the engine, the kitty-protocol input mapper,
// the one-keyboard-one-owner input router (game keys vs leaderboard text
// entry) and the Supabase-backed leaderboard UI. Lower-level pieces are
// importable directly: mario/engine (pure simulation), mario/render
// (frames → ANSI/canvas), mario/input (byte → Input mapping) and
// mario/board (PostgREST client).
//
// Consumers that own their own clock (e.g. the browser build) drive one
// frame at a time: app.Step(), then draw app.Game via render and read
// app.UI() for the leaderboard overlay.
package mario

import (
	"time"

	"mario/engine"
	"mario/input"
	"mario/internal/persist"
	"mario/internal/ui"
	"mario/render"
)

// Options configures New. The zero value is a playable default game.
type Options struct {
	// Levels are the levels to play through; nil means the built-ins.
	Levels []*engine.Level

	// ViewW is the viewport width in tiles (clamped to 16..60; 0 = 40).
	// ViewH is the viewport height in tiles (0 = the full level height).
	ViewW, ViewH int

	// Mapper is the input mapper to wire in; nil creates a fresh one and
	// applies any previously saved calibration (see SaveCalibration).
	Mapper *input.Mapper
}

// App is one wired-up game session: engine, input routing and leaderboard
// UI. It is safe to Feed from any single goroutine while Stepping on
// another (that is how the terminal runner works).
type App struct {
	Game   *engine.Game // the simulation; render reads its state each frame
	mapper *input.Mapper
	io     *ui.Router
	ui     *render.ScoreUI // latest leaderboard snapshot (nil when off)
	quit   bool
}

// New builds an App from opts and draws nothing yet.
func New(opts *Options) *App {
	if opts == nil {
		opts = &Options{}
	}
	levels := opts.Levels
	if len(levels) == 0 {
		levels = engine.DefaultLevels()
	}
	viewW := opts.ViewW
	if viewW == 0 {
		viewW = 40
	}
	if viewW < 16 {
		viewW = 16
	}
	if viewW > 60 {
		viewW = 60
	}
	viewH := opts.ViewH
	if viewH == 0 {
		viewH = engine.LevelHeight
	}
	if viewH < 4 {
		viewH = 4
	}
	if viewH > levels[0].Height {
		viewH = levels[0].Height
	}

	mapper := opts.Mapper
	if mapper == nil {
		mapper = input.NewMapper()
		persist.LoadCalibration(mapper)
	}

	return &App{
		Game:   engine.NewGame(levels, viewW, viewH),
		mapper: mapper,
		io:     ui.NewRouter(mapper, ui.NewUI(nil, nil)),
	}
}

// Feed routes one chunk of raw input bytes: to the leaderboard UI while a
// UI screen holds the keyboard, to the game mapper otherwise. Call this
// from the reader side; the game never blocks on it.
func (a *App) Feed(b []byte) { a.io.Feed(b) }

// Step advances the game exactly one tick: polls input, updates the
// engine and the leaderboard UI. Drive it from your own clock (the
// terminal runner uses 60 Hz) and read Game/UI afterwards.
func (a *App) Step() {
	in := a.io.Poll()
	a.Game.Update(in)
	a.ui = a.io.UITick(a.Game)
	a.quit = in.Quit || a.io.QuitRequested()
}

// UI returns the latest leaderboard render snapshot, or nil when no UI
// screen is showing.
func (a *App) UI() *render.ScoreUI { return a.ui }

// Quit reports whether the last Step saw a quit request (mapped 'q' or a
// leaderboard-screen close).
func (a *App) Quit() bool { return a.quit }

// Run plays the game at engine.TicksPerSecond, drawing differential
// frames to st, until quit. This is the blocking entry the terminal
// runner uses; browser or embedded consumers should call Step instead.
func (a *App) Run(st *render.Stream) {
	st.Draw(a.Game, nil) // first frame: full repaint

	ticker := time.NewTicker(time.Second / engine.TicksPerSecond)
	defer ticker.Stop()
	for range ticker.C {
		a.Step()
		st.Draw(a.Game, a.ui)
		if a.quit {
			return
		}
	}
}

// SaveCalibration persists the input mapper's learning (OS key-repeat
// delay, per-key hold habits) so the next session starts warm. Best-effort:
// failures are ignored.
func (a *App) SaveCalibration() { persist.SaveCalibration(a.mapper) }
