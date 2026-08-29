package render

import (
	"strings"
	"testing"

	"github.com/Daviey/mario/engine"
)

func mkScreen(w, h int, fill rune) *Screen {
	s := NewScreen(w, h)
	s.TrueColor = true
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			s.Set(x, y, fill, testPal.Text)
		}
	}
	return s
}

func TestDiffNoChangeIsEmpty(t *testing.T) {
	a := mkScreen(8, 3, 'x')
	if d := Diff(a, mkScreen(8, 3, 'x')); d != "" {
		t.Errorf("identical screens produced %q", d)
	}
	// Same via the actual render path: two renders without an update.
	g := newGame(t)
	if d := Diff(Render(g, testPal), Render(g, testPal)); d != "" {
		t.Errorf("identical frames produced %d bytes", len(d))
	}
}

func TestDiffNilPrevIsSyncedFullRepaint(t *testing.T) {
	d := Diff(nil, mkScreen(4, 2, 'q'))
	if !strings.HasPrefix(d, syncBegin) || !strings.HasSuffix(d, syncEnd) {
		t.Errorf("full repaint not wrapped in sync mode: %q", d)
	}
	if !strings.Contains(d, "\x1b[H") {
		t.Error("full repaint missing cursor home")
	}
	if !strings.Contains(d, "qqqq") {
		t.Error("full repaint missing content")
	}
}

func TestDiffSingleCellChange(t *testing.T) {
	a := mkScreen(10, 3, '.')
	b := mkScreen(10, 3, '.')
	b.Set(4, 1, 'Z', testPal.Text) // row 1, col 4 -> 1-based (2,5)
	d := Diff(a, b)
	if d == "" {
		t.Fatal("no diff for changed cell")
	}
	if !strings.HasPrefix(d, syncBegin) || !strings.HasSuffix(d, syncEnd) {
		t.Error("diff not wrapped in synchronized output")
	}
	if !strings.Contains(d, "\x1b[2;5H") {
		t.Errorf("missing 1-based cursor address for (row 1, col 4): %q", d)
	}
	if !strings.Contains(d, "Z") {
		t.Error("changed rune missing")
	}
	if strings.Contains(d, "\x1b[H") {
		t.Error("diff must not full-repaint")
	}
	if n := strings.Count(d, "Z"); n != 1 {
		t.Errorf("rune Z written %d times, want 1", n)
	}
}

func TestDiffTwoSpansSameRow(t *testing.T) {
	a := mkScreen(10, 2, '.')
	b := mkScreen(10, 2, '.')
	b.Set(1, 0, 'A', testPal.Text)
	b.Set(8, 0, 'B', testPal.Text)
	d := Diff(a, b)
	// Both spans are addressed (the second via the cheaper relative
	// forward move over the 6-cell gap), never a full repaint.
	if !strings.Contains(d, "\x1b[1;2H") {
		t.Errorf("first span not cursor-addressed: %q", d)
	}
	if !strings.Contains(d, "\x1b[6C") {
		t.Errorf("second span not reached by relative move: %q", d)
	}
	if strings.Contains(d, "\x1b[H") {
		t.Error("diff must not full-repaint")
	}
	if n := strings.Count(d, "A") + strings.Count(d, "B"); n != 2 {
		t.Errorf("span glyphs written %d times, want 2: %q", n, d)
	}
}

func TestDiffStyleOnlyChangeDetected(t *testing.T) {
	a := mkScreen(6, 2, 'x')
	b := mkScreen(6, 2, 'x')
	b.SetStyled(3, 1, 'x', testPal.FlagRed, testPal.HUDBG, true)
	if d := Diff(a, b); d == "" {
		t.Error("style-only change not detected (Cell compare must include color/bold)")
	}
}

func TestDiffSizeOrModeChangeFallsBackToFull(t *testing.T) {
	a := mkScreen(8, 3, 'x')
	if d := Diff(a, mkScreen(9, 3, 'x')); !strings.Contains(d, "\x1b[H") {
		t.Error("width change must trigger full repaint")
	}
	if d := Diff(a, mkScreen(8, 4, 'x')); !strings.Contains(d, "\x1b[H") {
		t.Error("height change must trigger full repaint")
	}
	basic := mkScreen(8, 3, 'x')
	basic.TrueColor = false
	if d := Diff(a, basic); !strings.Contains(d, "\x1b[H") {
		t.Error("color-mode change must trigger full repaint")
	}
}

func TestDiffNilNextIsEmpty(t *testing.T) {
	if d := Diff(mkScreen(4, 2, 'x'), nil); d != "" {
		t.Errorf("nil next produced %q", d)
	}
}

func TestDiffGameplayFramesAreSmall(t *testing.T) {
	// Two nearby gameplay frames should produce a diff far smaller than a
	// full frame: terrain and HUD are static; only animation-phase cells
	// (coin spin, block blink) and the timer move.
	g := newGame(t)
	g.Update(engine.Input{})
	s1 := Render(g, testPal)

	g.Tick += 7 // cross a coin-spin animation boundary deterministically
	g.Update(engine.Input{})
	s2 := Render(g, testPal)

	full := len(s2.String())
	d := len(Diff(s1, s2))
	if d == 0 {
		t.Fatal("frames across an animation boundary produced no diff")
	}
	if d > full/2 {
		t.Errorf("small-motion diff = %d bytes, full = %d; expected a fraction", d, full)
	}
}
