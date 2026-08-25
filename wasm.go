//go:build js

package main

import (
	"syscall/js"

	"mario/engine"
	"mario/input"
	"mario/render"
)

// Browser (WASM) entry point. The game runs fully client-side: the page
// owns the terminal emulator (ghostty-web) and bridges keystrokes in and
// ANSI frames out through JS globals:
//
//	window.marioOut(string)   - set by the page BEFORE instantiation;
//	                            receives every frame (always valid UTF-8).
//	marioFeed(string)         - exported here; the page calls it with
//	                            xterm-style key/escape strings.
//	window.marioCols          - optional; set before instantiation to size
//	                            the viewport (tiles = cols/4, clamped).
//	window.marioTrueColor     - optional; defaults to true.

// jsWriter forwards ANSI frames to the page's marioOut callback.
type jsWriter struct{}

func (jsWriter) Write(p []byte) (int, error) {
	js.Global().Call("marioOut", string(p))
	return len(p), nil
}

func main() {
	mapper := input.NewMapper()

	js.Global().Set("marioFeed", js.FuncOf(func(_ js.Value, args []js.Value) any {
		mapper.Feed([]byte(args[0].String()))
		return nil
	}))

	viewW := 20
	if v := js.Global().Get("marioCols"); v.Type() == js.TypeNumber {
		if n := v.Int() / render.Pix; n >= 16 {
			viewW = n
		}
		if viewW > 60 {
			viewW = 60
		}
	}
	viewH := 9
	if v := js.Global().Get("marioRows"); v.Type() == js.TypeNumber {
		if n := (v.Int() - 2) / 2; n >= 4 {
			viewH = n
		}
	}
	trueColor := true
	if v := js.Global().Get("marioTrueColor"); v.Type() == js.TypeBoolean {
		trueColor = v.Bool()
	}

	g := engine.NewGame(engine.DefaultLevels(), viewW, viewH)

	// Live viewport: the page reports the fitted terminal grid; a bigger
	// window shows more world at the same sprite scale (the renderer and
	// camera clamps read these fields every frame).
	js.Global().Set("marioResize", js.FuncOf(func(_ js.Value, args []js.Value) any {
		w := args[0].Int() / render.Pix
		h := (args[1].Int() - 2) / 2
		if w >= 16 && w <= 60 {
			g.ViewW = w
		}
		if h >= 4 && h <= g.Level.Height {
			g.ViewH = h
		}
		return nil
	}))

	st := render.NewStream(jsWriter{}, render.NewPalette(trueColor))
	marioRepaint := js.FuncOf(func(_ js.Value, _ []js.Value) any {
		st.Reset() // terminal was refit/cleared: next frame is a full repaint
		return nil
	})
	js.Global().Set("marioRepaint", marioRepaint)
	if hook := js.Global().Get("__setRepaint"); hook.Type() == js.TypeFunction {
		hook.Invoke(marioRepaint)
	}

	play(g, mapper, st)

	// Quit/game-over: hold the module so the final frame stays on screen;
	// the page offers a reload.
	select {}
}
