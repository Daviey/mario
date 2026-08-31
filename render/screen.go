package render

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"
)

// Cell is one character position with styling.
type Cell struct {
	Ch   rune
	Fg   Color
	Bg   Color
	Bold bool
}

// Screen is a rectangular cell buffer plus ANSI serialization.
type Screen struct {
	W, H   int
	cells  []Cell
	Colors ColorMode // Colors24, Colors256 or Colors16
}

// NewScreen returns a screen with every cell blank black.
func NewScreen(w, h int) *Screen {
	return refillScreen(nil, w, h)
}

// refillScreen returns a w×h screen with every cell blank, allocating
// only when s is nil or the wrong size. The total blank matters as much
// as the reuse: recycled screens must be indistinguishable from fresh
// ones even when the next frame leaves cells unwritten.
func refillScreen(s *Screen, w, h int) *Screen {
	if s == nil || s.W != w || s.H != h {
		s = &Screen{W: w, H: h, cells: make([]Cell, w*h)}
	}
	blank := Cell{Ch: ' '}
	for i := range s.cells {
		s.cells[i] = blank
	}
	return s
}

// Set writes a foreground-colored cell, clipping out-of-bounds writes.
func (s *Screen) Set(x, y int, ch rune, fg Color) {
	s.SetStyled(x, y, ch, fg, Color{}, false)
}

// SetStyled writes a fully styled cell, clipping out-of-bounds writes.
func (s *Screen) SetStyled(x, y int, ch rune, fg, bg Color, bold bool) {
	if x < 0 || x >= s.W || y < 0 || y >= s.H {
		return
	}
	s.cells[y*s.W+x] = Cell{Ch: ch, Fg: fg, Bg: bg, Bold: bold}
}

// At returns the cell at a position (zero Cell when out of bounds).
func (s *Screen) At(x, y int) Cell {
	if x < 0 || x >= s.W || y < 0 || y >= s.H {
		return Cell{}
	}
	return s.cells[y*s.W+x]
}

// Text writes a string in one color on black. Columns advance one per
// rune: a multibyte rune must not shift every later glyph one column
// per byte.
func (s *Screen) Text(x, y int, text string, fg Color) {
	cx := x
	for _, r := range text {
		s.SetStyled(cx, y, r, fg, Color{}, false)
		cx++
	}
}

// TextStyled writes a styled string.
func (s *Screen) TextStyled(x, y int, text string, fg, bg Color, bold bool) {
	cx := x
	for _, r := range text {
		s.SetStyled(cx, y, r, fg, bg, bold)
		cx++
	}
}

// Center writes text centered on row y.
func (s *Screen) Center(y int, text string, fg, bg Color, bold bool) {
	x := (s.W - utf8.RuneCountInString(text)) / 2
	if x < 0 {
		x = 0
	}
	s.TextStyled(x, y, text, fg, bg, bold)
}

// rowString returns the runes of one row (test helper).
func (s *Screen) rowString(y int) string {
	if y < 0 || y >= s.H {
		return ""
	}
	row := make([]rune, s.W)
	for x := 0; x < s.W; x++ {
		row[x] = s.cells[y*s.W+x].Ch
	}
	return string(row)
}

// String serializes the screen as one ANSI frame: cursor home, every cell
// with only the SGR parameters that changed since the previous cell, reset
// at the end. Style state deliberately persists across row breaks — the
// terminal keeps SGR across line feeds, so starting a row in the style the
// previous row ended in costs nothing.
func (s *Screen) String() string {
	var b strings.Builder
	b.WriteString("\x1b[H")
	st := sgrState{mode: s.Colors}
	for y := range s.H {
		for x := range s.W {
			c := s.cells[y*s.W+x]
			st.transition(&b, c)
			b.WriteRune(c.Ch)
		}
		if y < s.H-1 {
			b.WriteString("\r\n")
		}
	}
	b.WriteString("\x1b[0m")
	return b.String()
}

