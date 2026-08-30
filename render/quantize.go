package render

import "math"

// The 256-color fallback palette.
//
// Terminals that speak a "-256color" TERM render the fixed xterm cube
// (colors 16-231, six levels per channel) and the 24-step gray ramp
// (232-255) exactly as specified, no matter how their 16 base colors
// are configured — so one SGR 38;5 sequence reproduces the intended
// hue on gnome-terminal's Tango profile, Solarized, Apple Terminal and
// mosh's cell model alike. The base colors 0-15 are deliberately NOT
// candidates here: they are whatever the user's profile says they are
// (Tango color 9 renders #EF2929, Solarized #DC322F — the "washed out
// red" class of bug this file exists to avoid).
//
// nearest256 maps one palette RGB onto that fixed cube. Distance is
// measured in OKLab: plain RGB distance maps saturated mid-tones to
// pastel cube entries (the player red #FF3B30 lands on cube 203
// #FF5F5F — pink), while OKLab's lightness/chroma balance keeps it on
// the saturated 196 #FF0000. Computed once per Color at palette build;
// pure deterministic math, no runtime cost per frame.

var cube240 [240]struct {
	r, g, b  uint8
	l, a, bl float64 // OKLab coordinates
}

func init() {
	steps := [6]uint8{0, 95, 135, 175, 215, 255}
	i := 0
	for r := range 6 {
		for g := range 6 {
			for b := range 6 {
				c := &cube240[i]
				c.r, c.g, c.b = steps[r], steps[g], steps[b]
				c.l, c.a, c.bl = oklab(c.r, c.g, c.b)
				i++
			}
		}
	}
	for k := range 24 {
		v := uint8(8 + k*10)
		c := &cube240[216+k]
		c.r, c.g, c.b = v, v, v
		c.l, c.a, c.bl = oklab(v, v, v)
	}
}

// nearest256 returns the fixed-cube index (16-255) whose OKLab color
// is closest to c.
func nearest256(c RGB) int {
	l, a, b := oklab(byte(c>>16), byte(c>>8), byte(c))
	best, bestD := 16, math.MaxFloat64
	for i := range cube240 {
		q := &cube240[i]
		dl, da, db := l-q.l, a-q.a, b-q.bl
		if d := dl*dl + da*da + db*db; d < bestD {
			best, bestD = 16+i, d
		}
	}
	return best
}

// oklab converts one sRGB triple to OKLab (Björn Ottosson's reference
// transform). Chosen over RGB distance for the saturation property
// documented above.
func oklab(r, g, b uint8) (l, a, bb float64) {
	R, G, B := lin(float64(r)/255), lin(float64(g)/255), lin(float64(b)/255)
	L := 0.4122214708*R + 0.5363325363*G + 0.0514459929*B
	M := 0.2119034982*R + 0.6806995451*G + 0.1073969566*B
	S := 0.0883024619*R + 0.2817188376*G + 0.6299787005*B
	l_, m_, s_ := math.Cbrt(L), math.Cbrt(M), math.Cbrt(S)
	return 0.2104542553*l_ + 0.7936177850*m_ - 0.0040720468*s_,
		1.9779984951*l_ - 2.4285922050*m_ + 0.4505937099*s_,
		0.0259040371*l_ + 0.7827717662*m_ - 0.8086757660*s_
}

// lin is the sRGB→linear transfer function.
func lin(c float64) float64 {
	if c <= 0.04045 {
		return c / 12.92
	}
	return math.Pow((c+0.055)/1.055, 2.4)
}
