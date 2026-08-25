package render

import (
	"testing"

	"mario/engine"
)

// titleBands recomputes the title-screen text layout (independently of
// drawOverlayPx) as x0,y0,x1,y1 pixel rects: MARIO logo, SUPER CLI subtitle,
// PRESS ANY KEY blink. It pins the layout the cloud filter must respect.
func titleBands(f *Frame) [][4]int {
	groundTop := f.H - 2*Pix
	castY := groundTop - 10
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
		logoY := max(2, min(f.H/12, castY-18))
		add("MARIO", logoY, 2)
		if subY := logoY + 12; subY+5 <= castY {
			add(pickTextPx([]string{"SUPER CLI EDITION", "SUPER CLI"}, f.W-2), subY, 1)
		}
	}
	add(pickTextPx([]string{"PRESS ANY KEY", "ANY KEY"}, f.W-2), min(castY+11, f.H-5), 1)
	return bands
}

// TestTitleCloudsNeverOverlapTitleText sweeps the title screen of every
// built-in level at several viewport sizes: no cloud may paint a pixel
// inside any title text rect. Clouds and the subtitle are both white, so
// the check works from cloud sprite stencils, not pixel colors.
func TestTitleCloudsNeverOverlapTitleText(t *testing.T) {
	suppressed := 0
	for _, viewW := range []int{20, 30, 40} {
		for _, viewH := range []int{7, 9, 12, 15} {
			for _, lv := range engine.DefaultLevels() {
				g := engine.NewGame([]*engine.Level{lv}, viewW, viewH) // starts on title
				f := worldFrame(g, testPal)
				bands := titleBands(f)

				// Layout pin: each band must actually hold its text.
				has := func(b [4]int, c Color) bool {
					for y := b[1]; y < b[3]; y++ {
						for x := b[0]; x < b[2]; x++ {
							if f.At(x, y) == c {
								return true
							}
						}
					}
					return false
				}
				logo, sub, blink := 0, 0, 0
				for i, b := range bands {
					switch {
					case i == 0 && len(bands) == 3:
						if !has(b, testPal.FlagRed) {
							t.Fatalf("%s %dx%d: logo band %v has no MARIO pixels", lv.Name, viewW, viewH, b)
						}
						logo = 1
					case i == 1 && len(bands) == 3, i == 0 && len(bands) == 2:
						if b[1] < bands[0][3]+7 { // subtitle sits below the logo
							if !has(b, testPal.White) {
								t.Fatalf("%s %dx%d: subtitle band %v has no white pixels", lv.Name, viewW, viewH, b)
							}
							sub = 1
						} else if !has(b, testPal.GoldLight) {
							t.Fatalf("%s %dx%d: blink band %v has no PRESS ANY KEY pixels", lv.Name, viewW, viewH, b)
						}
					default:
						if !has(b, testPal.GoldLight) {
							t.Fatalf("%s %dx%d: blink band %v has no PRESS ANY KEY pixels", lv.Name, viewW, viewH, b)
						}
						blink = 1
					}
				}
				if len(bands) == 3 && (logo+sub+blink) < 2 {
					t.Fatalf("%s %dx%d: expected text bands incomplete", lv.Name, viewW, viewH)
				}

				// Contract: no cloud stencil pixel may land in any band.
				ox := int(g.CameraX * Pix)
				oy := int(CameraY(g) * Pix)
				for tx := 0; tx < lv.Width; tx++ {
					row, _, ok := CloudAt(tx)
					if !ok || cloudBlocked(g, tx, row) {
						continue
					}
					x0, y0 := tx*Pix-ox, row*Pix-oy
					hit := false
					for ry, art := range sprCloud {
						for rx, r := range art {
							if r != 'W' {
								continue
							}
							px, py := x0+rx, y0+ry
							if px < 0 || py < 0 || px >= f.W || py >= f.H {
								continue
							}
							for _, b := range bands {
								if px >= b[0] && px < b[2] && py >= b[1] && py < b[3] {
									t.Fatalf("%s %dx%d: cloud (tx=%d row=%d) paints (%d,%d) inside title text band %v",
										lv.Name, viewW, viewH, tx, row, px, py, b)
								}
							}
						}
					}
					// Would this cloud have intersected a band? Count it so
					// the sweep proves the filter is live, not vacuous.
					for _, b := range bands {
						if x0 < b[2] && x0+sprW(sprCloud) > b[0] && y0 < b[3] && y0+sprH(sprCloud) > b[1] {
							hit = true
						}
					}
					if hit {
						suppressed++
					}
				}
			}
		}
	}
	if suppressed == 0 {
		t.Fatal("no cloud ever intersects a title text band: sweep cannot catch regressions")
	}
}
