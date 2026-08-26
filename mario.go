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

	"github.com/Daviey/mario/engine"
	"github.com/Daviey/mario/input"
	"github.com/Daviey/mario/internal/persist"
	"github.com/Daviey/mario/internal/ui"
	"github.com/Daviey/mario/render"
	"github.com/Daviey/mario/replay"
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

	// DailyLevel provides the daily challenge level (called when the
	// player presses 'd' on the title screen). nil defaults to today's
	// generated level.
	DailyLevel func() *engine.Level
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

	dailyLevel func() *engine.Level
	idle       int // ticks idle on the title screen (attract mode)
	demoT      int // attract-demo script tick
	bestSaved  int // score already persisted this session

	rec          replay.Recorder // this run's input log (leaderboard proof)
	prevState    engine.State    // state before the last Update
	levelsTrust  bool            // built-in level set: runs are verifiable
	dailyTrusted bool            // default daily generator: daily runs verifiable
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

	daily := opts.DailyLevel
	if daily == nil {
		daily = func() *engine.Level {
			y, m, d := time.Now().UTC().Date()
			return engine.DailyLevelFor(y, int(m), d)
		}
	}

	g := engine.NewGame(levels, viewW, viewH)
	if pc, err := persist.LoadPlayer(); err == nil {
		g.Best = pc.Best
	}
	app := &App{
		Game:         g,
		mapper:       mapper,
		io:           nil, // set below
		dailyLevel:   daily,
		levelsTrust:  len(opts.Levels) == 0,
		dailyTrusted: opts.DailyLevel == nil,
	}
	mach := ui.NewUI(nil, nil)
	mach.SetReplaySource(func() (string, bool) {
		if !app.rec.Shippable() {
			return "", false
		}
		if g.Daily {
			return app.rec.JSON(), app.dailyTrusted
		}
		return app.rec.JSON(), app.levelsTrust
	})
	app.io = ui.NewRouter(mapper, mach)
	return app
}

// Feed routes one chunk of raw input bytes: to the leaderboard UI while a
// UI screen holds the keyboard, to the game mapper otherwise. Call this
// from the reader side; the game never blocks on it.
func (a *App) Feed(b []byte) { a.io.Feed(b) }

// Step advances the game exactly one tick: polls input, updates the
// engine and the leaderboard UI. Drive it from your own clock (the
// terminal runner uses 60 Hz) and read Game/UI afterwards.
func (a *App) Step() {
	g := a.Game

	// Title-screen 'd' starts the daily challenge (checked before the
	// update so the same press cannot also dismiss the title).
	if a.io.TakeDailyAtTitle(g) {
		a.StartDaily()
	}

	in := a.io.Poll()

	if g.Demo {
		// Attract mode: any real key bails back to the title; otherwise
		// the deterministic demo script drives the game.
		if in != (engine.Input{}) {
			g.EndDemo()
			a.idle, a.demoT = 0, 0
			in = engine.Input{}
		} else {
			in = ui.ScriptInput(a.demoT)
			a.demoT++
		}
	}
	if !g.Demo {
		switch g.State {
		case engine.StateWorldCard:
			// A fresh run begins here — unless the card follows a death
			// respawn or a level clear, in which case the same run (and
			// its recording) continues.
			switch a.prevState {
			case engine.StateDying, engine.StateScoreTick:
			default:
				a.rec.Start()
			}
		case engine.StateTitle:
			a.rec.Reset()
		}
		a.rec.Record(in)
	}
	g.Update(in)
	a.prevState = g.State
	if g.State == engine.StateGameOver || g.State == engine.StateWin {
		a.rec.Finish()
	}
	a.ui = a.io.UITick(g)
	a.quit = in.Quit || a.io.QuitRequested()
	if a.quit {
		return
	}

	if g.Demo {
		return // no attract bookkeeping below
	}

	// Attract mode: after ~10s idle on the title (and no UI screen
	// holding the keyboard), run the demo behind the title art.
	if g.State == engine.StateTitle && a.ui == nil {
		a.idle++
		if a.idle >= 600 {
			g.BeginDemo()
			a.demoT = 0
		}
	} else {
		a.idle = 0
	}

	// Persist the local best once per run end.
	if (g.State == engine.StateGameOver || g.State == engine.StateWin) &&
		g.Score > a.bestSaved && g.Score > 0 {
		a.bestSaved = g.Score
		go persist.SaveBest(g.Score)
	}
}

// StartDaily swaps in the daily challenge level and starts the run.
func (a *App) StartDaily() {
	a.Game.Levels = []*engine.Level{a.dailyLevel()}
	a.Game.BeginDaily()
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
	tick := time.NewTicker(time.Second / engine.TicksPerSecond)
	defer tick.Stop()
	for range tick.C {
		a.Step()
		if a.ui != nil {
			st.Draw(a.Game, a.ui)
		} else {
			st.Draw(a.Game)
		}
		if a.quit {
			return
		}
	}
}

// SaveCalibration persists the input mapper's learning (OS key-repeat
// delay, per-key hold habits) so the next session starts warm. Best-effort:
// failures are ignored.
func (a *App) SaveCalibration() { persist.SaveCalibration(a.mapper) }
