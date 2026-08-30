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

// titleBands recomputes the full-mode title-screen text layout
// (independently of titleTextEls) as x0,y0,x1,y1 pixel rects: MARIO logo,
// SUPER CLI subtitle, build version, PRESS ANY KEY blink, L LEADERBOARD hint.
func titleBands(f *Frame) [][4]int {
	castY := titleCastY(f)
	castH := 2 * sprH(sprMarioSmall)
	var bands [][4]int
	add := func(s string, y, scale int) {
		w := textWidthPx(s, scale)
		x := (f.W - w) / 2
		if x < 0 {
			x = 0
		}
		h := 5 * scale
		bands = append(bands, [4]int{x, y, min(x+w, f.W), min(y+h, f.H)})
	}

	if castY >= 13 {
		logoY := max(2, min(f.H/12, castY-10))
		add("MARIO", logoY, 2)
		if subY := logoY + 15; subY+5 <= castY {
			add(pickTextPx([]string{"SUPER CLI EDITION", "SUPER CLI"}, f.W-2), subY, 1)
			tailY := subY + 5
			if vc := versionCandidates(Version); len(vc) > 0 {
				if v := pickTextPx(vc, f.W-2); v != "" {
					if verY := subY + 7; verY+5 <= castY {
						add(v, verY, 1)
						tailY = verY + 5
					}
				}
			}
			// about banner (mirrors titleTextEls)
			if fanY := tailY + 2; fanY+5 <= castY {
				add(pickTextPx([]string{"UNOFFICIAL FAN GAME", "FAN GAME"}, f.W-2), fanY, 1)
				if nY := fanY + 6; nY+5 <= castY {
					add(pickTextPx([]string{"NOT AFFILIATED WITH NINTENDO", "NO NINTENDO AFFILIATION", "NOT NINTENDO"}, f.W-2), nY, 1)
				}
			}
		}
	}

	pressY := min(castY+castH+1, f.H-5) // ground band: first line under the cast
	add(pickTextPx([]string{"PRESS ANY KEY", "ANY KEY"}, f.W-2), pressY, 1)
	if hintY := pressY + 6; hintY+5 <= f.H { // second ground-band line, flush bottom
		add(pickTextPx([]string{"L LEADERBOARD", "L BOARD"}, f.W-2), hintY, 1)
	}
	return bands
}

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
				f := worldFrame(g, pal)
				bands := titleBands(f)
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
					row, _, ok := CloudAt(tx)
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
			f := worldFrame(g, testPal)
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
