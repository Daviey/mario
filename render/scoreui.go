package render

// In-game leaderboard UI: submit prompt, name entry and the score board,
// drawn with the 3x5 pixel font over the world frame (title backdrop or
// game-over backdrop). Pure function of the ScoreUI snapshot.

import (
	"fmt"
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

// ScoreRow is one leaderboard entry.
type ScoreRow struct {
	Rank  int
	Name  string
	Score int
	Mine  bool
}

// ScoreUI is an immutable snapshot of the leaderboard UI state.
type ScoreUI struct {
	Mode     UIMode
	Score    int // score being submitted (ask/entry)
	Name     string
	CursorOn bool // blinking cursor block (entry)
	Rows     []ScoreRow
	Loading  bool
	Status   string // e.g. "SUBMITTED!" or an error line (board)
}

// firstUI returns the first optional UI snapshot or nil.
func firstUI(ui []*ScoreUI) *ScoreUI {
	if len(ui) > 0 {
		return ui[0]
	}
	return nil
}

// drawScoreUIPx draws the active leaderboard UI. onTitle selects the title
// layout (board floats between logo and cast); otherwise the game-over /
// win layout (centered block).
func drawScoreUIPx(f *Frame, ui *ScoreUI, p *Palette, onTitle bool, tick int) {
	switch ui.Mode {
	case UIAsk:
		// Bottom-anchored block: banner, then a dark panel with the lines
		// the caller draws at fixed offsets (panel is backing only, so
		// every screen size lays out identically).
		y0 := f.H - 32
		drawBannerPx(f, y0, "GAME OVER", p.OverlayFG, p.OverlayBG, p)
		drawPanelPx(f, p, y0+9, 23)
		drawCenterPx(f, y0+13, fmt.Sprintf("SCORE %d", ui.Score), p.White, 1)
		if tick%40 < 28 {
			drawCenterPx(f, y0+20, "SUBMIT SCORE?", p.GoldLight, 1)
		}
		drawCenterPx(f, y0+27, "Y YES   N NO", p.White, 1)
	case UIEntry:
		y0 := f.H - 40
		drawBannerPx(f, y0, "GAME OVER", p.OverlayFG, p.OverlayBG, p)
		drawPanelPx(f, p, y0+9, 31)
		drawCenterPx(f, y0+13, fmt.Sprintf("SCORE %d", ui.Score), p.White, 1)
		drawCenterPx(f, y0+20, "ENTER NAME", p.GoldLight, 1)
		// The font has no bracket glyphs, so the name field is drawn as
		// gold rails flanking the text, with the cursor just after it.
		nameY := y0 + 27
		nw := textWidthPx(ui.Name, 1)
		x := (f.W - nw) / 2
		drawTextPx(f, x, nameY, ui.Name, p.White, 1)
		f.Fill(x-4, nameY, 1, 5, p.GoldLight)
		f.Fill(x+nw+6, nameY, 1, 5, p.GoldLight)
		if ui.CursorOn {
			f.Fill(x+nw+1, nameY, 2, 5, p.GoldLight)
		}
		drawCenterPx(f, y0+34, "ENTER OK  BS DEL", p.White, 1)
	case UIBoard:
		drawBoardPx(f, ui, p, onTitle, tick)
	}
}

// drawPanelPx paints a dark backing band; text lines are drawn by the
// caller at 7px pitch so special rows (name field, blink) share one
// coordinate source.
func drawPanelPx(f *Frame, p *Palette, y, h int) {
	pw := max(textWidthPx("SUBMIT SCORE?", 1), textWidthPx("ENTER OK  BS DEL", 1))
	if pw > f.W-4 {
		pw = f.W - 4
	}
	x := (f.W - pw) / 2
	f.Fill(x-2, y, pw+4, h, p.Dark)
}

func drawBoardPx(f *Frame, ui *ScoreUI, p *Palette, onTitle bool, tick int) {
	top := 4
	if onTitle {
		// Below the header, above the cast sprites (castY = f.H-18).
		top = 6
	}
	if !ui.Loading && len(ui.Rows) > 0 {
		// Solid band from just above the header through the last row:
		// sprites and clouds must never bleed through board text.
		fit := max(0, (f.H-20-(top+8))/7)
		f.Fill(0, top-1, f.W, fit*7+10, p.Dark)
		drawCenterPx(f, top, "LEADERBOARD", p.GoldLight, 1)
	} else {
		drawCenterShadowPx(f, top, "LEADERBOARD", p.GoldLight, 1, p.Dark)
	}
	if ui.Loading {
		if tick%40 < 28 {
			drawCenterShadowPx(f, top+8, "LOADING", p.White, 1, p.Dark)
		}
		return
	}
	if len(ui.Rows) == 0 {
		drawCenterShadowPx(f, top+8, "NO SCORES YET", p.White, 1, p.Dark)
	} else {
		// Rows run from top+8 down to the footer at f.H-6.
		fit := max(0, (f.H-20-(top+8))/7)
		for i, r := range ui.Rows {
			if i >= fit {
				break
			}
			c := p.White
			if r.Mine {
				c = p.GoldLight
			}
			drawCenterPx(f, top+8+i*7, fmt.Sprintf("%d %s %d", r.Rank, r.Name, r.Score), c, 1)
		}
	}
	if ui.Status != "" {
		drawCenterShadowPx(f, f.H-6, ui.Status, p.GoldLight, 1, p.Dark)
	} else if onTitle {
		if tick%40 < 28 {
			drawCenterShadowPx(f, f.H-6, "L CLOSE", p.White, 1, p.Dark)
		}
	} else if tick%40 < 28 {
		drawCenterShadowPx(f, f.H-6, "R RESTART   Q QUIT", p.White, 1, p.Dark)
	}
}
