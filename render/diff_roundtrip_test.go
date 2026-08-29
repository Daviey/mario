package render

import (
	"testing"
	"unicode/utf8"

	"github.com/Daviey/mario/engine"
)

// termModel is a minimal ANSI terminal: just enough state (cells, cursor,
// deferred wrap, SGR) to replay the exact byte stream Diff produces and
// reconstruct the visible screen. It is the contract checker for the
// differential encoder: if cursor movement, bridging or style carry-over
// ever misplaces a cell or drops an SGR, the round-trip compare fails.
type termModel struct {
	w, h  int
	cells []Cell
	x, y  int
	pend  bool // deferred wrap after writing the last column
	fg    Color
	bg    Color
	bold  bool
}

func newTermModel(w, h int) *termModel {
	return &termModel{w: w, h: h, cells: make([]Cell, w*h)}
}

func (tm *termModel) put(rn rune) {
	if tm.pend {
		tm.x = 0
		tm.y++
		tm.pend = false
	}
	tm.cells[tm.y*tm.w+tm.x] = Cell{Ch: rn, Fg: tm.fg, Bg: tm.bg, Bold: tm.bold}
	if tm.x == tm.w-1 {
		tm.pend = true
	} else {
		tm.x++
	}
}

func (tm *termModel) feed(s string) {
	for i := 0; i < len(s); {
		switch c := s[i]; c {
		case 0x1b:
			if i+2 >= len(s) || s[i+1] != '[' {
				i++
				continue
			}
			j := i + 2
			if j < len(s) && s[j] == '?' { // private mode (sync output)
				j++
			}
			k := j
			for k < len(s) && (s[k] >= '0' && s[k] <= '9' || s[k] == ';') {
				k++
			}
			if k >= len(s) {
				return
			}
			params := csiParams(s[j:k])
			switch s[k] {
			case 'H':
				tm.y = clampInt(posParam(params, 0), 1, tm.h) - 1
				tm.x = clampInt(posParam(params, 1), 1, tm.w) - 1
				tm.pend = false
			case 'C':
				tm.x += posParam(params, 0)
				if tm.x >= tm.w {
					tm.x = tm.w - 1
				}
				tm.pend = false
			case 'm':
				tm.sgr(params)
			}
			i = k + 1
		case '\r':
			tm.x = 0
			tm.pend = false
			i++
		case '\n':
			tm.y++
			i++
		default:
			rn, sz := utf8.DecodeRuneInString(s[i:])
			tm.put(rn)
			i += sz
		}
	}
}

// csiParams splits a CSI parameter list, preserving zero values (SGR 0 is
// a reset, not a default).
func csiParams(s string) []int {
	if s == "" {
		return nil
	}
	out := []int{}
	n := 0
	for _, r := range s {
		if r == ';' {
			out = append(out, n)
			n = 0
			continue
		}
		n = n*10 + int(r-'0')
	}
	return append(out, n)
}

// posParam reads a cursor-addressing parameter, where zero/absent means 1.
func posParam(p []int, i int) int {
	if i >= len(p) || p[i] == 0 {
		return 1
	}
	return p[i]
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// sgr applies an SGR parameter list to the tracked colors.
func (tm *termModel) sgr(p []int) {
	for i := 0; i < len(p); i++ {
		switch v := p[i]; {
		case v == 0:
			tm.fg, tm.bg, tm.bold = Color{}, Color{}, false
		case v == 1:
			tm.bold = true
		case v == 38 && i+4 < len(p) && p[i+1] == 2:
			tm.fg = rgbColor(p[i+2], p[i+3], p[i+4])
			i += 4
		case v == 48 && i+4 < len(p) && p[i+1] == 2:
			tm.bg = rgbColor(p[i+2], p[i+3], p[i+4])
			i += 4
		case v >= 30 && v <= 37:
			tm.fg = Color{ANSI: v - 30}
		case v >= 90 && v <= 97:
			tm.fg = Color{ANSI: v - 90 + 8}
		case v >= 40 && v <= 47:
			tm.bg = Color{ANSI: v - 40}
		case v >= 100 && v <= 107:
			tm.bg = Color{ANSI: v - 100 + 8}
		}
	}
}

func rgbColor(r, g, b int) Color {
	return Color{RGB: RGB(uint32(r)<<16 | uint32(g)<<8 | uint32(b))}
}

// compare asserts the model matches the target screen. Foreground is only
// compared on non-space cells (the encoder leaves a space's foreground
// unset: it is invisible), and colors are compared only in the component
// the palette's mode actually encodes — a 16-color frame cannot carry RGB.
func (tm *termModel) compare(t *testing.T, want *Screen, trueColor bool) {
	t.Helper()
	sameColor := func(a, b Color) bool {
		if trueColor {
			return a.RGB == b.RGB
		}
		return a.ANSI == b.ANSI
	}
	for y := range want.H {
		for x := range want.W {
			wc := want.cells[y*want.W+x]
			got := tm.cells[y*tm.w+x]
			if got.Ch != wc.Ch || !sameColor(got.Bg, wc.Bg) || got.Bold != wc.Bold ||
				(wc.Ch != ' ' && !sameColor(got.Fg, wc.Fg)) {
				t.Fatalf("cell (%d,%d) = %q fg=%v bg=%v bold=%v, want %q fg=%v bg=%v bold=%v",
					x, y, got.Ch, got.Fg, got.Bg, got.Bold, wc.Ch, wc.Fg, wc.Bg, wc.Bold)
			}
		}
	}
}

// TestDiffStreamRoundTrip replays a scripted game's full Diff stream —
// first frame is a full repaint, everything after is differential — into
// the terminal model and requires the visible cells to match a direct
// render at regular intervals. Color modes exercise both SGR encodings.
func TestDiffStreamRoundTrip(t *testing.T) {
	for _, tc := range []struct {
		name string
		pal  *Palette
	}{
		{"truecolor", NewPalette(true)},
		{"basic", NewPalette(false)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const viewW, viewH = 20, 9
			g := engine.NewGame(engine.DefaultLevels(), viewW, viewH)
			tm := newTermModel(viewW*Pix, 2+viewH*Pix/2)
			var prev *Screen
			for i := range 1500 {
				g.Update(benchScript(i))
				cur := Render(g, tc.pal)
				tm.feed(Diff(prev, cur))
				prev = cur
				if i%97 == 0 {
					tm.compare(t, cur, tc.pal.TrueColor)
				}
			}
			tm.compare(t, prev, tc.pal.TrueColor)
		})
	}
}
