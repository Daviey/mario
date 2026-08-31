package render

import (
	"io"
	"sync"

	"github.com/Daviey/mario/engine"
)

// Stream renders successive game frames with differential output.
// Reset forces the next Draw to emit a full repaint (used when the
// terminal has been cleared or refitted out from under us).
type Stream struct {
	out  io.Writer
	pal  *Palette
	prev *Screen

	// world is the recycled world raster, touched only by the Snapshot
	// goroutine. Screens cycle through a small pool: Snapshot takes one,
	// Flush retires the previous baseline into it. The SSH host runs
	// Snapshot and Flush on different goroutines, so the pool is guarded.
	world *Frame
	mu    sync.Mutex
	pool  []*Screen
}

// NewStream wraps a writer; frames are drawn with the given palette.
func NewStream(out io.Writer, pal *Palette) *Stream { return &Stream{out: out, pal: pal} }

// Reset drops the diff baseline.
func (s *Stream) Reset() {
	s.putScreen(s.prev)
	s.prev = nil
}

// Snapshot renders one frame to a Screen without writing anything — for
// callers that hand the frame to another goroutine (Flush). Rendering
// reads engine state, so it must run where the game is quiescent.
// The returned Screen belongs to the Stream and is recycled after it
// has served as the diff baseline (the second following Flush returns
// it to the pool): treat it as read-only and transient, and copy
// anything that must outlive that.
func (s *Stream) Snapshot(g *engine.Game, ui ...*ScoreUI) *Screen {
	sc, world := renderInto(s.takeScreen(g), s.world, g, s.pal, ui...)
	s.world = world
	return sc
}

// Flush diffs cur against the last flushed screen and writes the update.
// Safe to call from a different goroutine than Snapshot (and one at a
// time): Snapshot only reads the palette and its own scratch, Flush
// alone touches prev/out.
func (s *Stream) Flush(cur *Screen) {
	if d := Diff(s.prev, cur); d != "" {
		io.WriteString(s.out, d)
	}
	s.putScreen(s.prev)
	s.prev = cur
}

// Draw renders one frame, writing only what changed.
func (s *Stream) Draw(g *engine.Game, ui ...*ScoreUI) { s.Flush(s.Snapshot(g, ui...)) }

// takeScreen returns a blank screen at the size the next frame needs,
// reusing a pooled one when a size matches.
func (s *Stream) takeScreen(g *engine.Game) *Screen {
	w, h := g.ViewW*Pix, 2+viewTilesOf(g)*Pix/2
	s.mu.Lock()
	for i, sc := range s.pool {
		if sc.W == w && sc.H == h {
			last := len(s.pool) - 1
			s.pool[i] = s.pool[last]
			s.pool = s.pool[:last]
			s.mu.Unlock()
			return refillScreen(sc, w, h)
		}
	}
	s.mu.Unlock()
	return NewScreen(w, h)
}

// putScreen retires a screen into the pool. The pool stays tiny: at
// most one baseline, one in flight and one superseded mailbox holdout
// exist at a time; anything beyond that is dropped for the GC.
func (s *Stream) putScreen(sc *Screen) {
	if sc == nil {
		return
	}
	s.mu.Lock()
	if len(s.pool) < 3 {
		s.pool = append(s.pool, sc)
	}
	s.mu.Unlock()
}
