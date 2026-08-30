package render

import (
	"io"
	"strconv"
	"strings"

	"github.com/Daviey/mario/engine"
)

// Synchronized-output mode: the terminal buffers writes between the two
// sequences and applies them in one atomic repaint, eliminating tearing
// when a frame lands mid-refresh. Unsupported terminals ignore the mode.
const (
	syncBegin = "\x1b[?2026h"
	syncEnd   = "\x1b[?2026l"

	// bridgeMax bounds how many clean cells Diff may re-write instead of
	// paying for a cursor address. A clean cell costs at least one byte,
	// so beyond this a cursor move always wins.
	bridgeMax = 12
)

// Diff returns the ANSI snippet that updates the terminal contents from
// prev to next:
//
//   - prev == nil, or size/color-mode mismatch -> a synchronized full
//     repaint (cursor home + every cell).
//   - otherwise -> one run per span of changed cells, wrapped in
//     synchronized-output mode. Cursor movement between runs picks
//     the cheapest form (re-writing a few clean bridge cells, a relative
//     forward move, "\r\n" onto the next row, or an absolute address),
//     and style state carries across runs, so each run only pays for the
//     SGR parameters that actually changed.
//   - "" when nothing changed (callers skip the write entirely).
//
// This keeps per-frame output proportional to what actually moved, which
// is what makes 60 fps play well over an SSH link.
func Diff(prev, next *Screen) string {
	if next == nil {
		return ""
	}
	if prev == nil || prev.W != next.W || prev.H != next.H || prev.TrueColor != next.TrueColor {
		var b strings.Builder
		b.WriteString(syncBegin)
		// A size change means cells painted by the old frame may sit
		// outside the new one (the window shrank) — no diff can reach
		// them, so start from a blank screen.
		if prev != nil && (prev.W != next.W || prev.H != next.H) {
			b.WriteString("\x1b[2J")
		}
		b.WriteString(next.String())
		b.WriteString(syncEnd)
		return b.String()
	}

	var b strings.Builder
	st := sgrState{tc: next.TrueColor}
	lastY, lastX := -1, 0 // cursor sits just past the last emitted run
	dirty := false
	for y := range next.H {
		x := 0
		for x < next.W {
			if next.cells[y*next.W+x] == prev.cells[y*next.W+x] {
				x++
				continue
			}
			run := x
			for x < next.W && next.cells[y*next.W+x] != prev.cells[y*next.W+x] {
				x++
			}
			if !dirty {
				b.WriteString(syncBegin)
				dirty = true
			}
			emitRun(&b, next, &st, lastY, lastX, y, run, x)
			lastY, lastX = y, x
		}
	}
	if !dirty {
		return ""
	}
	b.WriteString("\x1b[0m")
	b.WriteString(syncEnd)
	return b.String()
}

// emitRun writes the dirty span [from,to) on row y, choosing the cheapest
// cursor movement by exact serialized cost: bridging over clean cells can
// cost more than a cursor address in glyphs yet still win by carrying the
// style state into the run. st is advanced to the state after the run in
// every path — the terminal and the tracker must agree.
func emitRun(b *strings.Builder, s *Screen, st *sgrState, lastY, lastX, y, from, to int) {
	var buf [16]byte
	cursor := csiCursor(buf[:0], y+1, from+1)
	tryBridge := false
	lead := "" // positions the cursor at the bridge start (next-row case)
	switch {
	case lastY < 0:
		b.WriteString(cursor)
		writeRun(b, s, y, from, to, st)
		return
	case y == lastY && from > lastX:
		gap := from - lastX
		if cuf := "\x1b[" + strconv.Itoa(gap) + "C"; len(cuf) < len(cursor) {
			cursor = cuf
		}
		tryBridge = gap <= bridgeMax
	case y == lastY+1 && from <= bridgeMax:
		if from == 0 {
			// Continuation row — two bytes instead of a cursor address,
			// and immune to the deferred-wrap state a full-width run
			// leaves behind.
			b.WriteString("\r\n")
			writeRun(b, s, y, from, to, st)
			return
		}
		lead = "\r\n"
		tryBridge = true
	}
	if !tryBridge {
		b.WriteString(cursor)
		writeRun(b, s, y, from, to, st)
		return
	}
	bridgeFrom := 0
	if y == lastY {
		bridgeFrom = lastX
	}
	bridge, bSt := bridgeRun(s, y, bridgeFrom, from, st) // bSt: after bridge
	bridged, bAfter := serializeRun(s, y, from, to, bSt) // bAfter: after run
	direct, dAfter := serializeRun(s, y, from, to, *st)  // dAfter: after run
	if len(lead)+len(bridge)+len(bridged) < len(cursor)+len(direct) {
		b.WriteString(lead)
		b.WriteString(bridge)
		b.WriteString(bridged)
		*st = bAfter
	} else {
		b.WriteString(cursor)
		b.WriteString(direct)
		*st = dAfter
	}
}

