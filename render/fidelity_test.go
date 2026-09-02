package render

import (
	"strings"
	"testing"

	"github.com/Daviey/mario/engine"
)

// Tests for the SMB1-fidelity render additions (contract S9): sprite
// inventory and dimensions, the underwater/night palettes, the new
// entity draw hooks, and the retainer cutscene.

// TestSmbSpriteDimensions pins every contract-S9 sprite to its exact
// rune-art footprint. The draw code anchors off these sizes (and the
// art grammar requires uniform row lengths), so a ragged edit must
// fail here rather than shear a sprite at runtime.
func TestSmbSpriteDimensions(t *testing.T) {
	for _, tc := range []struct {
		name string
		art  []string
		w, h int
	}{
		{"sprPodoboo", sprPodoboo, 6, 6},
		{"sprCheep", sprCheep, 7, 5},
		{"sprCheepGray", sprCheepGray, 7, 5},
		{"sprBloober", sprBloober, 6, 7},
		{"sprHammerBro", sprHammerBro, 7, 13},
		{"sprHammerBroWalk", sprHammerBroWalk, 7, 13},
		{"sprHammer", sprHammer, 5, 5},
		{"sprBuzzy", sprBuzzy, 6, 8},
		{"sprBuzzyWalk", sprBuzzyWalk, 6, 8},
		{"sprKoopaRed", sprKoopaRed, 6, 9},
		{"sprKoopaRedWalk", sprKoopaRedWalk, 6, 9},
		{"sprParaRed", sprParaRed, 8, 9},
		{"sprParaRedWalk", sprParaRedWalk, 8, 9},
		{"sprToad", sprToad, 7, 8},
		{"sprPrincess", sprPrincess, 7, 12},
		{"sprSpring", sprSpring, 7, 4},
		{"sprSpringDown", sprSpringDown, 7, 4},
		{"sprSpringHalf", sprSpringHalf, 7, 3},
		{"sprSpringArmed", sprSpringArmed, 7, 3},
		{"sprLift", sprLift, 4, 2},
		{"sprLiftFlimsy", sprLiftFlimsy, 4, 2},
	} {
		if gotW, gotH := sprW(tc.art), sprH(tc.art); gotW != tc.w || gotH != tc.h {
			t.Errorf("%s is %dx%d, want %dx%d", tc.name, gotW, gotH, tc.w, tc.h)
		}
		for i, row := range tc.art {
			if len([]rune(row)) != tc.w {
				t.Errorf("%s row %d has %d runes, want %d", tc.name, i, len([]rune(row)), tc.w)
			}
		}
	}
}

// TestSmbSpriteRunesKnown guards the art grammar: every non-transparent
// rune must resolve in the shared rune palette (or the sprite's own
// variant map), or DrawSprite would silently skip pixels.
func TestSmbSpriteRunesKnown(t *testing.T) {
	rc := runeColors(testPal)
	variants := map[string]map[rune]Color{
		"sprCheepGray":  cheepGrayColors(testPal),
		"sprKoopaRed":   koopaRedColors(testPal),
		"sprBuzzyShell": buzzyShellColors(testPal),
		"sprPrincess":   princessColors(testPal),
	}
	arts := map[string][]string{
		"sprPodoboo": sprPodoboo, "sprCheep": sprCheep, "sprCheepGray": sprCheepGray,
		"sprBloober": sprBloober, "sprHammerBro": sprHammerBro, "sprHammerBroWalk": sprHammerBroWalk,
		"sprHammer": sprHammer, "sprBuzzy": sprBuzzy, "sprBuzzyWalk": sprBuzzyWalk,
		"sprKoopaRed": sprKoopaRed, "sprKoopaRedWalk": sprKoopaRedWalk,
		"sprParaRed": sprParaRed, "sprParaRedWalk": sprParaRedWalk,
		"sprToad": sprToad, "sprPrincess": sprPrincess,
		"sprSpring": sprSpring, "sprSpringDown": sprSpringDown,
		"sprLift": sprLift, "sprLiftFlimsy": sprLiftFlimsy,
		"sprShell(buzzy)": sprShell,
	}
	for name, art := range arts {
		cols := rc
		if v, ok := variants[name]; ok {
			cols = v
		}
		for y, row := range art {
			for x, r := range row {
				if r == '.' || r == ' ' {
					continue
				}
				if _, ok := cols[r]; !ok {
					t.Errorf("%s rune %q at (%d,%d) has no color", name, r, x, y)
				}
			}
		}
	}
}

