package mario

import (
	"io"
	"testing"

	"github.com/Daviey/mario/engine"
	"github.com/Daviey/mario/internal/ui"
	"github.com/Daviey/mario/render"
)

// Benchmarks for the per-tick pipeline every surface runs 60 times a
// second: engine update → worldFrame snapshot → diff encode. The
// scripted session (ui.ScriptInput) is the same deterministic driver the
// demo and determinism tests use, so numbers are reproducible run to run.

// benchApp returns an app whose game is mid-level-1 gameplay, past the
// world card, with a diff stream already warmed (first full repaint
// done) — the steady state of a real session.
func benchApp(b *testing.B, viewW int) (*App, *render.Stream) {
	b.Helper()
	app := New(&Options{Levels: nil, ViewW: viewW})
	st := render.NewStream(io.Discard, render.NewPalette(render.Colors24))
	for t := 0; app.Game.State != engine.StatePlaying || t < 600; t++ {
		app.Game.Update(ui.ScriptInput(t))
		if t > 5000 {
			b.Fatal("script never reached gameplay")
		}
	}
	st.Snapshot(app.Game)
	return app, st
}

// BenchmarkEngineUpdate isolates the pure simulation: physics, entities,
// collisions. This is the part every surface shares.
func BenchmarkEngineUpdate(b *testing.B) {
	app, _ := benchApp(b, 40)
	g := app.Game
	b.ResetTimer()
	for i := range b.N {
		g.Update(ui.ScriptInput(600 + i))
	}
}

// BenchmarkSnapshot measures worldFrame construction: sprite blitting,
// palette mapping, overlay compositing — everything up to (but not
// including) the diff encoder.
func BenchmarkSnapshot(b *testing.B) {
	app, st := benchApp(b, 40)
	g := app.Game
	b.ResetTimer()
	for i := range b.N {
		g.Update(ui.ScriptInput(600 + i))
		st.Snapshot(g)
	}
}

// BenchmarkDiffEncode measures the wire half: diffing the previous
// screen against the current one and encoding minimal-diff ANSI (SGR
// state machine, gap bridging, cursor addressing). This is the bytes the
// terminal (or SSH client) actually receives.
func BenchmarkDiffEncode(b *testing.B) {
	app, st := benchApp(b, 40)
	g := app.Game
	b.ResetTimer()
	for i := range b.N {
		g.Update(ui.ScriptInput(600 + i))
		st.Flush(st.Snapshot(g))
	}
}

// BenchmarkDiffEncodeWide doubles the viewport width: per-tick cost
// should scale with cells, and the diff encoder's gap-bridging decisions
// change character at wider rows.
func BenchmarkDiffEncodeWide(b *testing.B) {
	app, st := benchApp(b, 80)
	g := app.Game
	b.ResetTimer()
	for i := range b.N {
		g.Update(ui.ScriptInput(600 + i))
		st.Flush(st.Snapshot(g))
	}
}

// BenchmarkStep exercises the full facade per tick: router polling,
// mapper decode, engine update, leaderboard UI tick — everything the
// 60 Hz loop does on the play side except the write syscall.
func BenchmarkStep(b *testing.B) {
	app, _ := benchApp(b, 40)
	b.ResetTimer()
	for range b.N {
		app.Step()
	}
}
