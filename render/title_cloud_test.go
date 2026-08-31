package render

import (
	"strings"
	"testing"

	"github.com/Daviey/mario/engine"
)

// sentinelCloudPal clones the test palette with clouds recolored magenta so
// cloud pixels stay identifiable even under the white SUPER CLI text.
func sentinelCloudPal() *Palette {
	pal := *testPal
	pal.Cloud = color(0xFF00FF, 5)
	return &pal
}

// Title bands for these sweeps come from the production single source —
// titleTextEls via titleTextBands, the same rects the cloud filter in
// drawDecorations keeps clear — so the test can never drift from the
// layout actually painted. The layout pins below (red logo pixels in
// band[0], a white subtitle under it) independently verify the bands
// match what the title painter stamps.

// TestTitleCloudsNeverOverlapTitleText sweeps the title screen of every
// built-in level at several viewport sizes: no cloud may paint a pixel
// inside any title text band. Clouds are sentinel-recolored first, so the
// check works even where clouds and text are both white.
func TestTitleCloudsNeverOverlapTitleText(t *testing.T) {
	pal := sentinelCloudPal()
	suppressed, drawn := 0, 0
	for _, viewW := range []int{20, 30, 40} {
		for _, viewH := range []int{7, 9, 12, 15} {
			for _, lv := range engine.DefaultLevels() {
				g := engine.NewGame([]*engine.Level{lv}, viewW, viewH) // starts on title
				f := worldFrame(nil, g, pal)
				bands := titleTextBands(f, g)
				if len(bands) == 0 {
					t.Fatalf("%s %dx%d: no title text bands", lv.Name, viewW, viewH)
				}

				// No cloud pixel inside any text band.
				for _, b := range bands {
					for y := b[1]; y < b[3]; y++ {
						for x := b[0]; x < b[2]; x++ {
							if f.At(x, y) == pal.Cloud {
								t.Fatalf("%s %dx%d: cloud pixel at (%d,%d) inside title text band %v",
									lv.Name, viewW, viewH, x, y, b)
							}
						}
					}
				}

				// Layout pin: the logo band holds red MARIO pixels, and at
				// full height a white subtitle sits under it.
				castY := f.H - 2*Pix - 10
				hasColor := func(b [4]int, c Color) bool {
					for y := b[1]; y < b[3]; y++ {
						for x := b[0]; x < b[2]; x++ {
							if f.At(x, y) == c {
								return true
							}
						}
					}
					return false
				}
				if castY >= 13 && !hasColor(bands[0], testPal.FlagRed) {
					t.Fatalf("%s %dx%d: logo band %v has no MARIO pixels", lv.Name, viewW, viewH, bands[0])
				}
				if viewH >= 12 {
					subOK := false
					for _, b := range bands[1:] {
						if b[1] > bands[0][3] && b[1] < bands[0][3]+12 && hasColor(b, testPal.White) {
							subOK = true
						}
					}
					if !subOK {
						t.Fatalf("%s %dx%d: no white subtitle pixels under the logo", lv.Name, viewW, viewH)
					}
				}

				// Sweep bookkeeping: prove the filter is live (candidates
				// exist that must be suppressed) and that clouds still
				// render on the title sky somewhere.
				ox := int(g.CameraX * Pix)
				oy := int(CameraY(g) * Pix)
				for tx := range lv.Width {
					row, ok := CloudAt(tx)
					if !ok || cloudBlocked(g, tx, row) {
						continue
					}
					x0, y0 := tx*Pix-ox, row*Pix-oy
					for _, b := range bands {
						if x0 < b[2] && x0+sprW(sprCloud) > b[0] && y0 < b[3] && y0+sprH(sprCloud) > b[1] {
							suppressed++
							break
						}
					}
				}
				for y := range f.H {
					for x := range f.W {
						if f.At(x, y) == pal.Cloud {
							drawn++
						}
					}
				}
			}
		}
	}
	if suppressed == 0 {
		t.Fatal("no cloud ever intersects a title text band: sweep cannot catch regressions")
	}
	if drawn == 0 {
		t.Fatal("no cloud pixels on any title screen: clouds were over-suppressed")
	}
}