// TestRetainerTextCharset pins the cutscene text to the arcade font:
// every glyph must exist in font3x5 (the HUD parity charset rules apply
// to any HUD-ish text), and the pinned no-comma deviation must hold —
// a stray ',' would render as garbage.
func TestRetainerTextCharset(t *testing.T) {
	for name, s := range map[string]string{
		"toad":     retainerTextToad,
		"princess": retainerTextPrincess,
		"press":    retainerTextPress,
	} {
		if strings.Contains(s, ",") {
			t.Errorf("%s text contains a comma (charset has none): %q", name, s)
		}
		for _, r := range s {
			if _, ok := font3x5[r]; !ok {
				t.Errorf("%s text rune %q missing from font3x5", name, r)
			}
		}
	}
}

// TestWrapTextPx pins the word-wrap: lines fit the width, wrap on
// spaces only, and reassemble into the original text.
func TestWrapTextPx(t *testing.T) {
	s := "THANK YOU MARIO BUT OUR PRINCESS IS IN ANOTHER CASTLE!"
	lines := wrapTextPx(s, 100, 1)
	if len(lines) < 2 {
		t.Fatalf("expected wrapping at 100px, got %d lines", len(lines))
	}
	for i, ln := range lines {
		if textWidthPx(ln, 1) > 100 {
			t.Errorf("line %d overflows: %q", i, ln)
		}
		if strings.HasPrefix(ln, " ") || strings.HasSuffix(ln, " ") {
			t.Errorf("line %d has stray edge spaces: %q", i, ln)
		}
	}
	if got := strings.Join(lines, " "); got != s {
		t.Errorf("wrap lost text: %q", got)
	}
	// A width that fits the whole line keeps it on one line.
	if got := wrapTextPx("PRESS ANY KEY", textWidthPx("PRESS ANY KEY", 1), 1); len(got) != 1 {
		t.Errorf("exact-fit text wrapped: %v", got)
	}
}

// TestNewThemePalettes checks the S9 themes: the underwater skin turns
// the sky to water-blue and the terrain teal (distinct from the
// underground's black), and the night pass darkens the sky while the
// base theme still shows through for terrain.
func TestNewThemePalettes(t *testing.T) {
	base := NewPalette(Colors24)
	water := underwaterTheme(base)
	if water.Sky == base.Sky || water.Sky == underground(base).Sky {
		t.Error("underwater sky must differ from base and underground")
	}
	if water.GroundMid == base.GroundMid || water.GroundMid == underground(base).GroundMid {
		t.Error("underwater terrain must be its own teal")
	}
	night := nightTheme(base)
	if night.Sky == base.Sky {
		t.Error("night sky must darken")
	}
	if night.Green == base.Green || night.GreenLight == base.GreenLight {
		t.Error("night hills must go dark blue")
	}
	// paletteFor honors the theme and the Night flag, stacked.
	g := newGame(t)
	if got := paletteFor(g, testPal); got != testPal {
		t.Error("overworld day must use the base palette")
	}
	g.Level.Theme = engine.ThemeUnderwater
	if got := paletteFor(g, testPal); got.Sky != water.Sky {
		t.Error("paletteFor ignored ThemeUnderwater")
	}
	g.Level.Theme = engine.ThemeSky
	g.Level.Night = true
	sk := nightTheme(skyTheme(testPal)).Sky
	if got := paletteFor(g, testPal); got.Sky != sk {
		t.Error("paletteFor must stack night on the level theme")
	}
}

