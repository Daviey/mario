package render

// In-game leaderboard UI as real text. The screens replace the world with
// terminal-font text cells (native build — same layer as the HUD row) or a
// DOM panel (browser build — the page renders the JSON snapshot). Pure
// function of the ScoreUI snapshot and the game tick (blink).

import (
	"fmt"
	"strings"
)

// UIMode is which leaderboard screen (if any) is showing.
type UIMode int

const (
	// UIOff draws no leaderboard UI.
	UIOff UIMode = iota
	// UIAsk is the "submit score?" y/n prompt.
	UIAsk
	// UIEntry is name entry.
	UIEntry
	// UIBoard is the leaderboard table.
	UIBoard
)

// String names the mode for logs and the JSON bridge.
func (m UIMode) String() string {
	switch m {
	case UIAsk:
		return "ask"
	case UIEntry:
		return "entry"
	case UIBoard:
		return "board"
	}
	return "off"
}

// MarshalJSON encodes the mode as its name so the page can switch on it.
func (m UIMode) MarshalJSON() ([]byte, error) {
	return []byte(`"` + m.String() + `"`), nil
}

// ScoreRow is one leaderboard entry.
type ScoreRow struct {
	Rank  int    `json:"rank"`
	Name  string `json:"name"`
	Score int    `json:"score"`
	Mine  bool   `json:"mine"`
}

// ScoreUI is an immutable snapshot of the leaderboard UI state.
type ScoreUI struct {
	Mode     UIMode     `json:"mode"`
	Score    int        `json:"score"` // score being submitted (ask/entry)
	Name     string     `json:"name"`
	CursorOn bool       `json:"cursorOn"` // blinking cursor (entry)
	Rows     []ScoreRow `json:"rows"`
	Loading  bool       `json:"loading"`
	Status   string     `json:"status"` // e.g. "SUBMITTED!" or an error line
	Title    bool       `json:"title"`  // opened from the title screen
}

// firstUI returns the first optional UI snapshot or nil.
func firstUI(ui []*ScoreUI) *ScoreUI {
	if len(ui) > 0 {
		return ui[0]
	}
	return nil
}

const (
	nameFieldW   = 8  // max name chars, mirrors lbui maxNameLen
	boardMaxRows = 10 // the board shows the top ten
)

// blinkVisible reports whether blinking lines draw this tick (same cadence
// as the pixel UI: ~0.7s on, ~0.3s off at 60 Hz).
func blinkVisible(tick int) bool { return tick%40 < 28 }

// drawScoreUIText paints the active leaderboard screen as real text cells
// over a dark background, replacing the world rows of the screen (rows
// 1..H-2; row 0 is the HUD, the last row the status line).
func drawScoreUIText(s *Screen, ui *ScoreUI, p *Palette, tick int) {
	for y := 1; y < s.H-1; y++ {
		for x := 0; x < s.W; x++ {
			s.SetStyled(x, y, ' ', p.White, p.Dark, false)
		}
	}
	switch ui.Mode {
	case UIAsk:
		drawCenteredBlock(s, p.Dark, tick, []uiLine{
			{"GAME OVER", p.White, true, false},
			{fmt.Sprintf("SCORE %06d", ui.Score), p.White, false, false},
			{"", p.White, false, false},
			{"SUBMIT TO LEADERBOARD?", p.GoldLight, false, true},
			{"Y YES   N NO", p.White, false, false},
		})
	case UIEntry:
		drawCenteredBlock(s, p.Dark, tick, []uiLine{
			{"GAME OVER", p.White, true, false},
			{fmt.Sprintf("SCORE %06d", ui.Score), p.White, false, false},
			{"", p.White, false, false},
			{"ENTER NAME", p.GoldLight, false, false},
			{nameField(ui.Name, ui.CursorOn), p.White, false, false},
			{"ENTER OK   BS DEL   ESC BACK", p.White, false, false},
		})
	case UIBoard:
		drawBoardText(s, ui, p, tick)
	}
}

// uiLine is one line of a centered text block.
type uiLine struct {
	text  string
	fg    Color
	bold  bool
	blink bool
}

// drawCenteredBlock vertically centers a block of lines on the world rows.
func drawCenteredBlock(s *Screen, bg Color, tick int, lines []uiLine) {
	top := 1 + max(0, (s.H-2-len(lines))/2)
	for i, ln := range lines {
		if ln.blink && !blinkVisible(tick) {
			continue
		}
		s.Center(top+i, ln.text, ln.fg, bg, ln.bold)
	}
}

// nameField renders "[ DAVE_    ]" — a constant-width field so the layout
// never jitters while typing; the cursor is a blinking underscore.
func nameField(name string, cursor bool) string {
	inner := name
	if cursor {
		inner += "_"
	}
	return "[" + inner + strings.Repeat(" ", max(0, nameFieldW+1-len(inner))) + "]"
}

// drawBoardText paints the leaderboard table: header, up to ten rows,
// status/footer. Rows clamp on very short viewports instead of spilling
// into the HUD or status lines.
func drawBoardText(s *Screen, ui *ScoreUI, p *Palette, tick int) {
	bg := p.Dark
	s.Center(1, "LEADERBOARD", p.GoldLight, bg, true)
	footY := s.H - 2
	bodyY := 3
	switch {
	case ui.Loading:
		if blinkVisible(tick) {
			s.Center(bodyY, "LOADING...", p.White, bg, false)
		}
	case len(ui.Rows) == 0:
		s.Center(bodyY, "NO SCORES YET", p.White, bg, false)
	default:
		lastRow := footY - 2 // keep a blank row above the footer
		n := min(len(ui.Rows), boardMaxRows, max(0, lastRow-bodyY+1))
		for i := range n {
			r := ui.Rows[i]
			s.Center(bodyY+i, fmt.Sprintf("%2d  %-8s %6d", r.Rank, r.Name, r.Score),
				rowColor(r.Mine, p), bg, r.Mine)
		}
	}
	switch {
	case ui.Status != "":
		s.Center(footY, ui.Status, p.GoldLight, bg, false)
	case ui.Title:
		if blinkVisible(tick) {
			s.Center(footY, "L CLOSE", p.White, bg, false)
		}
	default:
		if blinkVisible(tick) {
			s.Center(footY, "R RESTART   Q QUIT", p.White, bg, false)
		}
	}
}

func rowColor(mine bool, p *Palette) Color {
	if mine {
		return p.GoldLight
	}
	return p.White
}
