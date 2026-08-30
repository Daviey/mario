package render

import (
	"bytes"
	"io"
	"testing"

	"github.com/Daviey/mario/engine"
)

// Buffer recycling regression tests. The recycled paths (Stream's screen
// pool, worldFrame/renderInto refills, RenderPixelsInto, RGBBytesInto)
// must be observably identical to the allocating one-shots: a partial
// blank or a missed band would leak last tick's pixels into this one.
// (internal/ui cannot be imported here — it imports render — so these
// tests drive the game with a deterministic synthetic input instead of
// the demo script; identity is per-tick fresh-vs-reused, not gameplay.)

// scriptInput is a deterministic wiggle: run with a hop every so often,
// a suicide rarely, so states cycle through title, play, death, respawn.
func scriptInput(t int) engine.Input {
	return engine.Input{
		Right:   t%37 != 0,
		Up:      t%23 == 0 || t%23 == 1,
		Run:     t%11 == 0,
		Suicide: t == 400,
	}
}

func cellsEqual(a, b *Screen) bool {
	if a == nil || b == nil || a.W != b.W || a.H != b.H {
		return false
	}
	for i := range a.cells {
		if a.cells[i] != b.cells[i] {
			return false
		}
	}
	return true
}

// TestSnapshotRecycledMatchesFresh drives a scripted session (title,
// gameplay, death-respawn, a leaderboard screen) and requires every
// Stream.Snapshot — which recycles the pool's screens and the world
// raster — to match a fresh allocating render of the same tick cell for
// cell.
func TestSnapshotRecycledMatchesFresh(t *testing.T) {
	g := engine.NewGame(engine.DefaultLevels(), 40, engine.LevelHeight)
	g.Reset()
	st := NewStream(io.Discard, NewPalette(Colors24))
	scoreUI := &ScoreUI{Mode: UIAsk}

	for tick := 0; tick < 900; tick++ {
		g.Update(scriptInput(tick))
		want, _ := renderInto(nil, nil, g, st.pal)
		got := st.Snapshot(g)
		if !cellsEqual(want, got) {
			t.Fatalf("tick %d (state %s): recycled snapshot diverged from fresh render", tick, g.State)
		}
		wantUI, _ := renderInto(nil, nil, g, st.pal, scoreUI)
		gotUI := st.Snapshot(g, scoreUI)
		if !cellsEqual(wantUI, gotUI) {
			t.Fatalf("tick %d: recycled UI snapshot diverged from fresh render", tick)
		}
		st.Flush(gotUI)
	}
}

// TestRenderPixelsIntoMatchesFresh checks the pixel path (browser/EFI
// renderer) the same way, including a viewport resize mid-run — the
// refill must reallocate rather than paint the new frame with the old
// buffer's geometry.
func TestRenderPixelsIntoMatchesFresh(t *testing.T) {
	pal := NewPalette(Colors24)
	for _, viewW := range []int{20, 40, 60, 40} { // last one resizes back
		g := engine.NewGame(engine.DefaultLevels(), viewW, engine.LevelHeight)
		g.Reset()
		var dst, world *Frame
		var rgb []byte
		for tick := 0; tick < 300; tick++ {
			g.Update(scriptInput(tick))
			wantFresh := RenderPixels(g, pal)
			dst, world = RenderPixelsInto(dst, world, g, pal)
			if dst.W != wantFresh.W || dst.H != wantFresh.H {
				t.Fatalf("viewW %d tick %d: reused frame %dx%d, want %dx%d",
					viewW, tick, dst.W, dst.H, wantFresh.W, wantFresh.H)
			}
			gotRGB := dst.RGBBytes()
			rgb = dst.RGBBytesInto(rgb)
			if !bytes.Equal(gotRGB, wantFresh.RGBBytes()) || !bytes.Equal(rgb, wantFresh.RGBBytes()) {
				t.Fatalf("viewW %d tick %d: recycled pixel frame diverged from fresh render", viewW, tick)
			}
		}
	}
}

// TestStreamPoolRecyclesScreens pins the cycling itself: steady-state
// Snapshot+Flush must reuse the same handful of screens (no per-tick
// allocation), and a screen that is currently the diff baseline (prev)
// must never be handed out by the next Snapshot.
func TestStreamPoolRecyclesScreens(t *testing.T) {
	g := engine.NewGame(engine.DefaultLevels(), 40, engine.LevelHeight)
	g.Reset()
	st := NewStream(io.Discard, NewPalette(Colors16))
	for range 100 {
		g.Update(scriptInput(g.Tick))
		st.Draw(g) // warms prev
	}
	seen := map[*Screen]int{}
	for range 10 {
		g.Update(scriptInput(g.Tick))
		cur := st.Snapshot(g)
		if cur == st.prev {
			t.Fatal("Snapshot handed out the current diff baseline")
		}
		seen[cur]++
		st.Flush(cur)
	}
	if len(seen) > 3 {
		t.Fatalf("pool failed to recycle: %d distinct screens over 10 ticks", len(seen))
	}
}

// measuring bytes (the diff's output strings scale with game content —
// that's TestStreamBandwidth's domain): across a long steady run the
// world raster must be refilled in place (same *Frame throughout) and
// the screens must cycle through a tiny pool rather than allocate.
func TestStreamSteadyStateRecycles(t *testing.T) {
	g := engine.NewGame(engine.DefaultLevels(), 40, engine.LevelHeight)
	g.Reset()
	st := NewStream(io.Discard, NewPalette(Colors24))
	for range 100 {
		g.Update(scriptInput(g.Tick))
		st.Draw(g) // warms prev and the pool
	}
	world := st.world
	seen := map[*Screen]bool{}
	for range 200 {
		g.Update(scriptInput(g.Tick))
		st.Draw(g)
		if st.world != world {
			t.Fatal("world raster reallocated mid-run at a fixed viewport")
		}
		seen[st.prev] = true
	}
	if n := len(seen); n == 0 || n > 3 {
		t.Fatalf("steady state used %d distinct screens (want a 2-3 screen cycle)", n)
	}
}
