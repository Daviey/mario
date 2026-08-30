package render

import (
	"strings"
	"testing"

	"github.com/Daviey/mario/engine"
)

// titleEl finds the first title element whose text is s.
func titleEl(f *Frame, g *engine.Game, s string) *titleText {
	for i, e := range titleTextEls(f, g) {
		if e.s == s {
			return &titleTextEls(f, g)[i]
		}
	}
	return nil
}

// TestTitleAboutBannerPresent: on a tall viewport the fan-game disclaimer
// appears under the version tag, above the cast, in the arcade charset.
func TestTitleAboutBannerPresent(t *testing.T) {
	g := engine.NewGame(engine.DefaultLevels(), 30, 12)
	f := worldFrame(nil, g, testPal)
	castY := titleCastY(f)

	fan := titleEl(f, g, "UNOFFICIAL FAN GAME")
	if fan == nil {
		t.Fatal("about banner line 1 missing on a 12-tile viewport")
	}
	if fan.y+5 > castY {
		t.Errorf("banner line 1 at y=%d overlaps the cast (castY=%d)", fan.y, castY)
	}
	nin := titleEl(f, g, "NOT AFFILIATED WITH NINTENDO")
	if nin == nil {
		t.Fatal("about banner line 2 missing on a 12-tile viewport")
	}
	if nin.y != fan.y+6 {
		t.Errorf("banner line 2 at y=%d, want %d (6px below line 1)", nin.y, fan.y+6)
	}
	if nin.y+5 > castY {
		t.Errorf("banner line 2 at y=%d overlaps the cast (castY=%d)", nin.y, castY)
	}
}

// TestTitleAboutBannerLadderWidth: narrow viewports shorten the disclaimer
// instead of overflowing.
func TestTitleAboutBannerLadderWidth(t *testing.T) {
	for _, tc := range []struct {
		vw          int
		want1, want string
	}{
		{30, "UNOFFICIAL FAN GAME", "NOT AFFILIATED WITH NINTENDO"}, // 180px
		{16, "UNOFFICIAL FAN GAME", "NO NINTENDO AFFILIATION"},      // 96px
		{10, "FAN GAME", "NOT NINTENDO"},                            // 60px
	} {
		g := engine.NewGame(engine.DefaultLevels(), tc.vw, 12)
		f := worldFrame(nil, g, testPal)
		if e := titleEl(f, g, tc.want1); e == nil {
			t.Errorf("vw=%d: banner line 1 %q missing", tc.vw, tc.want1)
		}
		if e := titleEl(f, g, tc.want); e == nil {
			t.Errorf("vw=%d: banner line 2 %q missing", tc.vw, tc.want)
		}
	}
}

// TestTitleAboutBannerCharset: the banner is drawn through the 3×5 arcade
// font, so it must stick to its glyph set (regression guard for edits).
func TestTitleAboutBannerCharset(t *testing.T) {
	const ok = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 .-+:!/?"
	for _, s := range []string{"UNOFFICIAL FAN GAME", "FAN GAME",
		"NOT AFFILIATED WITH NINTENDO", "NO NINTENDO AFFILIATION", "NOT NINTENDO"} {
		for _, r := range s {
			if !strings.ContainsRune(ok, r) {
				t.Errorf("banner text %q uses non-arcade rune %q", s, r)
			}
		}
	}
}

// TestTitleAboutBannerAbsentOnShortViewports: below ~11 tiles the sky has
// no room; the banner must vanish (the status line carries it instead).
func TestTitleAboutBannerAbsentOnShortViewports(t *testing.T) {
	for _, vh := range []int{7, 8, 9, 10} {
		g := engine.NewGame(engine.DefaultLevels(), 30, vh)
		f := worldFrame(nil, g, testPal)
		for _, e := range titleTextEls(f, g) {
			if strings.Contains(e.s, "FAN") || strings.Contains(e.s, "NINTENDO") {
				t.Errorf("vh=%d: about banner %q drawn without room", vh, e.s)
			}
		}
	}
}

// TestStatusAboutBannerOnTitle: the bottom real-text line swaps from the
// controls to the fan-game disclaimer at the title, on every width (the
// ladder only shortens, never drops it), and back once play starts.
func TestStatusAboutBannerOnTitle(t *testing.T) {
	g := engine.NewGame(engine.DefaultLevels(), 20, 9) // title state
	if s := statusText(999, g); !strings.Contains(s, "unofficial fan game") ||
		!strings.Contains(s, "nintendo") {
		t.Errorf("title status = %q; want the fan-game disclaimer", s)
	}
	// Ladder: every width keeps a disclaimer, shrinking to "fan game".
	for _, tc := range []struct {
		w    int
		want string
	}{
		{999, "unofficial fan game - not affiliated with nintendo - runs in your terminal"},
		{50, "unofficial fan game - not affiliated with nintendo"},
		{34, "unofficial fan game - not nintendo"},
		{8, "fan game"},
		{1, "fan game"},
	} {
		if s := statusText(tc.w, g); s != tc.want {
			t.Errorf("statusText(%d) = %q; want %q", tc.w, s, tc.want)
		}
	}

	// Full render: the bottom screen row carries the disclaimer at the
	// title, and the controls once the game is running.
	scr := Render(g, testPal)
	row := strings.TrimSpace(scr.rowString(scr.H - 1))
	if !strings.Contains(row, "unofficial fan game") {
		t.Errorf("title bottom row = %q; want about banner", row)
	}
	g.Update(engine.Input{AnyKey: true}) // start a run
	g.Update(engine.Input{})
	if s := statusText(999, g); s != "a/d move - w/space jump - s duck - x run - p pause - k die - r restart - q quit" {
		t.Errorf("in-play status = %q; want controls", s)
	}
}
