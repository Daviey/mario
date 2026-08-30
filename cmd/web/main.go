//go:build js

package main

// Browser (WASM) entry point. The game runs fully client-side and paints
// its pixel buffer straight onto a canvas owned by the page — no terminal
// emulation, so there are no font metrics, no cell grids and no escape
// parsing to artifact.
//
// Page contract (all relative-safe; the page owns presentation):
//
//	window.marioFrame(w, h int, rgb []byte)
//	                            - set BEFORE instantiation; receives each
//	                              frame as tight RGB (w*h*3).
//	marioFeed(string)          - exported here; the page sends xterm/kitty
//	                              style key sequences (press and release).
//	marioSize(worldPxW, worldPxH)
//	                            - exported here; sizes the game viewport in
//	                              world pixels (tiles = px/4).
//	marioBoard(json string)    - called by the game on leaderboard UI
//	                              changes; the page renders it as DOM text
//	                              ({"mode":"off"} hides the panel).

import (
	"encoding/json"
	"strings"
	"syscall/js"

	"github.com/Daviey/mario"
	"github.com/Daviey/mario/engine"
	"github.com/Daviey/mario/render"
)

// jsRGB pushes frame pixels into a page-owned Uint8Array and hands it to
// marioFrame, minimizing per-frame allocations.
type jsRGB struct {
	buf js.Value // Uint8Array of len w*h*3, resized when the frame size changes
	w   int
	h   int
}

var jsFrameSink jsRGB

func (j *jsRGB) deliver(f *render.Frame) {
	n := f.W * f.H * 3
	if j.buf.IsUndefined() || j.w != f.W || j.h != f.H || j.buf.Length() != n {
		j.buf = js.Global().Get("Uint8Array").New(n)
		j.w, j.h = f.W, f.H
	}
	js.CopyBytesToJS(j.buf, f.RGBBytes())
	js.Global().Call("marioFrame", f.W, f.H, j.buf)
}

func main() {
	// ?cheats opts into cheat mode: unlimited fireballs, run unrecorded
	// and therefore unsubmitable (see mario.Options.Cheats).
	// Play-context diagnostics: the browser's user agent rides along on
	// every submission (surface "web"); UA header + column on the DB side.
	cheats := strings.Contains(js.Global().Get("location").Get("search").String(), "cheats")
	ua := ""
	if n := js.Global().Get("navigator"); n.Truthy() {
		ua = n.Get("userAgent").String()
	}
	app := mario.New(&mario.Options{Cheats: cheats, Surface: "web", UserAgent: ua})

	js.Global().Set("marioFeed", js.FuncOf(func(_ js.Value, args []js.Value) any {
		app.Feed([]byte(args[0].String()))
		return nil
	}))

	g := app.Game

	// Live viewport: the page reports how many world pixels it can show;
	// tiles = pixels/Pix. This callback runs on the browser event loop
	// while the ticker goroutine Steps, so the write must go through
	// App.Resize (mutex'd, applied on the next Step) — assigning
	// g.ViewW/H here races the tick. Out-of-range sizes clamp to the
	// nearest bound instead of being ignored (App.Resize: 16..60 wide,
	// 4+ tall; the engine bounds by level).
	js.Global().Set("marioSize", js.FuncOf(func(_ js.Value, args []js.Value) any {
		w := args[0].Int() / render.Pix
		if w < 16 {
			w = 16 // playable minimum, matching the pre-Resize behavior
		}
		h := (args[1].Int() - render.HudBandPx - render.StatusBandPx) / render.Pix
		app.Resize(w, h)
		jsFrameSink = jsRGB{} // frame size changes: reallocate on next deliver
		return nil
	}))

	pal := render.NewPalette(render.Colors24)
	// The canvas always shows the world; leaderboard screens go to the
	// page as JSON and render as real DOM text (window.marioBoard).
	board := js.Global().Get("marioBoard")
	lastBoard := ""
	pushBoard := func(ui *render.ScoreUI) {
		if ui == nil {
			ui = &render.ScoreUI{}
		}
		if b, err := json.Marshal(ui); err == nil && string(b) != lastBoard {
			lastBoard = string(b)
			board.Invoke(string(b))
		}
	}
	// Page-provided hooks (both optional; the page defines them before
	// the wasm loads): marioSfx(name) plays a sound event, marioTitle(b)
	// tells the page when the title screen is up (it shows the DAILY
	// button only there).
	sfx := js.Global().Get("marioSfx")
	titleFn := js.Global().Get("marioTitle")
	lastTitle := true
	if titleFn.Truthy() {
		titleFn.Invoke(true)
	}

	draw := func() { jsFrameSink.deliver(render.RenderPixels(g, pal)) }
	draw()
	pushBoard(nil)

	ticker := newTicker(engine.TicksPerSecond)
	for {
		ticker.wait()
		app.Step()
		draw()
		pushBoard(app.UI())
		if sfx.Truthy() {
			for _, ev := range g.Events {
				sfx.Invoke(ev)
			}
		}
		if at := g.State == engine.StateTitle; at != lastTitle {
			lastTitle = at
			if titleFn.Truthy() {
				titleFn.Invoke(at)
			}
		}
		if app.Quit() {
			app.ResetToTitle()
		}
	}
	select {} // hold the last frame; the page offers a restart
}

// ticker bridges the Go ticker to the browser event loop: each wait yields
// to the event loop so JS callbacks (input) can run between frames.
type ticker struct {
	ch chan struct{}
}

func newTicker(hz int) *ticker {
	t := &ticker{ch: make(chan struct{}, 1)}
	go func() {
		for {
			<-timeAfter(1000 / hz)
			select {
			case t.ch <- struct{}{}:
			default:
			}
		}
	}()
	return t
}

func (t *ticker) wait() { <-t.ch }

// timeAfter fires after ms on the browser event loop. Each call mints a
// fresh one-shot js.FuncOf (its channel differs per call) rather than
// reusing a single callback: the churn is bounded — one pending timer
// per ticker wait, and each Func is released by its finalizer once the
// fired callback becomes unreachable — so 60 Hz stays cheap.
func timeAfter(ms int) chan struct{} {
	ch := make(chan struct{})
	js.Global().Call("setTimeout", js.FuncOf(func(_ js.Value, _ []js.Value) any {
		close(ch)
		return nil
	}), ms)
	return ch
}
