//go:build js

package main

import (
	"syscall/js"

	"mario/engine"
	"mario/input"
	"mario/render"
)

// Browser (WASM) entry point. The game runs fully client-side and paints
// its pixel buffer straight onto a canvas owned by the page — no terminal
// emulation, so there are no font metrics, no cell grids and no escape
// parsing to artifact.
//
// Page contract (all relative-safe; the page owns presentation):
//
//	window.marioFrame(w, h int, rgb []byte)
//	                            - set BEFORE instantiation; receives each
//	                            frame as tight RGB (w*h*3).
//	marioFeed(string)          - exported here; the page sends xterm/kitty
//	                            style key sequences (press and release).
//	marioSize(worldPxW, worldPxH)
//	                            - exported here; sizes the game viewport in
//	                            world pixels (tiles = px/4).

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
	b := f.RGBBytes()
	js.CopyBytesToJS(j.buf, b)
	js.Global().Call("marioFrame", f.W, f.H, j.buf)
}

func main() {
	mapper := input.NewMapper()

	js.Global().Set("marioFeed", js.FuncOf(func(_ js.Value, args []js.Value) any {
		mapper.Feed([]byte(args[0].String()))
		return nil
	}))

	g := engine.NewGame(engine.DefaultLevels(), 40, engine.LevelHeight)

	// Live viewport: the page reports how many world pixels it can show;
	// tiles = pixels/4. Never below the playable minimum.
	js.Global().Set("marioSize", js.FuncOf(func(_ js.Value, args []js.Value) any {
		w := args[0].Int() / render.Pix
		h := (args[1].Int() - render.HudBandPx - render.StatusBandPx) / render.Pix
		if w >= 16 && w <= 60 {
			g.ViewW = w
		}
		if h >= 4 && h <= g.Level.Height {
			g.ViewH = h
		}
		jsFrameSink = jsRGB{} // frame size changes: reallocate on next deliver
		return nil
	}))

	pal := render.NewPalette(true)
	draw := func() { jsFrameSink.deliver(render.RenderPixels(g, pal)) }
	draw() // first frame

	ticker := newTicker(engine.TicksPerSecond)
	for {
		ticker.wait()
		in := mapper.Poll()
		g.Update(in)
		draw()
		if in.Quit {
			break
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

func timeAfter(ms int) chan struct{} {
	ch := make(chan struct{})
	js.Global().Call("setTimeout", js.FuncOf(func(_ js.Value, _ []js.Value) any {
		close(ch)
		return nil
	}), ms)
	return ch
}
