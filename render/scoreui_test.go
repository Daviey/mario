package render

// ScoreUI rendering tests: the screens are real text — every mode must
// render its strings as screen rows, survive the playable viewport range,
// and the entry name field must hold a constant width.

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Daviey/mario/engine"
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

func TestScoreUITextScreens(t *testing.T) {
	g := uiGame(t)
	pal := NewPalette(true)

	s := Render(g, pal, &ScoreUI{Mode: UIAsk, Score: 12500})
	found := false
	for y := 1; y < s.H-1; y++ {
		if strings.Contains(rowText(s, y), "GAME OVER") {
			found = true
		}
	}
	if !found {
		t.Error("ask screen missing GAME OVER")
	}
	found = false
	for y := 1; y < s.H-1; y++ {
		if strings.Contains(rowText(s, y), "SUBMIT TO LEADERBOARD?") {
			found = true
		}
	}
	if !found {
		t.Error("ask screen missing submit prompt")
	}

	s = Render(g, pal, &ScoreUI{Mode: UIEntry, Score: 12500, Name: "DAVE", CursorOn: true})
	found = false
	for y := 1; y < s.H-1; y++ {
		if strings.Contains(rowText(s, y), "[DAVE_") {
			found = true
		}
	}
	if !found {
		t.Errorf("entry screen missing name field: %q", rowText(s, s.H/2))
	}

	ui := &ScoreUI{Mode: UIBoard, Rows: bigRows(30)}
	s = Render(g, pal, ui)
	if txt := rowText(s, 1); !strings.Contains(txt, "LEADERBOARD") {
		t.Errorf("board header = %q", txt)
	}
	if txt := rowText(s, 3); !strings.Contains(txt, fmt.Sprintf("%2d  %-8s %6d", 1, "P1", 30000)) {
		t.Errorf("first board row = %q", txt)
	}
	// The top ten, and only ten, render as rows.
	n := 0
	for y := 1; y < s.H-1; y++ {
		if strings.Contains(rowText(s, y), "  P") {
			n++
		}
	}
	if n != 10 {
		t.Errorf("board rendered %d rows, want the top ten", n)
	}
}

func TestScoreUIAllViewportSizes(t *testing.T) {
	// No viewport size may panic; text clips/centers instead of spilling.
	g := uiGame(t)
	pal := NewPalette(true)
	for _, viewW := range []int{16, 20, 30, 40, 60} {
		g.ViewW = viewW
		for _, viewH := range []int{4, 6, 9, 15} {
			g.ViewH = viewH
			for _, mode := range []UIMode{UIAsk, UIEntry, UIBoard} {
				ui := &ScoreUI{Mode: mode, Score: 999999, Name: "8CHARSLN", Rows: bigRows(30)}
				_ = Render(g, pal, ui)
				_ = RenderPixels(g, pal)
			}
		}
	}
}

func TestBoardClampsOnShortViewports(t *testing.T) {
	// On a 4-tile viewport (8 world rows) the board must not write into
	// the HUD or status rows, and still shows its header + footer.
	g := uiGame(t)
	g.ViewW, g.ViewH = 20, 4
	s := Render(g, NewPalette(true), &ScoreUI{Mode: UIBoard, Rows: bigRows(30)})
	if txt := rowText(s, 1); !strings.Contains(txt, "LEADERBOARD") {
		t.Errorf("short board header = %q", txt)
	}
	for _, y := range []int{0, s.H - 1} {
		if strings.Contains(rowText(s, y), "P") && !strings.Contains(rowText(s, y), "SCORE") {
			t.Errorf("board bled into band row %d: %q", y, rowText(s, y))
		}
	}
}

func TestBoardStatusAndFooters(t *testing.T) {
	g := uiGame(t)
	pal := NewPalette(true)
	s := Render(g, pal, &ScoreUI{Mode: UIBoard, Rows: bigRows(3), Status: "OFFLINE"})
	if !strings.Contains(rowText(s, s.H-2), "OFFLINE") {
		t.Errorf("status footer = %q", rowText(s, s.H-2))
	}
	s = Render(g, pal, &ScoreUI{Mode: UIBoard, Title: true})
	if !strings.Contains(rowText(s, s.H-2), "L CLOSE") {
		t.Errorf("title footer = %q", rowText(s, s.H-2))
	}
	s = Render(g, pal, &ScoreUI{Mode: UIBoard})
	if !strings.Contains(rowText(s, 3), "NO SCORES YET") {
		t.Errorf("empty board = %q", rowText(s, 3))
	}
}

func TestNameFieldConstantWidth(t *testing.T) {
	widths := map[string]int{}
	for _, name := range []string{"", "D", "DAVE", "8CHARSLN"} {
		w := len(nameField(name, true))
		widths[name] = w
	}
	if widths[""] != widths["8CHARSLN"] {
		t.Errorf("name field width varies: %v", widths)
	}
}

func TestScoreUIJSONModeNames(t *testing.T) {
	for _, m := range []struct {
		mode UIMode
		want string
	}{{UIOff, "off"}, {UIAsk, "ask"}, {UIEntry, "entry"}, {UIBoard, "board"}} {
		b, err := m.mode.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != `"`+m.want+`"` {
			t.Errorf("mode %d marshals as %s, want %q", m.mode, b, m.want)
		}
	}
}
