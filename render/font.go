package render

// A compact 3×5 pixel font for in-world overlays. Glyphs are 3 px wide and
// 5 px tall; drawTextPx inserts 1 px of tracking (4 px per character).

var font3x5 = map[rune][]string{
	'A': {"###", "#.#", "###", "#.#", "#.#"},
	'B': {"##.", "#.#", "##.", "#.#", "##."},
	'C': {"###", "#..", "#..", "#..", "###"},
	'D': {"##.", "#.#", "#.#", "#.#", "##."},
	'E': {"###", "#..", "##.", "#..", "###"},
	'F': {"###", "#..", "##.", "#..", "#.."},
	'G': {"###", "#..", "#.#", "#.#", "###"},
	'H': {"#.#", "#.#", "###", "#.#", "#.#"},
	'I': {"###", ".#.", ".#.", ".#.", "###"},
	'J': {"..#", "..#", "..#", "#.#", "###"},
	'K': {"#.#", "#.#", "##.", "#.#", "#.#"},
	'L': {"#..", "#..", "#..", "#..", "###"},
	'M': {"#.#", "###", "###", "#.#", "#.#"},
	'N': {"##.", "#.#", "#.#", "#.#", "#.#"},
	'O': {"###", "#.#", "#.#", "#.#", "###"},
	'P': {"###", "#.#", "###", "#..", "#.."},
	'Q': {"###", "#.#", "###", "..#", "..#"},
	'R': {"###", "#.#", "##.", "#.#", "#.#"},
	'S': {"###", "#..", "###", "..#", "###"},
	'T': {"###", ".#.", ".#.", ".#.", ".#."},
	'U': {"#.#", "#.#", "#.#", "#.#", "###"},
	'V': {"#.#", "#.#", "#.#", "#.#", ".#."},
	'W': {"#.#", "#.#", "###", "###", "#.#"},
	'X': {"#.#", "#.#", ".#.", "#.#", "#.#"},
	'Y': {"#.#", "#.#", "###", ".#.", ".#."},
	'Z': {"###", "..#", ".#.", "#..", "###"},
	'0': {"###", "#.#", "#.#", "#.#", "###"},
	'1': {".#.", "##.", ".#.", ".#.", "###"},
	'2': {"###", "..#", ".#.", "#..", "###"},
	'3': {"###", "..#", ".##", "..#", "###"},
	'4': {"#.#", "#.#", "###", "..#", "..#"},
	'5': {"###", "#..", "###", "..#", "###"},
	'6': {"#..", "#..", "###", "#.#", "###"},
	'7': {"###", "..#", ".#.", ".#.", ".#."},
	'8': {"###", "#.#", "###", "#.#", "###"},
	'9': {"###", "#.#", "###", "..#", "..#"},
	'!': {".#.", ".#.", ".#.", "...", ".#."},
	'.': {"...", "...", "...", "...", ".#."},
	'-': {"...", "...", "###", "...", "..."},
	'+': {"...", ".#.", "###", ".#.", "..."},
	'/': {"..#", "..#", ".#.", "#..", "#.."},
	':': {"...", ".#.", "...", ".#.", "..."},
	'?': {"###", "..#", ".#.", "...", ".#."},
	' ': {"...", "...", "...", "...", "..."},
}

// textWidthPx returns the pixel width of a string at a given scale.
func textWidthPx(s string, scale int) int {
	n := 0
	for range s {
		n++
	}
	if n == 0 {
		return 0
	}
	return (4*n - 1) * scale
}

// drawTextPx stamps text into the frame, upper-casing unknown runes.
func drawTextPx(f *Frame, x, y int, s string, c Color, scale int) {
	cx := x
	for _, r := range s {
		g, ok := font3x5[r]
		if !ok {
			g, ok = font3x5[toUpper(r)]
			if !ok {
				g = font3x5['?']
			}
		}
		for ry, row := range g {
			for rx, bit := range row {
				if bit == '#' {
					if scale == 1 {
						f.Set(cx+rx, y+ry, c)
					} else {
						f.Fill(cx+rx*scale, y+ry*scale, scale, scale, c)
					}
				}
			}
		}
		cx += 4 * scale
	}
}

// drawBannerPx draws a centered banner: solid fill, 2 px padding, then text.
func drawBannerPx(f *Frame, y int, s string, fg, bg Color, p *Palette) {
	w := textWidthPx(s, 1) + 4
	x := (f.W - w) / 2
	f.Fill(x, y-2, w, 9, bg)
	drawTextPx(f, x+2, y, s, fg, 1)
}

// drawCenterPx draws centered plain text.
func drawCenterPx(f *Frame, y int, s string, c Color, scale int) {
	drawTextPx(f, (f.W-textWidthPx(s, scale))/2, y, s, c, scale)
}

func toUpper(r rune) rune {
	if r >= 'a' && r <= 'z' {
		return r - 32
	}
	return r
}
