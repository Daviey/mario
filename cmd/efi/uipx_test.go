//go:build linux

package main

import (
	"testing"

	"github.com/Daviey/mario/render"
)

func uiGame(t *testing.T) *render.Frame {
	// A mock world frame to paint UI over.
	pal := render.NewPalette(render.Colors24)
	base := render.NewFrame(240, 105, pal.Sky)
	base.Fill(0, 0, 240, render.HudBandPx, pal.HUDBG)
	base.Fill(0, base.H-render.StatusBandPx, 240, render.StatusBandPx, pal.StatusBG)
	return base
}

func countColors(f *render.Frame, ignore ...render.Color) int {
	skip := make(map[render.Color]bool)
	for _, c := range ignore {
		skip[c] = true
	}
	n := 0
	for y := render.HudBandPx; y < f.H-render.StatusBandPx; y++ {
		for x := 0; x < f.W; x++ {
			if !skip[f.At(x, y)] {
				n++
			}
		}
	}
	return n
}

func TestAskScreenPaintsWorldRows(t *testing.T) {
	pal := render.NewPalette(render.Colors24)
	f := uiFrame(uiGame(t), pal, &render.ScoreUI{Mode: render.UIAsk, Score: 12500}, 0)

	// The HUD band must stay.
	if f.At(100, 2) != pal.HUDBG {
		t.Errorf("HUD band corrupted")
	}

	// The world rows are mostly dark (filled dark), plus some text.
	mid := render.HudBandPx + 10
	if f.At(10, mid) != pal.Dark {
		t.Errorf("world rows not darkened")
	}

	// Text drawn (not purely dark).
	drawn := countColors(f, pal.Dark)
	if drawn < 10 {
		t.Errorf("UI screen empty? non-dark pixels = %d", drawn)
	}
}

func TestBoardRendersRowsAndClamps(t *testing.T) {
	pal := render.NewPalette(render.Colors24)
	var rows []render.ScoreRow
	for i := range 30 {
		rows = append(rows, render.ScoreRow{Rank: i + 1, Name: "DAVE", Score: i})
	}
	ui := &render.ScoreUI{Mode: render.UIBoard, Status: "OFFLINE", Title: true, Rows: rows}

	// Small frame: simulate 240 x 39 viewport (tiny H) — clamp must not panic.
	base := render.NewFrame(240, 39, pal.Sky)
	f := uiFrame(base, pal, ui, 0)

	drawn := countColors(f, pal.Dark)
	if drawn < 10 {
		t.Errorf("Board empty? non-dark pixels = %d", drawn)
	}
}

func TestEntryFieldRailsAndCursor(t *testing.T) {
	pal := render.NewPalette(render.Colors24)
	ui := &render.ScoreUI{Mode: render.UIEntry, Name: "DAVE", CursorOn: true}
	f := uiFrame(uiGame(t), pal, ui, 0)

	// The rails are drawn in GoldLight.
	gold := 0
	for y := render.HudBandPx; y < f.H-render.StatusBandPx; y++ {
		for x := 0; x < f.W; x++ {
			if f.At(x, y) == pal.GoldLight {
				gold++
			}
		}
	}
	if gold < 10 {
		t.Errorf("Missing entry field rails? gold pixels = %d", gold)
	}
}
