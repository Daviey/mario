package render

import (
	"strings"
	"sync"

	"github.com/Daviey/mario/engine"
)

// RGB is a 24-bit color (0xRRGGBB).
type RGB uint32

// Color pairs a truecolor value with its fallback indexes for lesser
// terminals: Idx256 points into the fixed xterm cube (16-255, rendered
// exactly by every -256color terminal), ANSI into the base-16 palette
// (0-15, rendered as whatever the user's profile says).
type Color struct {
	RGB    RGB
	Idx256 int
	ANSI   int
}

func color(hex uint32, ansi int) Color {
	return Color{RGB: RGB(hex), Idx256: nearest256(RGB(hex)), ANSI: ansi}
}

// ColorMode is the color depth a Screen serializes: how palette colors
// are encoded into SGR sequences (see NewPalette and ColorDepthFor).
type ColorMode int

// The three color depths, in increasing fidelity.
const (
	Colors16  ColorMode = 16  // base ANSI palette, SGR 30-37/90-97
	Colors256 ColorMode = 256 // fixed xterm cube, SGR 38;5 — exact on every -256color terminal
	Colors24  ColorMode = 24  // truecolor, SGR 38;2
)

// Palette holds every color the renderer uses.
type Palette struct {
	Colors ColorMode // Colors24, Colors256 or Colors16 — the SGR encoding this palette emits

	Sky          Color // classic SMB sky blue
	GroundLight  Color // sunlit top edge of terrain
	GroundMid    Color
	GroundDark   Color // deep earth / seams
	BrickLight   Color
	BrickDark    Color // mortar shadow
	QuestionBG   Color
	QuestionDim  Color // flash partner of QuestionBG; the mark never dims
	QuestionHi   Color
	QuestionMark Color
	UsedBG       Color
	Pole         Color
	FlagRed      Color
	Coin         Color
	GoldLight    Color
	Player       Color
	Skin         Color
	Overall      Color
	Dark         Color // eyes, shoes, outlines
	Goomba       Color
	GoombaFlat   Color
	Green        Color
	GreenLight   Color
	GreenDark    Color
	KoopaSkin    Color
	Cloud        Color
	Door         Color
	Window       Color
	Text         Color
	TextDim      Color
	HUDBG        Color
	StatusBG     Color
	OverlayBG    Color
	OverlayFG    Color
	White        Color
}

// TrueColorTerm reports whether a TERM value names a terminal that
// renders 24-bit color sequences without any COLORTERM hint. Hosts that
// only ever see TERM (mosh overwrites the child's TERM to xterm-256color
// and drops COLORTERM entirely) use this on the TERM the client sent at
// ssh time to decide whether the game may run truecolor.
func TrueColorTerm(term string) bool {
	// kitty and Alacritty name themselves in TERM and are always
	// 24-bit; like ghostty/wezterm they need no COLORTERM hint.
	return strings.Contains(term, "truecolor") ||
		strings.Contains(term, "direct") ||
		term == "ghostty" || term == "xterm-ghostty" || term == "wezterm" ||
		term == "kitty" || term == "xterm-kitty" || term == "alacritty"
}

// ColorDepthFor picks the SGR color mode for a terminal described only
// by its TERM (and a COLORTERM the environment carried, if any):
// 24-bit when the terminal is known to render it (TrueColorTerm
// families or a COLORTERM truecolor hint), otherwise the fixed xterm
// whenever TERM advertises 256 colors — the convention every
// -256color terminal and mosh's cell model honors — and base-16 only
// for terminals that claim neither. Shared decision rule for the
// native runner, the SSH host's sessions and the mosh child alike.
func ColorDepthFor(term, colorterm string) ColorMode {
	if trueColorHint(colorterm) || TrueColorTerm(term) {
		return Colors24
	}
	if strings.Contains(term, "256") {
		return Colors256
	}
	return Colors16
}

// trueColorHint reports whether a COLORTERM value advertises 24-bit
// color. The spellings are the set terminals and launchers actually
// write ("yes"/"true" included); any other value — e.g. "256color" —
// is not a truecolor claim and falls through to the TERM check.
func trueColorHint(colorterm string) bool {
	switch strings.ToLower(colorterm) {
	case "truecolor", "24bit", "24-bit", "yes", "true":
		return true
	}
	return false
}