// TestUnderwaterSkipsClouds: an underwater level draws no cloud pixels
// — the whole viewport is water palette.
func TestUnderwaterSkipsClouds(t *testing.T) {
	// Paint the sky dressing directly (no camera): the day frame must
	// hold cloud pixels, the underwater frame none.
	paint := func(theme engine.Theme, under bool) int {
		g := newGame(t)
		g.Level.Theme = theme
		g.Level.Underwater = under
		f := NewFrame(g.Level.Width*Pix, engine.LevelHeight*Pix, Color{})
		drawDecorations(f, g, testPal, runeColors(testPal), 0, 0, nil)
		n := 0
		for y := range f.H {
			for x := range f.W {
				if f.At(x, y) == testPal.Cloud {
					n++
				}
			}
		}
		return n
	}
	if day := paint(engine.ThemeOverworld, false); day == 0 {
		t.Fatal("overworld dressing painted no clouds; test premise broken")
	}
	if got := paint(engine.ThemeUnderwater, true); got != 0 {
		t.Errorf("underwater painted %d cloud pixels; the water has no sky dressing", got)
	}
	if got := paint(engine.ThemeUnderwater, false); got != 0 {
		t.Errorf("ThemeUnderwater alone painted %d cloud pixels", got)
	}
	if got := paint(engine.ThemeOverworld, true); got != 0 {
		t.Errorf("Underwater flag alone painted %d cloud pixels", got)
	}
}

// TestNewEntitiesDrawn smoke-checks every S9 draw hook: populate each
// new Game slice, render, and require non-sky pixels in the sprite's
// screen neighbourhood. Draw-call ordering regressions (a slice never
// reached) fail here.
func TestNewEntitiesDrawn(t *testing.T) {
	g := newGame(t)
	g.Tick = 61 // odd spin/stride phases
	g.Podoboos = []*engine.Podoboo{{Pos: engine.Vec{X: 4, Y: 9}, W: 0.8, H: 0.8, BaseY: 13}}
	g.Cheeps = []*engine.Cheep{
		{Pos: engine.Vec{X: 7, Y: 8}, Vel: engine.Vec{X: -0.1}, W: 0.9, H: 0.55, Red: true},
		{Pos: engine.Vec{X: 11, Y: 6}, Vel: engine.Vec{X: -0.06}, W: 0.9, H: 0.55},
	}
	g.Bloopers = []*engine.Bloober{{Pos: engine.Vec{X: 15, Y: 6}, W: 0.9, H: 1.0}}
	g.HammerBros = []*engine.HammerBro{{Pos: engine.Vec{X: 3, Y: 10.3}, W: 0.9, H: 1.7, Dir: -1}}
	g.Hammers = []*engine.Hammer{{Pos: engine.Vec{X: 17, Y: 9}, Rot: 4}}
	g.Lifts = []*engine.Lift{
		{X: 2, Y: 8, W: 3, Kind: engine.LiftVert},
		{X: 6, Y: 8, W: 3, Kind: engine.LiftFlimsy},
	}
	g.Springs = []*engine.Spring{{X: 13, Y: 12.5, Compress: 10}}
	f := worldFrame(nil, g, testPal)
	sky := paletteFor(g, testPal).Sky
	spots := []struct {
		name   string
		cx, cy int // world-tile centre to sample around
		r      int // pixel radius
	}{
		{"podoboo", 4, 10, 3},
		{"cheep red", 7, 8, 3},
		{"cheep gray", 11, 6, 3},
		{"bloober", 15, 6, 3},
		{"hammer bro", 3, 11, 4},
		{"hammer", 17, 9, 3},
		{"lift", 3, 8, 2},
		{"flimsy lift", 7, 8, 2},
		{"spring", 13, 12, 3},
	}
	// Camera Y centres the ground-level player; compute it the way the
	// renderer does so the sample window lands on the sprite.
	camY := CameraY(g)
	for _, sp := range spots {
		px := int(float64(sp.cx) * Pix)
		py := int((float64(sp.cy) - camY) * Pix)
		hit := false
		for dx := -sp.r * Pix / 2; dx <= sp.r*Pix/2 && !hit; dx++ {
			for dy := -sp.r * Pix; dy <= sp.r*Pix && !hit; dy++ {
				if f.At(px+dx, py+dy) != sky {
					hit = true
				}
			}
		}
		if !hit {
			t.Errorf("%s drew no pixels around (%d,%d)", sp.name, sp.cx, sp.cy)
		}
	}
}