// csiCursor builds "\x1b[<row>;<col>H" into buf with hand-rolled itoa —
// run addressing runs per dirty span, so Sprintf's reflection cost adds
// up on busy frames.
func csiCursor(buf []byte, row, col int) string {
	buf = append(buf, "\x1b["...)
	buf = strconv.AppendInt(buf, int64(row), 10)
	buf = append(buf, ';')
	buf = strconv.AppendInt(buf, int64(col), 10)
	buf = append(buf, 'H')
	return string(buf)
}

// bridgeRun serializes the clean cells [from,to) on row y from a copy of
// the style state; the cells are identical in prev and next, so writing
// them is a visual no-op.
func bridgeRun(s *Screen, y, from, to int, st *sgrState) (string, sgrState) {
	sim := *st
	var b strings.Builder
	writeRun(&b, s, y, from, to, &sim)
	return b.String(), sim
}

// serializeRun renders the span [from,to) on row y from a copy of st,
// returning the bytes and the state after them.
func serializeRun(s *Screen, y, from, to int, st sgrState) (string, sgrState) {
	var b strings.Builder
	writeRun(&b, s, y, from, to, &st)
	return b.String(), st
}

// Stream renders successive game frames with differential output.
// Reset forces the next Draw to emit a full repaint (used when the
// terminal has been cleared or refitted out from under us).
type Stream struct {
	out  io.Writer
	pal  *Palette
	prev *Screen
}

// NewStream wraps a writer; frames are drawn with the given palette.
func NewStream(out io.Writer, pal *Palette) *Stream { return &Stream{out: out, pal: pal} }

// Reset drops the diff baseline.
func (s *Stream) Reset() { s.prev = nil }

// Snapshot renders one frame to a Screen without writing anything — for
// callers that hand the frame to another goroutine (Flush). Rendering
// reads engine state, so it must run where the game is quiescent.
func (s *Stream) Snapshot(g *engine.Game, ui ...*ScoreUI) *Screen {
	return Render(g, s.pal, ui...)
}

// Flush diffs cur against the last flushed screen and writes the update.
// Safe to call from a different goroutine than Snapshot (and one at a
// time): Snapshot only reads the palette, Flush alone touches prev/out.
func (s *Stream) Flush(cur *Screen) {
	if d := Diff(s.prev, cur); d != "" {
		io.WriteString(s.out, d)
	}
	s.prev = cur
}

// Draw renders one frame, writing only what changed.
func (s *Stream) Draw(g *engine.Game, ui ...*ScoreUI) { s.Flush(s.Snapshot(g, ui...)) }

// writeRun emits one contiguous span of cells [from,to) on row y, keeping
// the frame-wide style state — the caller resets the terminal once after
// the last run, not per run.
func writeRun(b *strings.Builder, s *Screen, y, from, to int, st *sgrState) {
	for x := from; x < to; x++ {
		c := s.cells[y*s.W+x]
		st.transition(b, c)
		b.WriteRune(c.Ch)
	}
}
