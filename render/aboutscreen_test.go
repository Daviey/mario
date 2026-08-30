package render

import (
	"strings"
	"testing"

	"github.com/Daviey/mario/engine"
)

// TestAboutScreenText: the about screen paints the disclaimer lines as
// real text over the world rows.
func TestAboutScreenText(t *testing.T) {
	g := engine.NewGame(engine.DefaultLevels(), 30, engine.LevelHeight) // title
	ui := &ScoreUI{Mode: UIAbout, Title: true}
	s := Render(g, testPal, ui)
	var rows []string
	for y := 1; y < s.H-1; y++ {
		rows = append(rows, s.RowString(y))
	}
	all := strings.Join(rows, "\n")
	for _, want := range []string{
		"SUPER CLI MARIO",
		"fan-made terminal platformer",
		"unofficial fan art · not affiliated with nintendo",
		"mario is a trademark of nintendo",
		"I CLOSE",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("about screen missing %q", want)
		}
	}
}

// TestUIModeStrings: every mode needs a JSON name — the web build
// switches on it (a missing name reads as "off" and kills the screen).
func TestUIModeStrings(t *testing.T) {
	for _, tc := range []struct {
		m    UIMode
		want string
	}{
		{UIOff, "off"}, {UIAsk, "ask"}, {UIEntry, "entry"},
		{UIBoard, "board"}, {UIAbout, "about"},
	} {
		if got := tc.m.String(); got != tc.want {
			t.Errorf("UIMode(%d).String() = %q; want %q", tc.m, got, tc.want)
		}
	}
}