// sgrState mirrors the terminal's current SGR state so successive cells,
// runs and rows emit only the SGR parameters that actually changed — style
// sequences dominate a frame's byte cost, so this is where the SSH
// bandwidth goes. A space paints nothing but its background, so its
// foreground is a don't-care and never costs bytes.
type sgrState struct {
	mode ColorMode // Colors24/256/16 — which SGR encoding the terminal takes
	have bool      // any style emitted yet (terminal starts at default)
	fg   Color
	bg   Color
	bold bool
}

func isDefaultColor(c Color) bool { return c.RGB == 0 && c.ANSI == 0 }

// colorParams returns the SGR parameters that set c as foreground
// (bg=false) or background in the given color mode. Every mode is
// table-driven so the per-changed-cell wire path never formats a string:
// basic mode reads basicSGR (every ANSI-16 code, computed once at init),
// 24-bit and 256-cube modes read sgrMem (memoized per Color the first
// time it is emitted — in practice at palette construction, once per
// process).
func colorParams(mode ColorMode, c Color, bg bool) string {
	switch mode {
	case Colors24, Colors256:
		if s, ok := sgrMem.Load(sgrKey{c: c, mode: mode, bg: bg}); ok {
			return s.(string)
		}
		base := 38
		if bg {
			base = 48
		}
		var str string
		if mode == Colors24 {
			str = fmt.Sprintf("%d;2;%d;%d;%d", base, c.RGB>>16, (c.RGB>>8)&0xFF, c.RGB&0xFF)
		} else {
			str = fmt.Sprintf("%d;5;%d", base, c.Idx256)
		}
		sgrMem.Store(sgrKey{c: c, mode: mode, bg: bg}, str)
		return str
	}
	// Basic mode: every Color's ANSI index is 0-15 (color() assigns
	// each swatch one; the zero Color is 0), so this is a plain table
	// read with no clamping.
	return basicSGR[c.ANSI][b2i(bg)]
}

// basicSGR holds every ANSI-16 SGR parameter string (fg and bg) so basic
// mode is a plain array read.
var basicSGR [16][2]string

func init() {
	for i := range 16 {
		basicSGR[i][0] = ansiCode(i, false)
		basicSGR[i][1] = ansiCode(i, true)
	}
}

// sgrMem memoizes 24-bit and 256-cube SGR parameter strings per
// (Color, mode, role) — deterministic values, written once per unique
// combination.
var sgrMem sync.Map // sgrKey -> string

