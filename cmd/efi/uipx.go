//go:build linux

package main

// Leaderboard screens on the framebuffer: the terminal build draws these
// as real text cells and the browser as DOM text, but the EFI boot has
// neither — so they are re-composed with the 3×5 pixel font over the dark
// palette, mirroring render's drawScoreUIText screens line for line.
// Line pitch is 7px (5px glyphs + 2px gap); rows clamp on short viewports
// instead of spilling into the HUD or status bands, like every other
// board surface.

import (
	"fmt"

	"github.com/Daviey/mario/render"
)

// uiLine is one centered line of a leaderboard screen.
type uiLine struct {
	text  string
	color render.Color
	blink bool
}

// uiFrame paints the active leaderboard screen over the world rows of the
// freshly rendered game frame (HUD and status bands are kept). The frame
// is modified in place — RenderPixels returns a new frame every tick.
func uiFrame(base *render.Frame, p *render.Palette, u *render.ScoreUI, tick int) *render.Frame {
	worldY := render.HudBandPx
	worldH := base.H - render.HudBandPx - render.StatusBandPx
	base.Fill(0, worldY, base.W, worldH, p.Dark)
	switch u.Mode {
	case render.UIAsk:
		askLines(base, p, u, worldY, worldH, tick)
	case render.UIEntry:
		entryScreen(base, p, u, worldY, worldH)
	case render.UIBoard:
		boardScreen(base, p, u, worldY, worldH, tick)
	case render.UIAbout:
		aboutScreen(base, p, worldY, worldH, tick)
	}
	return base
}

// centerBlock vertically centers a block of lines and draws them.
func centerBlock(f *render.Frame, worldY, worldH int, lines []uiLine, tick int) {
	y := worldY + max(0, (worldH-len(lines)*7)/2)
	drawLines(f, lines, y, tick)
}

func drawLines(f *render.Frame, lines []uiLine, y int, tick int) {
	for _, ln := range lines {
		if ln.blink && !render.BlinkVisible(tick) {
			y += 7
			continue
		}
		if ln.text != "" {
			f.DrawTextPx(ln.text, (f.W-render.TextWidthPx(ln.text))/2, y, ln.color)
		}
		y += 7
	}
}

func askLines(f *render.Frame, p *render.Palette, u *render.ScoreUI, worldY, worldH int, tick int) {
	best := ""
	if u.Best > 0 {
		best = fmt.Sprintf("BEST %06d", u.Best)
	}
	centerBlock(f, worldY, worldH, []uiLine{
		{"GAME OVER", p.White, false},
		{fmt.Sprintf("SCORE %06d", u.Score), p.White, false},
		{best, p.TextDim, false},
		{"SUBMIT TO LEADERBOARD?", p.GoldLight, true},
		{"Y YES   N NO", p.White, false},
	}, tick)
}

// entryScreen is the name-entry screen. The pixel font has no brackets
// or underscore glyphs (a product constraint), so the constant-width name
// field gets drawn rails: two 1px bars with the 8-char slot between, and
// the cursor as a blinking filled block after the typed name.
func entryScreen(f *render.Frame, p *render.Palette, u *render.ScoreUI, worldY, worldH int) {
	lines := []uiLine{
		{"GAME OVER", p.White, false},
		{fmt.Sprintf("SCORE %06d", u.Score), p.White, false},
		{"", p.White, false},
		{"ENTER NAME", p.GoldLight, false},
	}
	y := worldY + max(0, (worldH-(len(lines)+2)*7)/2)
	drawLines(f, lines, y, 0)
	y += len(lines) * 7

	// Name field: 8 slots of 4px + 1px gap between rails.
	slot := render.TextWidthPx("WWWWWWWW")
	fx := (f.W - slot) / 2
	f.Fill(fx-3, y-1, 1, 7, p.GoldLight)
	f.Fill(fx+slot+2, y-1, 1, 7, p.GoldLight)
	if u.Name != "" {
		f.DrawTextPx(u.Name, fx, y, p.White)
	}
	if u.CursorOn {
		f.Fill(fx+4*len([]rune(u.Name)), y, 4, 5, p.White)
	}
	y += 7 * 2
	f.DrawTextPx("ENTER OK   BS DEL   ESC BACK", (f.W-render.TextWidthPx("ENTER OK   BS DEL   ESC BACK"))/2, y, p.TextDim)
}

func boardScreen(f *render.Frame, p *render.Palette, u *render.ScoreUI, worldY, worldH int, tick int) {
	head := "LEADERBOARD"
	if u.Daily {
		head = "DAILY LEADERBOARD"
	}
	f.DrawTextPx(head, (f.W-render.TextWidthPx(head))/2, worldY, p.GoldLight)

	foot := "L CLOSE   R RESTART"
	if u.Title {
		foot = "L CLOSE"
	}
	footY := worldY + worldH - 7
	f.DrawTextPx(foot, (f.W-render.TextWidthPx(foot))/2, footY, p.TextDim)

	if u.Status != "" && footY-7 >= worldY+14 {
		f.DrawTextPx(u.Status, (f.W-render.TextWidthPx(u.Status))/2, footY-7, p.White)
	}

	if u.Loading {
		if render.BlinkVisible(tick) {
			msg := "LOADING"
			f.DrawTextPx(msg, (f.W-render.TextWidthPx(msg))/2, worldY+14, p.TextDim)
		}
		return
	}
	if len(u.Rows) == 0 {
		msg := "NO SCORES YET"
		f.DrawTextPx(msg, (f.W-render.TextWidthPx(msg))/2, worldY+14, p.TextDim)
		return
	}

	// Rows between the header and the status/footer lines.
	maxRows := (footY - 7 - (worldY + 14)) / 7
	for i, row := range u.Rows {
		if i >= maxRows {
			break
		}
		color := p.White
		if row.Mine {
			color = p.GoldLight
		}
		mark := " "
		if row.Verified {
			mark = "+"
		}

		txt := fmt.Sprintf("%2d %-8s %06d L%-2d %s", i+1, row.Name, row.Score, row.Level, mark)
		f.DrawTextPx(txt, (f.W-render.TextWidthPx(txt))/2, worldY+14+i*7, color)
	}
}

// aboutScreen is the about page in the arcade glyph set (no lowercase,
// no '·' — charset A-Z 0-9 space . - + / : ! ?).
func aboutScreen(f *render.Frame, p *render.Palette, worldY, worldH int, tick int) {
	centerBlock(f, worldY, worldH, []uiLine{
		{"SUPER CLI MARIO", p.White, false},
		{"", p.White, false},
		{"A FAN-MADE TERMINAL PLATFORMER", p.GoldLight, false},
		{"UNOFFICIAL FAN ART", p.White, false},
		{"NOT AFFILIATED WITH NINTENDO", p.White, false},
		{"MARIO IS A TRADEMARK OF NINTENDO", p.TextDim, false},
		{"", p.White, false},
		{"PLAYS IN YOUR TERMINAL", p.White, false},
		{"OVER SSH AND IN THE BROWSER", p.White, false},
		{"", p.White, false},
		{"I CLOSE", p.TextDim, true},
	}, tick)
}