// TestLeaderHintDrawnOnTitle pins the L LEADERBOARD hint across viewport
// sizes: the element must exist, sit fully inside the frame, and actually
// light white pixels in its rows. (Regression: the bottom-anchored cascade
// once pinned the hint to the ground band with an unsatisfiable fit gate,
// so it silently never rendered at any size.)
func TestLeaderHintDrawnOnTitle(t *testing.T) {
	for _, viewH := range []int{4, 9, engine.LevelHeight} {
		for _, lv := range engine.DefaultLevels() {
			g := engine.NewGame([]*engine.Level{lv}, 40, viewH) // starts on title
			f := worldFrame(nil, g, testPal)
			var hint *titleText
			for i, e := range titleTextEls(f, g) {
				if strings.Contains(e.s, "DAILY") {
					hint = &titleTextEls(f, g)[i]
				}
			}
			if hint == nil {
				t.Fatalf("%s ViewH=%d: leaderboard hint element missing", lv.Name, viewH)
			}
			if hint.y < 0 || hint.y+5 > f.H {
				t.Fatalf("%s ViewH=%d: hint rows %d..%d exceed frame height %d", lv.Name, viewH, hint.y, hint.y+4, f.H)
			}
			white := 0
			for y := hint.y; y < hint.y+5; y++ {
				for x := range f.W {
					if f.At(x, y) == testPal.White {
						white++
					}
				}
			}
			if white == 0 {
				t.Fatalf("%s ViewH=%d: hint drew no white pixels", lv.Name, viewH)
			}
		}
	}
}

// TestDemoTitleCloudsNeverOverlapTitleText is the demo-mode half of the
// cloud sweep: attract mode draws the same full title overlay over live
// play, so its text needs the same keep-clear bands. (Regression: the
// bands were computed only for StateTitle, so demo clouds sliced
// straight through the logo and hint lines.)
func TestDemoTitleCloudsNeverOverlapTitleText(t *testing.T) {
	pal := sentinelCloudPal()
	suppressed, drawn := 0, 0
	for _, viewW := range []int{20, 30, 40} {
		for _, viewH := range []int{7, 9, 12, 15} {
			for _, lv := range engine.DefaultLevels() {
				g := engine.NewGame([]*engine.Level{lv}, viewW, viewH)
				g.BeginDemo() // StatePlaying + Demo: the attract mode
				f := worldFrame(nil, g, pal)
				bands := titleTextBands(f, g)
				if len(bands) == 0 {
					t.Fatalf("%s %dx%d: no title text bands", lv.Name, viewW, viewH)
				}
				for _, b := range bands {
					for y := b[1]; y < b[3]; y++ {
						for x := b[0]; x < b[2]; x++ {
							if f.At(x, y) == pal.Cloud {
								t.Fatalf("%s %dx%d: cloud pixel at (%d,%d) inside demo title band %v",
									lv.Name, viewW, viewH, x, y, b)
							}
						}
					}
				}

				// Sweep bookkeeping, as in the title test: the filter must
				// be live, and clouds must still draw on the demo sky.
				ox := int(g.CameraX * Pix)
				oy := int(CameraY(g) * Pix)
				for tx := range lv.Width {
					row, ok := CloudAt(tx)
					if !ok || cloudBlocked(g, tx, row) {
						continue
					}
					x0, y0 := tx*Pix-ox, row*Pix-oy
					for _, b := range bands {
						if x0 < b[2] && x0+sprW(sprCloud) > b[0] && y0 < b[3] && y0+sprH(sprCloud) > b[1] {
							suppressed++
							break
						}
					}
				}
				for y := range f.H {
					for x := range f.W {
						if f.At(x, y) == pal.Cloud {
							drawn++
						}
					}
				}
			}
		}
	}
	if suppressed == 0 {
		t.Fatal("no cloud ever intersects a demo title band: sweep cannot catch regressions")
	}
	if drawn == 0 {
		t.Fatal("no cloud pixels on any demo screen: clouds were over-suppressed")
	}
}

// TestDemoDrawsLivePlayUnderTitle pins that the demo renders live-play
// tiles under the attract overlay, not the bare title backdrop: 1-1's
// first ? block sits inside a 40-tile viewport, and only the tile
// painter draws its orange. Guards the worldFrame branch split — an
// early-return title path for demos would silently replace play with
// the backdrop.
func TestDemoDrawsLivePlayUnderTitle(t *testing.T) {
	g := engine.NewGame([]*engine.Level{engine.DefaultLevels()[0]}, 40, 12)
	g.BeginDemo()
	f := worldFrame(nil, g, testPal) // tick 0: the ? block body is in its bright phase
	for y := range f.H {
		for x := range f.W {
			if f.At(x, y) == testPal.QuestionBG {
				return // live tile painter ran under the title overlay
			}
		}
	}
	t.Fatal("demo frame has no ? block: the title backdrop replaced live play")
}
