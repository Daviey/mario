package render

import "testing"

// TestNearest256 pins the cube mapping for load-bearing palette colors.
// The saturated mid-reds MUST land on the saturated cube entries: plain
// RGB distance maps the player red #FF3B30 onto cube 203 #FF5F5F (a
// pastel pink — the "washed out red" this quantizer exists to prevent);
// OKLab keeps it on 196 #FF0000.
func TestNearest256(t *testing.T) {
	for _, tc := range []struct {
		name string
		hex  uint32
		want int
	}{
		{"player red", 0xFF3B30, 196}, // #FF0000 — never 203 #FF5F5F
		{"flag red", 0xE4221B, 160},   // #D70000
		{"sky blue", 0x5C94FC, 69},    // #5F87FF
		{"pipe green", 0x00A800, 34},  // #00AF00
		{"coin gold", 0xFCD000, 220},  // #FFD700
		{"cloud white", 0xFFFFFF, 231},
		{"hud navy", 0x0A1C6E, 18}, // stays navy, not a gray
	} {
		if got := nearest256(RGB(tc.hex)); got != tc.want {
			t.Errorf("nearest256(%s #%06X) = %d, want %d", tc.name, tc.hex, got, tc.want)
		}
	}
}

// Every palette entry (every theme included) maps inside the fixed cube
// 16-255: the base colors 0-15 are terminal-profile-dependent and must
// never be picked by the 256-tier encoder.
func TestPaletteIdx256InCube(t *testing.T) {
	base := NewPalette(Colors24)
	for name, pal := range map[string]*Palette{
		"overworld":   base,
		"underground": underground(base),
		"sky":         skyTheme(base),
		"castle":      castleTheme(base),
	} {
		for runeTag, c := range runeColors(pal) {
			if c.Idx256 < 16 || c.Idx256 > 255 {
				t.Errorf("%s rune %q: Idx256 = %d, want 16..255", name, runeTag, c.Idx256)
			}
		}
		for _, c := range []Color{pal.FlagRed, pal.Player, pal.HUDBG, pal.StatusBG, pal.OverlayBG} {
			if c.Idx256 < 16 || c.Idx256 > 255 {
				t.Errorf("%s: Idx256 = %d, want 16..255", name, c.Idx256)
			}
		}
	}
}
