package render

import (
	"testing"

	"github.com/Daviey/mario/engine"
)

// benchScript mirrors internal/ui.ScriptInput (the attract/demo input):
// hold right, run most ticks, hop regularly, dismiss the title on tick 0.
func benchScript(t int) engine.Input {
	return engine.Input{
		Right:  true,
		Run:    t%3 != 0,
		Up:     t%97 < 22,
		AnyKey: t == 0,
	}
}

type countWriter struct{ n int }

func (w *countWriter) Write(p []byte) (int, error) { w.n += len(p); return len(p), nil }

// driveBandwidth plays ticks of scripted input through a Stream and returns
// the total bytes written.
func driveBandwidth(t *testing.T, viewW, viewH, ticks int, pal *Palette) int {
	t.Helper()
	g := engine.NewGame(engine.DefaultLevels(), viewW, viewH)
	w := &countWriter{}
	st := NewStream(w, pal)
	for i := range ticks {
		g.Update(benchScript(i))
		st.Draw(g)
	}
	return w.n
}

// TestStreamBandwidth measures the steady-state ANSI output cost of play at
// 60 Hz on a full-screen terminal viewport. The caps are generous guards
// against encoding regressions (a change that doubles per-frame bytes fails
// here long before anyone notices a laggy SSH link), not tight budgets.
func TestStreamBandwidth(t *testing.T) {
	const (
		viewW = 33 // 198 columns on a ~200-col terminal
		viewH = 9  // 29 screen rows
		ticks = 1800
	)
	for _, tc := range []struct {
		name string
		pal  *Palette
		cap  int // bytes per tick ceiling
	}{
		{"truecolor", NewPalette(Colors24), 1500},
		{"cube256", NewPalette(Colors256), 1000},
		{"basic", NewPalette(Colors16), 700},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bytes := driveBandwidth(t, viewW, viewH, ticks, tc.pal)
			perTick := bytes / ticks
			t.Logf("%s: %d bytes over %d ticks = %d B/tick (~%d KiB/s @60fps)",
				tc.name, bytes, ticks, perTick, perTick*60/1024)
			if perTick > tc.cap {
				t.Errorf("%s output = %d B/tick, want <= %d (bandwidth regression)",
					tc.name, perTick, tc.cap)
			}
		})
	}
}
