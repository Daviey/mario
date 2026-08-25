package render

// ScoreUI rendering tests: every mode must render without panic across the
// playable viewport range, text must stay inside the frame, and the entry
// cursor must land within bounds.

import (
	"fmt"
	"testing"

	"mario/engine"
)

func uiGame(t *testing.T) *engine.Game {
	t.Helper()
	g := engine.NewGame(engine.DefaultLevels(), 40, engine.LevelHeight)
	for t := range 6000 {
		g.Update(engine.Input{
			Right:  true,
			Run:    t%3 != 0,
			Up:     t%97 < 22,
			AnyKey: t == 0,
		})
	}
	if g.State != engine.StateGameOver {
		t.Fatalf("want game over, got %v", g.State)
	}
	return g
}

func bigRows(n int) []ScoreRow {
	rows := make([]ScoreRow, n)
	for i := range n {
		rows[i] = ScoreRow{Rank: i + 1, Name: fmt.Sprintf("P%d", i+1), Score: 30000 - i*100, Mine: i == 2}
	}
	return rows
}

func TestScoreUIRendersAllModes(t *testing.T) {
	g := uiGame(t)
	pal := NewPalette(true)
	for _, mode := range []UIMode{UIAsk, UIEntry, UIBoard} {
		ui := &ScoreUI{Mode: mode, Score: 12500, Name: "DAVE", CursorOn: true, Status: "SUBMITTED!"}
		if s := Render(g, pal, ui); s == nil {
			t.Fatalf("mode %v rendered nil", mode)
		}
	}
}

func TestScoreUIAllViewportSizes(t *testing.T) {
	// No viewport size may panic mid-game; drawing clips out-of-bounds
	// writes, so a crash here means a negative/unclamped coordinate.
	g := uiGame(t)
	pal := NewPalette(true)
	for viewW := 16; viewW <= 60; viewW += 4 {
		for viewH := 4; viewH <= engine.LevelHeight; viewH++ {
			g.ViewW, g.ViewH = viewW, viewH
			for _, mode := range []UIMode{UIAsk, UIEntry, UIBoard} {
				ui := &ScoreUI{Mode: mode, Score: 999999, Name: "8CHARSLN", Rows: bigRows(30)}
				_ = Render(g, pal, ui)
				_ = RenderPixels(g, pal, ui)
			}
		}
	}
}

func TestBoardHeaderFontPixelsPresent(t *testing.T) {
	g := uiGame(t)
	pal := NewPalette(true)
	f := worldFrame(g, pal, &ScoreUI{
		Mode: UIBoard,
		Rows: []ScoreRow{{Rank: 1, Name: "DAVE", Score: 12500}, {Rank: 2, Name: "KIM", Score: 900}},
	})
	// Header "LEADERBOARD" centered at top: some pixel in its span must be
	// non-sky at the header row.
	x := (f.W - textWidthPx("LEADERBOARD", 1)) / 2
	if !anyPixel(f, x, 4, textWidthPx("LEADERBOARD", 1), pal.Sky) {
		t.Fatalf("no header pixels at x=%d row=4", x)
	}
}

func TestEntryCursorWithinFrame(t *testing.T) {
	g := uiGame(t)
	f := worldFrame(g, NewPalette(true), &ScoreUI{Mode: UIEntry, Name: "ABCDEFGH", CursorOn: true})
	// The cursor is a 1x5 gold block right after "[ABCDEFGH". Compute the
	// same way the drawer does and assert it lands inside the frame.
	w := textWidthPx("[ABCDEFGH", 1) + 1
	x := (f.W - textWidthPx("[ABCDEFGH]", 1)) / 2
	if x+w < 0 || x+w >= f.W {
		t.Fatalf("cursor column %d outside frame width %d", x+w, f.W)
	}
}

func anyPixel(f *Frame, x, y, w int, bg Color) bool {
	for dx := range w {
		if f.At(x+dx, y) != bg {
			return true
		}
	}
	return false
}