// NewPalette returns the game palette. colors picks the SGR encoding:
// Colors24 emits 24-bit sequences, Colors256 the fixed xterm cube
// (38;5), Colors16 the base ANSI palette.
func NewPalette(mode ColorMode) *Palette {
	return &Palette{
		Colors:       mode,
		Sky:          color(0x5C94FC, 12),
		GroundLight:  color(0xF8B060, 11),
		GroundMid:    color(0xC84C0C, 1),
		GroundDark:   color(0x7C2800, 1),
		BrickLight:   color(0xD86818, 3), // 208%16 == 0 once slipped through here: black bricks in 16-color mode
		BrickDark:    color(0x6B2B00, 1),
		QuestionBG:   color(0xFC9838, 11),
		QuestionDim:  color(0xE08018, 3),
		QuestionHi:   color(0xFFD9A0, 11),
		QuestionMark: color(0xFFF8E0, 15),
		Pole:         color(0x98E858, 10),
		FlagRed:      color(0xE4221B, 9),
		Coin:         color(0xFCD000, 11),
		GoldLight:    color(0xFFF3B0, 11),
		Player:       color(0xFF3B30, 9),
		Skin:         color(0xFFC89E, 6),
		UsedBG:       color(0x9C5A24, 3),
		Overall:      color(0x2B5DD7, 12),
		Dark:         color(0x1A0E04, 0),
		Goomba:       color(0xC86428, 3),
		GoombaFlat:   color(0x8B4513, 3),
		Green:        color(0x00A800, 2),
		GreenLight:   color(0x80D010, 10),
		GreenDark:    color(0x004000, 2),
		KoopaSkin:    color(0xF8D878, 3),
		Cloud:        color(0xFFFFFF, 15),
		Door:         color(0x2B1A0A, 0),
		Window:       color(0x101820, 0),
		Text:         color(0xFFFFFF, 15),
		TextDim:      color(0x9FB6D9, 7),
		HUDBG:        color(0x0A1C6E, 4),
		StatusBG:     color(0x071330, 4),
		OverlayBG:    color(0xFFF8E7, 15),
		OverlayFG:    color(0x101010, 0),
		White:        color(0xFFFFFF, 15),
	}
}

// underground re-skins the palette for the underground world: black sky,
// blue ground and bricks — the classic 1-2 look.
func underground(p *Palette) *Palette {
	q := *p
	q.Sky = color(0x000000, 0)
	q.GroundLight = color(0x8888D8, 12)
	q.GroundMid = color(0x3850C8, 4)
	q.GroundDark = color(0x1A2A70, 4)
	q.BrickLight = color(0x5878E8, 12)
	q.BrickDark = color(0x2030A0, 4)
	q.Cloud = color(0x2A2A4A, 4)
	return &q
}

// skyTheme re-skins the athletic world: pale sky, sandstone terrain.
func skyTheme(p *Palette) *Palette {
	q := *p
	q.Sky = color(0x88C8F8, 12)
	q.GroundLight = color(0xF8F0D0, 15)
	q.GroundMid = color(0xD8B070, 11)
	q.GroundDark = color(0x8A6230, 3)
	q.BrickLight = color(0xE8C890, 11)
	q.BrickDark = color(0x8A6230, 3)
	return &q
}

// castleTheme re-skins the finale: black sky, grey stone, dead air.
func castleTheme(p *Palette) *Palette {
	q := *p
	q.Sky = color(0x000000, 0)
	q.GroundLight = color(0xC8C8C8, 7)
	q.GroundMid = color(0x909098, 7)
	q.GroundDark = color(0x484850, 8)
	q.BrickLight = color(0xA8A8B0, 7)
	q.BrickDark = color(0x404048, 8)
	q.Cloud = color(0x2A2A2A, 8)
	return &q
}

// themedCache memoizes themed palettes per (base palette, theme): the
// theme used to be rebuilt — a full Palette copy — every frame. Keys
// are palette contents, not pointers: NewPalette output depends only on
// the color mode, so production holds at most three entries however
// many sessions build palettes, while test-crafted variants stay
// distinct.
var themedCache sync.Map // themeKey -> *Palette

type themeKey struct {
	p     Palette
	theme engine.Theme
}

// paletteFor returns the palette for the current level's theme, built
// once per (base palette, theme). Every caller renders a game with a
// loaded level — the viewport math dereferences g.Level regardless.
func paletteFor(g *engine.Game, p *Palette) *Palette {
	var build func(*Palette) *Palette
	switch g.Level.Theme {
	case engine.ThemeUnderground:
		build = underground
	case engine.ThemeSky:
		build = skyTheme
	case engine.ThemeCastle:
		build = castleTheme
	default:
		return p
	}
	k := themeKey{*p, g.Level.Theme}
	if v, ok := themedCache.Load(k); ok {
		return v.(*Palette)
	}
	q := build(p)
	themedCache.Store(k, q)
	return q
}

// runeColors maps sprite-art runes to palette swatches. Cached per
// palette contents (see themedCache for why contents, not pointers) —
// callers must treat the map as read-only (fire/star variants are
// separately cached, never built by mutating this one).
var runeColorsCache sync.Map // Palette -> map[rune]Color

func runeColors(p *Palette) map[rune]Color {
	if v, ok := runeColorsCache.Load(*p); ok {
		return v.(map[rune]Color)
	}
	rc := map[rune]Color{
		'R': p.Player, 'S': p.Skin, 'D': p.Dark, 'B': p.Overall,
		'W': p.White, 'C': p.Cloud, 'Y': p.Coin, 'L': p.GoldLight,
		'O': p.GroundMid, 'o': p.GroundLight,
		'G': p.Green, 'E': p.GreenLight, 'g': p.GreenDark,
		'K': p.KoopaSkin, 'n': p.Goomba,
	}
	runeColorsCache.Store(*p, rc)
	return rc
}
