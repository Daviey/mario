package render

import (
	"fmt"
	"io"
	"strings"

	"github.com/Daviey/mario/engine"
)

// Synchronized-output mode: the terminal buffers writes between the two
// sequences and applies them in one atomic repaint, eliminating tearing
// when a frame lands mid-refresh. Unsupported terminals ignore the mode.
const (
	syncBegin = "\x1b[?2026h"
	syncEnd   = "\x1b[?2026l"
)

// Diff returns the ANSI snippet that updates the terminal contents from
// prev to next:
//
//   - prev == nil, or size/color-mode mismatch -> a synchronized full
//     repaint (cursor home + every cell).
//   - otherwise -> one cursor-addressed run per span of changed cells,
//     wrapped in synchronized-output mode.
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
	dirty := false
	for y := 0; y < next.H; y++ {
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
			fmt.Fprintf(&b, "\x1b[%d;%dH", y+1, run+1)
			writeRun(&b, next, y, run, x)
		}
	}
	if !dirty {
		return ""
	}
	b.WriteString(syncEnd)
	return b.String()
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

// Draw renders one frame, writing only what changed.
func (s *Stream) Draw(g *engine.Game, ui ...*ScoreUI) {
	cur := Render(g, s.pal, ui...)
	if d := Diff(s.prev, cur); d != "" {
		io.WriteString(s.out, d)
	}
	s.prev = cur
}

// writeRun emits one contiguous span of cells [from,to) on row y, starting
// from a fresh style state.
func writeRun(b *strings.Builder, s *Screen, y, from, to int) {
	have := false
	var pFg, pBg Color
	pBold := false
	for x := from; x < to; x++ {
		c := s.cells[y*s.W+x]
		if !have || c.Fg != pFg || c.Bg != pBg || c.Bold != pBold {
			b.WriteString(s.styleSeq(c))
			pFg, pBg, pBold, have = c.Fg, c.Bg, c.Bold, true
		}
		b.WriteRune(c.Ch)
	}
	b.WriteString("\x1b[0m")
}