// TestRetainerScene checks the cutscene: dark interior, the message
// word-wrapped near the top in white, the NPC on the floor at
// RetainerAt, and the princess variant's blinking prompt.
func TestRetainerScene(t *testing.T) {
	for _, tc := range []struct {
		name     string
		retainer int
		wantMsg  string
	}{
		{"toad", 1, retainerTextToad},
		{"princess", 2, retainerTextPrincess},
	} {
		g := newGame(t)
		g.Level.Retainer = tc.retainer
		g.Level.RetainerAt = engine.Vec{X: 14, Y: 12}
		g.State = engine.StateRetainer
		g.Tick = 20 // blink-on phase for the prompt
		f := worldFrame(nil, g, testPal)
		// The first wrapped line's glyphs sit in the top band.
		line := wrapTextPx(tc.wantMsg, f.W-8, 1)[0]
		x0 := (f.W - textWidthPx(line, 1)) / 2
		if x0 <= 0 {
			t.Fatalf("%s: first line not centered (x0=%d)", tc.name, x0)
		}
		found := false
		for y := 3; y < 8 && !found; y++ {
			if f.At(x0, y) == testPal.White || f.At(x0+1, y) == testPal.White {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: message text missing near top", tc.name)
		}
		// The NPC stands at RetainerAt: non-background pixels on the
		// floor above the surface row.
		nx := int((float64(g.Level.RetainerAt.X) + 0.5) * Pix)
		npcHit := false
		for dy := -sprH(sprPrincess); dy < 0 && !npcHit; dy++ {
			for dx := -4; dx <= 4 && !npcHit; dx++ {
				c := f.At(nx+dx, f.H-Pix+dy)
				if c != (Color{}) && c != paletteFor(g, testPal).Sky {
					npcHit = true
				}
			}
		}
		if !npcHit {
			t.Errorf("%s: no NPC pixels above the floor at RetainerAt", tc.name)
		}
	}
}

// TestRetainerSceneWrap verifies a narrow viewport wraps the toad
// message to several lines instead of overflowing.
func TestRetainerSceneWrap(t *testing.T) {
	g := engine.NewGame([]*engine.Level{underflowLevel(t)}, 14, 10)
	g.Level.Retainer = 1
	g.Level.RetainerAt = engine.Vec{X: 10, Y: 12}
	g.State = engine.StateRetainer
	f := worldFrame(nil, g, testPal)
	if len(wrapTextPx(retainerTextToad, f.W-8, 1)) < 2 {
		t.Errorf("toad message must wrap on a %dpx frame", f.W)
	}
}

// TestRetainerWindowSteady pins the window placement against the
// prompt blink: the reserved-but-invisible blink-off phase must not
// move the room's window (it once bounced 9px every 40 ticks).
func TestRetainerWindowSteady(t *testing.T) {
	rowAt := func(tick int) int {
		g := newGame(t)
		g.Level.Retainer = 2
		g.Level.RetainerAt = engine.Vec{X: 14, Y: 12}
		g.State = engine.StateRetainer
		g.Tick = tick
		f := worldFrame(nil, g, testPal)
		p := paletteFor(g, testPal)
		for y := range f.H {
			for x := f.W/2 - 3; x < f.W/2+3; x++ {
				if f.At(x, y) == p.GroundDark {
					return y
				}
			}
		}
		return -1
	}
	on, off := rowAt(20), rowAt(30) // blink on-phase, off-phase
	if on < 0 || off < 0 {
		t.Fatalf("window not found (on=%d off=%d)", on, off)
	}
	if on != off {
		t.Errorf("window moved with the blink: on-phase y=%d, off-phase y=%d", on, off)
	}
}

// underflowLevel builds a narrow level for wrap testing.
func underflowLevel(t *testing.T) *engine.Level {
	t.Helper()
	b := engine.NewBuilder(30, engine.LevelHeight)
	b.Ground(0, 29)
	b.Set(2, 12, 'M')
	l, err := engine.ParseLevel("narrow", b.Rows())
	if err != nil {
		t.Fatalf("ParseLevel: %v", err)
	}
	return l
}