type sgrKey struct {
	c    Color
	mode ColorMode
	bg   bool
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// transition appends the minimal SGR sequence that renders cell c and
// updates the tracked state.
func (st *sgrState) transition(b *strings.Builder, c Cell) {
	dBg, dBold := c.Bg, c.Bold
	dFg := c.Fg
	if c.Ch == ' ' {
		dFg = st.fg // invisible on a space: keep whatever is already set
	}
	if st.have && st.sameColor(dFg, st.fg) && st.sameColor(dBg, st.bg) && dBold == st.bold {
		return
	}
	// Attributes that must be cleared (bold off, a color returning to the
	// terminal default) have no cheap additive form: fall back to a full
	// "0;..." reset+set.
	needClear := st.have &&
		((!dBold && st.bold) ||
			(isDefaultColor(dFg) && !isDefaultColor(st.fg)) ||
			(isDefaultColor(dBg) && !isDefaultColor(st.bg)))
	if !st.have || needClear {
		parts := []string{"0"}
		if dBold {
			parts = append(parts, "1")
		}
		if c.Ch != ' ' && !isDefaultColor(c.Fg) {
			parts = append(parts, colorParams(st.mode, c.Fg, false))
		}
		if !isDefaultColor(dBg) {
			parts = append(parts, colorParams(st.mode, dBg, true))
		}
		b.WriteString("\x1b[" + strings.Join(parts, ";") + "m")
		if c.Ch == ' ' {
			dFg = Color{} // the full form omits fg: terminal is at default
		}
	} else {
		var parts []string
		if !st.sameColor(dFg, st.fg) {
			parts = append(parts, colorParams(st.mode, dFg, false))
		}
		if !st.sameColor(dBg, st.bg) {
			parts = append(parts, colorParams(st.mode, dBg, true))
		}
		if dBold && !st.bold {
			parts = append(parts, "1")
		}
		b.WriteString("\x1b[" + strings.Join(parts, ";") + "m")
	}
	st.fg, st.bg, st.bold, st.have = dFg, dBg, dBold, true
}

// sameColor reports whether two colors are indistinguishable on the wire
// in this color mode — the comparison must match what is actually
// emitted. Basic mode sends only the ANSI index, so same-index colors
// (GroundMid/GroundDark both 1, Goomba/BrickLight both 3) are the same
// terminal color and must not re-emit SGR every frame; 256-cube mode
// sends only the cube index; truecolor compares the full pair. The
// terminal default is its own wire state (no SGR at all), so a default
// color never compares equal to a real one even when their indices match.
func (st *sgrState) sameColor(a, b Color) bool {
	da, db := isDefaultColor(a), isDefaultColor(b)
	if da || db {
		return da == db
	}
	switch st.mode {
	case Colors24:
		return a == b
	case Colors256:
		return a.Idx256 == b.Idx256
	}
	return a.ANSI == b.ANSI
}

func ansiCode(idx int, bg bool) string {
	base := 30
	if bg {
		base = 40
	}
	if idx >= 8 {
		base += 60
		idx -= 8
	}
	return fmt.Sprintf("%d", base+idx)
}

// blit packs two pixel rows per screen cell: differing halves ride the
// half block (fg = upper, bg = lower); a solid pair is a plain space over
// the colour — one byte on the wire instead of the block's three, and sky
// and other large fills are almost entirely solid pairs.
func blit(s *Screen, f *Frame) {
	worldRows := f.H / 2
	for cy := range worldRows {
		for x := range min(f.W, s.W) {
			upper := f.At(x, cy*2)
			lower := f.At(x, cy*2+1)
			if upper == lower {
				s.SetStyled(x, 1+cy, ' ', upper, upper, false)
				continue
			}
			s.SetStyled(x, 1+cy, '▀', upper, lower, false)
		}
	}
}

// Synchronized-output mode: the terminal buffers writes between the two
// sequences and applies them in one atomic repaint, eliminating tearing
// when a frame lands mid-refresh. Unsupported terminals ignore the mode.
const (
	syncBegin = "\x1b[?2026h"
	syncEnd   = "\x1b[?2026l"

	// bridgeMax bounds how many clean cells Diff may consider re-writing
	// instead of paying for a cursor address. It is a scan bound, not an
	// exact cost threshold: bridging can still win slightly past it by
	// carrying style state, but long bridges bloat the frame for little
	// gain, so the scan stops here.
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
	if prev == nil || prev.W != next.W || prev.H != next.H || prev.Colors != next.Colors {
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
	var scratch strings.Builder // emitRun's cost probes: reused, never read
	st := sgrState{mode: next.Colors}
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
			emitRun(&b, next, &st, lastY, lastX, y, run, x, &scratch)
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
//
// Candidates are costed by serializing them into scratch — one builder
// reused across the whole Diff, only the byte counts matter — and the
// winner is then serialized for real into b: the same bytes, since
// serialization is a pure function of the cells and the start state, but
// without materializing three throwaway strings per span.
func emitRun(b *strings.Builder, s *Screen, st *sgrState, lastY, lastX, y, from, to int, scratch *strings.Builder) {
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
	scratch.Reset()
	sim := *st
	writeRun(scratch, s, y, bridgeFrom, from, &sim)
	nBridge := scratch.Len()
	writeRun(scratch, s, y, from, to, &sim)
	nBridged := scratch.Len() - nBridge
	directSim := *st
	writeRun(scratch, s, y, from, to, &directSim)
	nDirect := scratch.Len() - nBridge - nBridged
	if len(lead)+nBridge+nBridged < len(cursor)+nDirect {
		b.WriteString(lead)
		writeRun(b, s, y, bridgeFrom, from, st)
		writeRun(b, s, y, from, to, st)
	} else {
		b.WriteString(cursor)
		writeRun(b, s, y, from, to, st)
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
