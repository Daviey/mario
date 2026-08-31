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
	// UIAbout is the fan-game about screen (title 'i').
	UIAbout
)

// BoardRowFormat is the leaderboard row layout shared by every board
// surface (terminal cells here, the EFI pixel-font board in cmd/efi/uipx.go
// — which substitutes '+' for the '✓' mark because the 3×5 font has no
// checkmark glyph). Single source: column alignment must not drift between
// surfaces.
const BoardRowFormat = "%2d %s %-8s %6d  L%d"

// String names the mode for logs and the JSON bridge.
func (m UIMode) String() string {
	switch m {
	case UIAsk:
		return "ask"
	case UIEntry:
		return "entry"
	case UIBoard:
		return "board"
	case UIAbout:
		return "about"
	}
	return "off"
}

// MarshalJSON encodes the mode as its name so the page can switch on it.
func (m UIMode) MarshalJSON() ([]byte, error) {
	return []byte(`"` + m.String() + `"`), nil
}

// ScoreRow is one leaderboard entry.
type ScoreRow struct {
	Rank     int    `json:"rank"`
	Name     string `json:"name"`
	Score    int    `json:"score"`
	Level    int    `json:"level"` // 1-based level reached
	Mine     bool   `json:"mine"`
	Verified bool   `json:"verified"` // replay-confirmed by the verifier
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
	Best     int        `json:"best"`   // local best score (0 = none yet)
	Rank     int        `json:"rank"`   // post-submit rank, 0 = not in top n
	Daily    bool       `json:"daily"`  // daily-challenge leaderboard
}

// firstUI returns the first optional UI snapshot or nil.
func firstUI(ui []*ScoreUI) *ScoreUI {
	if len(ui) > 0 {
		return ui[0]
	}
	return nil
}

const (
	nameFieldW   = 8  // max name chars, mirrors the UI maxNameLen
	boardMaxRows = 10 // the board shows the top ten
)

// BlinkVisible reports whether blinking lines draw this tick (the one
// blink cadence shared by every surface — terminal text, the WASM canvas
// and the EFI framebuffer UI); on ~28 of every 40 ticks (0.47s on, 0.2s
// off at 60 Hz).
func BlinkVisible(tick int) bool { return tick%40 < 28 }

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
		best := ""
		if ui.Best > 0 {
			best = fmt.Sprintf("BEST %06d", ui.Best)
		}
		drawCenteredBlock(s, p.Dark, tick, []uiLine{
			{"GAME OVER", p.White, true, false},
			{fmt.Sprintf("SCORE %06d", ui.Score), p.White, false, false},
			{best, p.TextDim, false, false},
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
	case UIAbout:
		drawAboutText(s, p, tick)
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
		if ln.blink && !BlinkVisible(tick) {
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
	head := "LEADERBOARD"
	if ui.Daily {
		head = "DAILY LEADERBOARD"
	}
	s.Center(1, head, p.GoldLight, bg, true)
	footY := s.H - 2
	bodyY := 3
	switch {
	case ui.Loading:
		if BlinkVisible(tick) {
			s.Center(bodyY, "LOADING...", p.White, bg, false)
		}
	case len(ui.Rows) == 0:
		s.Center(bodyY, "NO SCORES YET", p.White, bg, false)
	default:
		lastRow := footY - 2 // keep a blank row above the footer
		n := min(len(ui.Rows), boardMaxRows, max(0, lastRow-bodyY+1))
		for i := range n {
			r := ui.Rows[i]
			mark := " "
			if r.Verified {
				mark = "✓"
			}
			s.Center(bodyY+i, fmt.Sprintf(BoardRowFormat, r.Rank, mark, r.Name, r.Score, r.Level),
				rowColor(r.Mine, p), bg, r.Mine)
		}
		if ui.Rank > 0 {
			s.Center(bodyY+n, fmt.Sprintf("YOU ARE #%d", ui.Rank), p.GoldLight, bg, false)
		}
	}
	// Status (and the post-submit rank line) sits just above the footer so
	// the footer always carries the close/restart invitation.
	if ui.Status != "" && footY-1 >= 2 {
		s.Center(footY-1, ui.Status, p.GoldLight, bg, false)
	}
	switch {
	case ui.Title:
		if BlinkVisible(tick) {
			s.Center(footY, "L CLOSE", p.White, bg, false)
		}
	default:
		if BlinkVisible(tick) {
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

// drawAboutText paints the about screen: what the game is, what it isn't,
// and where it runs. Real text (same layer as the other UI screens).
func drawAboutText(s *Screen, p *Palette, tick int) {
	drawCenteredBlock(s, p.Dark, tick, []uiLine{
		{"SUPER CLI MARIO", p.White, true, false},
		{"", p.White, false, false},
		{"a fan-made terminal platformer", p.GoldLight, false, false},
		{"unofficial fan art · not affiliated with nintendo", p.White, false, false},
		{"mario is a trademark of nintendo", p.TextDim, false, false},
		{"", p.White, false, false},
		{"plays in your terminal · over ssh · in the browser", p.White, false, false},
		{"arrows/wasd move · space jumps · hold x to run", p.GoldLight, false, false},
		{"", p.White, false, false},
		{"I CLOSE", p.TextDim, false, true},
	})
}
