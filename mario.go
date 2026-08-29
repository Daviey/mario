// Package mario is an importable terminal Mario-style platformer.
//
// The game is fully deterministic — identical input sequences reproduce
// identical games — and renders as square terminal pixels (see the render
// package). Embed it in any Go program as an easter egg:
//
//	app := mario.New(nil)          // default levels and viewport
//
// //	go pumpInput(app)              // feed raw key bytes via app.Feed
//
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
	"strconv"
	"sync"
	"time"

	"github.com/Daviey/mario/board"
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

	// Mapper is the input mapper to wire in; nil creates a fresh one.
	Mapper *input.Mapper

	// DailyLevel provides the daily challenge level (called when the
	// player presses 'd' on the title screen). nil defaults to today's
	// generated level.
	DailyLevel func() *engine.Level

	// Session is an independent player identity for hosts that run many
	// Apps in one process (the SSH server gives each connection one).
	// nil keeps the process-wide identity (the native terminal path).
	Session *persist.Session

	// Cheats enables cheat mode: the fireball cap is lifted and the run
	// is deliberately NOT recorded, which keeps it off the leaderboard
	// (the UI shows UNRECORDED and refuses to submit; the server's
	// require_replay trigger rejects recording-less rows as well).
	// Opt-in only — it can never affect a normal run.
	Cheats bool
	// Sound, when non-nil, is invoked once per engine sound event
	// (Game.Events: "coin", "stomp", "die", ...) right after each
	// engine update. It is a notification only — events are not
	// consumed and stay readable on Game.Events (the browser build
	// reads them there for its WebAudio synth). Attract-mode demo
	// events are not reported: a title screen must not beep. The hook
	// runs on the goroutine that calls Step.
	Sound func(ev string)
	// Play context — operator-only diagnostics stored with each
	// submission: where the run was played (local/ssh/web), the
	// terminal's TERM and COLORTERM, and on the web build the browser's
	// user agent. The input regime and viewport are read live at submit.
	Surface, Term, ColorTerm, UserAgent string
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

	// Viewport change requested from another goroutine (Resize); lands
	// on the next Step so the engine is only touched from one place.
	resizeMu         sync.Mutex
	resizeW, resizeH int

	dailyLevel   func() *engine.Level
	idle         int             // ticks idle on the title screen (attract mode)
	demoT        int             // attract-demo script tick
	bestSaved    int             // score already persisted this session
	rec          replay.Recorder // this run's input log (leaderboard proof)
	prevState    engine.State    // state before the last Update
	levelsTrust  bool            // built-in level set: runs are verifiable
	dailyTrusted bool            // default daily generator: daily runs verifiable
	saveBest     func(score int) // records the session best (persist or per-connection)
	sound        func(string)    // optional per-event sound notification (Options.Sound)
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
	}

	daily := opts.DailyLevel
	if daily == nil {
		daily = func() *engine.Level {
			y, m, d := time.Now().UTC().Date()
			return engine.DailyLevelFor(y, int(m), d)
		}
	}

	g := engine.NewGame(levels, viewW, viewH)
	g.Cheats = opts.Cheats
	if opts.Session != nil {
		g.Best = opts.Session.Player().Best
	} else if pc, err := persist.LoadPlayer(); err == nil {
		g.Best = pc.Best
	}
	app := &App{
		Game:         g,
		mapper:       mapper,
		io:           nil, // set below
		dailyLevel:   daily,
		levelsTrust:  len(opts.Levels) == 0,
		dailyTrusted: opts.DailyLevel == nil,
		saveBest:     persist.SaveBest,
		sound:        opts.Sound,
	}
	mach := ui.NewUI(nil, nil)
	if opts.Session != nil {
		// One player identity per connection: submissions, name entry and
		// best-score bookkeeping all read the session, never the process
		// cache (which is neither per-player nor goroutine-safe).
		mach.SetIdentity(opts.Session.Player, opts.Session.SaveName)
		app.saveBest = opts.Session.SaveBest
	}
	mach.SetReplaySource(func() (string, bool) {
		if !app.rec.Shippable() {
			return "", false
		}
		if g.Daily {
			return app.rec.JSON(), app.dailyTrusted
		}
		return app.rec.JSON(), app.levelsTrust
	})
	surface, term, colorterm, userAgent := opts.Surface, opts.Term, opts.ColorTerm, opts.UserAgent
	mach.SetPlayContext(func() board.Entry {
		e := board.Entry{Surface: surface, UserAgent: userAgent, Term: term, ColorTerm: colorterm}
		if mapper.SawKitty() {
			e.InputRegime = "kitty"
		} else {
			e.InputRegime = "legacy"
		}
		e.Viewport = strconv.Itoa(g.ViewW) + "x" + strconv.Itoa(g.ViewH)
		return e
	})
	app.io = ui.NewRouter(mapper, mach)
	return app
}

// Feed routes one chunk of raw input bytes: to the leaderboard UI while a
// UI screen holds the keyboard, to the game mapper otherwise. Call this
// from the reader side; the game never blocks on it.
func (a *App) Feed(b []byte) { a.io.Feed(b) }

// Resize requests a new viewport in tiles, following the same policy as
// New (16..60 wide, at least 4 tall). Safe to call from any goroutine
// while another Steps: the change is applied on the next Step, and the
// next drawn frame repaints in full at the new size (render.Diff).
func (a *App) Resize(viewW, viewH int) {
	if viewW < 16 {
		viewW = 16
	}
	if viewW > 60 {
		viewW = 60
	}
	if viewH < 4 {
		viewH = 4
	}
	a.resizeMu.Lock()
	a.resizeW, a.resizeH = viewW, viewH
	a.resizeMu.Unlock()
}

// Step advances the game exactly one tick: polls input, updates the
// engine and the leaderboard UI. Drive it from your own clock (the
// terminal runner uses 60 Hz) and read Game/UI afterwards.
func (a *App) Step() {
	g := a.Game

	// A viewport change requested from another goroutine (SIGWINCH,
	// SSH window-change) lands here, on the tick goroutine.
	a.resizeMu.Lock()
	w, h := a.resizeW, a.resizeH
	a.resizeW, a.resizeH = 0, 0
	a.resizeMu.Unlock()
	if w != 0 {
		g.SetViewport(w, h)
	}

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
	// Cheat runs are deliberately unrecorded: without a shippable
	// recording the UI refuses to submit (UNRECORDED) and the server's
	// require_replay trigger would reject the row anyway.
	if !g.Demo && !g.Cheats {
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
	if a.sound != nil && !g.Demo {
		for _, ev := range g.Events {
			a.sound(ev)
		}
	}
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
		go a.saveBest(g.Score)
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
