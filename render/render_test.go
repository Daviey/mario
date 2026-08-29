package render

import (
	"regexp"
	"strings"
	"testing"

	"github.com/Daviey/mario/engine"
)

func TestScreenDimensions(t *testing.T) {
	g := newGame(t)
	s := Render(g, testPal)
	wantW := g.ViewW * Pix
	wantH := 2 + g.ViewH*Pix/2
	if s.W != wantW || s.H != wantH {
		t.Fatalf("dims = %dx%d, want %dx%d", s.W, s.H, wantW, wantH)
	}
}

func TestHUD(t *testing.T) {
	g := newGame(t)
	g.Score, g.CoinCount, g.Time, g.Lives = 1234, 5, 200, 2
	hud := rowText(Render(g, testPal), 0)
	for _, want := range []string{"SCORE", "001234", "COINS", "x05", "WORLD", "t", "TIME", "200", "LIVES", "x2"} {
		if !strings.Contains(hud, want) {
			t.Errorf("HUD %q missing %q", hud, want)
		}
	}
	if got := Render(g, testPal).At(0, 0).Bg; got != testPal.HUDBG {
		t.Errorf("HUD background = %+v, want banner blue", got)
	}
}

func TestStatusLine(t *testing.T) {
	s := Render(newGame(t), testPal)
	if !strings.Contains(rowText(s, s.H-1), "q quit") {
		t.Error("controls line missing quit hint")
	}
}

func TestSkyBackground(t *testing.T) {
	g := newGame(t)
	s := Render(g, testPal)
	found := false
	for x := 0; x < s.W; x++ {
		if worldPx(s, x, 4) == testPal.Sky {
			found = true
			break
		}
	}
	if !found {
		t.Error("no sky pixel found near the top of the view")
	}
}

func TestGroundPixels(t *testing.T) {
	g := newGame(t)
	s := Render(g, testPal)
	// Camera clamped to Y=6: ground top (world 13) at pixel row 42.
	if got := worldPx(s, 10, 42); got != testPal.GroundLight {
		t.Errorf("ground top pixel = %+v, want sunlit", got)
	}
	if got := worldPx(s, 10, 44); got != testPal.GroundMid {
		t.Errorf("soil pixel = %+v", got)
	}
	// Deep ground row (world 14, pixel row 48) has no sunlit stripe.
	if got := worldPx(s, 10, 48); got == testPal.GroundLight {
		t.Error("deep ground row painted sunlit")
	}
}

func TestQuestionAndUsedPixels(t *testing.T) {
	g := newGame(t)
	s := Render(g, testPal) // Tick 0: bright body phase
	// Question at world (6,9): pixel cols 36..41, rows 18..23.
	if got := worldPx(s, 37, 20); got != testPal.QuestionBG {
		t.Errorf("question block body = %+v", got)
	}
	if got := worldPx(s, 38, 18); got != testPal.QuestionMark {
		t.Errorf("'?' pixel = %+v, want always-bright mark", got)
	}
	if got := worldPx(s, 36, 19); got != testPal.QuestionMark {
		t.Errorf("'?' curl pixel = %+v, want always-bright mark", got)
	}
	// Dim phase: the body deepens but the mark stays bright.
	g.Tick = 24
	s = Render(g, testPal)
	if got := worldPx(s, 37, 20); got != testPal.QuestionDim {
		t.Errorf("dim body = %+v", got)
	}
	if got := worldPx(s, 38, 18); got != testPal.QuestionMark {
		t.Errorf("'?' pixel in dim phase = %+v, mark must never dim", got)
	}
	// A used block is solid dull brown.
	g.Level.Set(8, 9, engine.Used)
	s = Render(g, testPal)
	if got := worldPx(s, 49, 20); got != testPal.UsedBG {
		t.Errorf("used block = %+v", got)
	}
}

func TestPipeShading(t *testing.T) {
	g := newGame(t)
	s := Render(g, testPal)
	// Pipe at world x=10, lip at row 11 -> rim pixel rows 30-31.
	if got := worldPx(s, 63, 30); got != testPal.GreenLight {
		t.Errorf("pipe rim = %+v, want lit green", got)
	}
	if got := worldPx(s, 70, 31); got != testPal.GreenDark {
		t.Errorf("pipe rim shadow = %+v", got)
	}
	// The shaft starts inside the lip tile: no sky band under the rim.
	if got := worldPx(s, 61, 32); got != testPal.GreenLight {
		t.Errorf("shaft under rim = %+v, want lit green (no gap)", got)
	}
	// Body continues the shaft, inset 1px per side so the rim overhangs.
	if got := worldPx(s, 60, 36); got != testPal.Sky {
		t.Errorf("pipe inset column = %+v, want sky", got)
	}
	if got := worldPx(s, 61, 36); got != testPal.GreenLight {
		t.Errorf("pipe body lit edge = %+v", got)
	}
	if got := worldPx(s, 69, 36); got != testPal.GreenDark {
		t.Errorf("pipe body shaded edge = %+v", got)
	}
	if got := worldPx(s, 71, 36); got != testPal.Sky {
		t.Errorf("pipe inset column = %+v, want sky", got)
	}
}

func TestCloudsNeverBehindBlocks(t *testing.T) {
	// Bricks every 3rd column on row 9: every cloud anchored there
	// overlaps one (clouds span 3 tiles), so no cloud pixel may appear
	// anywhere in that row band.
	b := engine.NewBuilder(80, engine.LevelHeight)
	b.Ground(0, 79)
	for x := 0; x < 80; x += 3 {
		b.Set(x, 9, 'B')
	}
	b.Set(2, 12, 'M')
	b.Flag(70)
	l, err := engine.ParseLevel("t", b.Rows())
	if err != nil {
		t.Fatalf("ParseLevel: %v", err)
	}
	g := engine.NewGame([]*engine.Level{l}, 20, 9)
	g.State = engine.StatePlaying
	s := Render(g, testPal)
	for x := 0; x < s.W; x++ {
		for y := 18; y <= 23; y++ {
			if got := worldPx(s, x, y); got == testPal.Cloud {
				t.Fatalf("cloud pixel at (%d,%d) behind the brick row", x, y)
			}
		}
	}
}

func TestPlayerPixels(t *testing.T) {
	g := newGame(t)
	s := Render(g, testPal)
	// Small player at world (2,12): center px 14, sprite rows 35..41.
	if got := worldPx(s, 13, 35); got != testPal.Player {
		t.Errorf("player cap pixel = %+v, want red", got)
	}
	if got := worldPx(s, 14, 39); got != testPal.Overall {
		t.Errorf("player overalls pixel = %+v, want blue", got)
	}
	if worldPx(s, 13, 34) == testPal.Player {
		t.Error("small player sprite too tall")
	}

	// Super player spans two tiles (rows 29..41).
	g.Player.Power = engine.PowerSuper
	g.Player.W, g.Player.H = engine.SuperW, engine.SuperH
	g.Player.Pos.Y -= engine.SuperH - engine.SmallH
	s = Render(g, testPal)
	if got := worldPx(s, 13, 29); got != testPal.Player {
		t.Errorf("super cap pixel = %+v", got)
	}
	if worldPx(s, 13, 28) == testPal.Player {
		t.Error("super player sprite too tall")
	}
}

func TestEnemyPixels(t *testing.T) {
	g := newGame(t)
	s := Render(g, testPal)
	// Goomba at world (14,12): center px 87, rows 36..41.
	if got := worldPx(s, 87, 37); got != testPal.Goomba {
		t.Errorf("goomba body pixel = %+v", got)
	}
	// Koopa at world (17,12): head skin at row 34.
	if got := worldPx(s, 105, 34); got != testPal.KoopaSkin {
		t.Errorf("koopa head pixel = %+v", got)
	}
	// Shell state draws a green dome.
	g.Enemies[1].State = engine.EnemyShell
	g.Enemies[1].H = engine.GoombaH
	g.Enemies[1].Pos.Y = 13 - engine.GoombaH
	s = Render(g, testPal)
	if got := worldPx(s, 104, 38); got != testPal.Green {
		t.Errorf("shell dome = %+v", got)
	}
}

func TestCoinPixels(t *testing.T) {
	g := newGame(t)
	g.Tick = 0 // full-face spin frame
	s := Render(g, testPal)
	// Coin at world (5.2,8.2): pixel cols 31..34, rows 12..17.
	if got := worldPx(s, 31, 13); got != testPal.Coin {
		t.Errorf("coin pixel = %+v, want gold", got)
	}
	g.Tick = 8 // edge-on frame
	s = Render(g, testPal)
	if got := worldPx(s, 33, 12); got != testPal.GoldLight {
		t.Errorf("coin edge pixel = %+v, want highlight", got)
	}
}

func TestMushroomPixels(t *testing.T) {
	g := newGame(t)
	g.Mushrooms = append(g.Mushrooms, &engine.Mushroom{Pos: engine.Vec{X: 5, Y: 12.1}})
	s := Render(g, testPal)
	if got := worldPx(s, 32, 37); got != testPal.Player {
		t.Errorf("mushroom cap = %+v, want red", got)
	}
}

func TestParticlePixels(t *testing.T) {
	g := newGame(t)
	g.Particles = append(g.Particles,
		&engine.Particle{Pos: engine.Vec{X: 5, Y: 6.5}, Life: 10, Kind: engine.ParticleCoin},
		&engine.Particle{Pos: engine.Vec{X: 7, Y: 6.5}, Life: 10, Kind: engine.ParticleDebris},
		&engine.Particle{Pos: engine.Vec{X: 9, Y: 6.5}, Life: 10, Kind: engine.ParticleSparkle},
	)
	s := Render(g, testPal)
	if got := worldPx(s, 31, 3); got != testPal.Coin {
		t.Errorf("coin pop pixel = %+v", got)
	}
	if got := worldPx(s, 42, 3); got != testPal.BrickDark {
		t.Errorf("debris pixel = %+v", got)
	}
	if got := worldPx(s, 55, 2); got != testPal.White {
		t.Errorf("sparkle pixel = %+v", got)
	}
}

func TestInvincibleFlicker(t *testing.T) {
	g := newGame(t)
	g.Player.Invincible = 100
	g.Tick = 0 // (0/3)%2 == 0 -> hidden
	if got := worldPx(Render(g, testPal), 13, 35); got == testPal.Player {
		t.Error("flicker off-tick shows player")
	}
	g.Tick = 3
	if got := worldPx(Render(g, testPal), 13, 35); got != testPal.Player {
		t.Error("flicker on-tick hides player")
	}
}

func TestBumpLiftsBlock(t *testing.T) {
	g := newGame(t)
	g.Player.Pos = engine.Vec{X: 6.15, Y: 13 - engine.SmallH}
	for i := 0; i < 40; i++ {
		g.Update(engine.Input{Up: true})
		if g.BumpActive(6, 9) {
			s := Render(g, testPal)
			// The block is lifted half a tile: row 15 is block, not sky.
			if got := worldPx(s, 38, 15); got == testPal.Sky {
				t.Error("bumping block not lifted")
			}
			return
		}
	}
	t.Fatal("bump never happened")
}

func TestCameraScrollAndVerticalFollow(t *testing.T) {
	g := newGame(t)
	g.Player.Pos.X = 40
	g.Update(engine.Input{})
	if g.CameraX == 0 {
		t.Fatal("camera did not move")
	}
	s := Render(g, testPal)
	// Ground is everywhere: it must still fill the bottom of the view.
	if got := worldPx(s, s.W-2, 44); got != testPal.GroundMid {
		t.Errorf("ground missing after scroll: %+v", got)
	}
	// Vertical follow: jumping raises the camera off the bottom clamp.
	g2 := newGame(t)
	for i := 0; i < 30; i++ {
		g2.Update(engine.Input{Up: true})
	}
	if CameraY(g2) >= 6 {
		t.Errorf("camera Y = %f, want it to have risen while jumping", CameraY(g2))
	}
	g3 := newGame(t)
	g3.Update(engine.Input{})
	if got := CameraY(g3); got != 6 {
		t.Errorf("camera Y grounded = %f, want 6 (bottom clamp)", got)
	}
}

func TestDecorationsDrawn(t *testing.T) {
	g := newGame(t)
	s := Render(g, testPal)
	white := false
	for y := 0; y < 18; y++ {
		for x := 0; x < s.W; x++ {
			if worldPx(s, x, y) == testPal.Cloud {
				white = true
			}
		}
	}
	if !white {
		for tx := 0; tx < g.ViewW; tx++ {
			if row, _, ok := CloudAt(tx); ok && row >= 6 && row <= 10 {
				t.Fatalf("cloud at tx=%d row=%d should be visible", tx, row)
			}
		}
		t.Log("no cloud happened to be in this window; helpers verified elsewhere")
	}
}

func TestCastlePixels(t *testing.T) {
	g := newGame(t) // flag at 70 -> castle at world x 73..77, rows 9..12
	g.Player.Pos.X = 68
	g.Update(engine.Input{}) // camera scrolls right (clamped at 60)
	if g.CameraX < 55 {
		t.Fatalf("camera did not reach the castle: %f", g.CameraX)
	}
	s := Render(g, testPal)

	// Keep brick and door land inside the pixel view.
	if got := worldPx(s, 86, 30); got != testPal.BrickLight {
		t.Errorf("castle brick = %+v", got)
	}
	if got := worldPx(s, 92, 36); got != testPal.Door {
		t.Errorf("castle door = %+v", got)
	}
}

func TestOverlayPixelText(t *testing.T) {
	g := newGame(t)
	g.Paused = true
	s := Render(g, testPal)
	bg := false
	for y := 20; y < 34; y++ {
		for x := 0; x < s.W; x++ {
			if worldPx(s, x, y) == testPal.OverlayBG {
				bg = true
			}
		}
	}
	if !bg {
		t.Error("paused banner background missing")
	}

	g.Paused = false
	g.State = engine.StateTitle
	s = Render(g, testPal)
	// Title logo at pixel row 4: the 'M' glyph's solid rows are FlagRed.
	if got := worldPx(s, 41, 5); got != testPal.FlagRed {
		t.Errorf("title MARIO art missing: %+v", got)
	}
	// The title band (rows 2..12) must contain only title pixels: no
	// sprite colors bleeding over the text.
	for x := 0; x < s.W; x++ {
		c := worldPx(s, x, 8)
		if c == testPal.Skin || c == testPal.Overall || c == testPal.Goomba {
			t.Fatalf("sprite pixel at (%d,8) covers the title", x)
		}
	}
	// Mario and goomba stand on the ground line (see fit_test for the
	// precise placement contract); here just assert they exist on title.
	mario, goomba := false, false
	for y := 0; y < s.H*2; y++ {
		for x := 0; x < s.W; x++ {
			switch worldPx(s, x, y) {
			case testPal.Player:
				mario = true
			case testPal.Goomba:
				goomba = true
			}
		}
	}
	if !mario || !goomba {
		t.Errorf("title sprites missing: mario=%v goomba=%v", mario, goomba)
	}
}

func TestTrueColorVsBasicSequences(t *testing.T) {
	g := newGame(t)
	tc := Render(g, NewPalette(true)).String()
	if !strings.Contains(tc, "38;2;") || !strings.Contains(tc, "48;2;") {
		t.Error("truecolor frame missing 24-bit sequences")
	}
	basic := Render(g, NewPalette(false)).String()
	if strings.Contains(basic, "38;2;") {
		t.Error("basic frame must not emit 24-bit sequences")
	}
	// Differential SGR emits bare ANSI-16 codes ("36") without the old
	// "0;" reset prefix; match them in any position.
	if !regexp.MustCompile(`\x1b\[[0-9;]*(3[0-7]|9[0-7])m`).MatchString(basic) {
		t.Error("basic frame missing ANSI-16 codes")
	}
}

func TestANSIFrame(t *testing.T) {
	s := Render(newGame(t), testPal)
	f := s.String()
	if !strings.HasPrefix(f, "\x1b[H") {
		t.Error("frame must start with cursor home")
	}
	if !strings.HasSuffix(f, "\x1b[0m") {
		t.Error("frame must end with reset")
	}
	if n := strings.Count(f, "\r\n"); n != s.H-1 {
		t.Errorf("row breaks = %d, want %d", n, s.H-1)
	}
	// World rows pack two pixels per cell: differing halves ride the
	// half block, solid pairs are a space over the colour.
	blocks, solid := 0, 0
	for y := 1; y < s.H-1; y++ {
		for x := range s.W {
			c := s.At(x, y)
			switch c.Ch {
			case '▀':
				blocks++
				if c.Fg == c.Bg {
					t.Errorf("cell (%d,%d): solid pair encoded as half block", x, y)
				}
			case ' ':
				solid++
				if c.Fg != c.Bg {
					t.Errorf("cell (%d,%d): split pair encoded as space", x, y)
				}
			default:
				t.Errorf("cell (%d,%d): unexpected world glyph %q", x, y, c.Ch)
			}
		}
	}
	if blocks == 0 || solid == 0 {
		t.Error("world rows must use both half blocks and solid spaces")
	}
}

func TestScreenHelpers(t *testing.T) {
	s := NewScreen(10, 3)
	s.Set(20, 0, 'X', testPal.White) // clipped, must not panic
	s.Text(0, 0, "hello", testPal.FlagRed)
	if s.At(0, 0).Ch != 'h' || s.At(0, 0).Fg != testPal.FlagRed {
		t.Error("Text/Set failed")
	}
	if s.At(5, 0).Ch != ' ' {
		t.Error("default cell not space")
	}
	s.Center(1, "ab", testPal.White, testPal.HUDBG, true)
	c := s.At(4, 1)
	if c.Ch != 'a' || c.Bg != testPal.HUDBG || !c.Bold {
		t.Errorf("Center wrote %+v", c)
	}
	if rowText(s, 0) != "hello     " {
		t.Errorf("RowString = %q", rowText(s, 0))
	}
	if (Cell{}) != s.At(-1, 0) {
		t.Error("out-of-bounds At must return zero cell")
	}
}

func TestFrameANSIHelper(t *testing.T) {
	f := FrameANSI(newGame(t), testPal)
	if !strings.HasPrefix(f, "\x1b[H") || !strings.Contains(f, "SCORE") {
		t.Error("FrameANSI output malformed")
	}
}

func TestDecorationHelpersDeterministic(t *testing.T) {
	for tx := 0; tx < 200; tx++ {
		r1, w1, ok1 := CloudAt(tx)
		r2, w2, ok2 := CloudAt(tx)
		if ok1 != ok2 || r1 != r2 || w1 != w2 {
			t.Fatalf("CloudAt nondeterministic at %d", tx)
		}
	}
	n := 0
	for tx := 0; tx < 90; tx++ {
		if _, _, ok := CloudAt(tx); ok {
			n++
		}
	}
	if n == 0 || n > 30 {
		t.Errorf("cloud density = %d/90 columns", n)
	}
}

func TestPixelFont(t *testing.T) {
	f := NewFrame(40, 8, testPal.Sky)
	drawTextPx(f, 1, 1, "A1", testPal.White, 1)
	if f.At(1, 1) != testPal.White || f.At(3, 1) != testPal.White {
		t.Error("font glyph 'A' not drawn")
	}
	// '1' starts 4px later; its top row has the stroke at glyph column 1.
	if f.At(6, 1) != testPal.White {
		t.Error("font glyph '1' not drawn")
	}
	if w := textWidthPx("AB", 1); w != 7 {
		t.Errorf("textWidthPx(AB) = %d, want 7", w)
	}
	if w := textWidthPx("AB", 2); w != 14 {
		t.Errorf("textWidthPx(AB,2) = %d, want 14", w)
	}
	// Unknown runes fall back to '?' instead of vanishing.
	f2 := NewFrame(40, 8, testPal.Sky)
	drawTextPx(f2, 0, 0, "&", testPal.White, 1)
	if f2.At(1, 0) != testPal.White {
		t.Error("unknown rune fallback missing")
	}
	// '?' itself is available.
	f3 := NewFrame(40, 8, testPal.Sky)
	drawTextPx(f3, 0, 0, "?", testPal.White, 1)
	if f3.At(1, 0) != testPal.White {
		t.Error("glyph '?' missing")
	}
}

func TestRenderPixels(t *testing.T) {
	g := newGame(t)
	f := RenderPixels(g, testPal)
	wantH := HudBandPx + g.ViewH*Pix + StatusBandPx
	if f.W != g.ViewW*Pix || f.H != wantH {
		t.Fatalf("pixel frame = %dx%d, want %dx%d", f.W, f.H, g.ViewW*Pix, wantH)
	}
	// HUD band across the top, status band across the bottom.
	if got := f.At(4, 0); got != testPal.HUDBG {
		t.Errorf("hud band = %+v", got)
	}
	if got := f.At(4, f.H-1); got != testPal.StatusBG {
		t.Errorf("status band = %+v", got)
	}
	// World sits between the bands: sky pixel just under the HUD.
	if got := f.At(4, HudBandPx+2); got != testPal.Sky {
		t.Errorf("world sky = %+v", got)
	}
	// RGB packing: 3 bytes per pixel, sky encodes to 5c 94 fc.
	rgb := f.RGBBytes()
	if len(rgb) != f.W*f.H*3 {
		t.Fatalf("rgb len = %d", len(rgb))
	}
	i := ((HudBandPx+2)*f.W + 4) * 3 // row HudBandPx+2, col 4
	if rgb[i] != 0x5c || rgb[i+1] != 0x94 || rgb[i+2] != 0xfc {
		t.Errorf("sky rgb = %x %x %x", rgb[i], rgb[i+1], rgb[i+2])
	}
}

func TestTrueColorTerm(t *testing.T) {
	for _, tc := range []struct {
		term string
		want bool
	}{
		{"xterm-ghostty", true},
		{"ghostty", true},
		{"wezterm", true},
		{"xterm-truecolor", true},
		{"xterm-direct", true},
		{"xterm-256color", false},
		{"xterm", false},
		{"tmux-256color", false},
		{"", false},
	} {
		if got := TrueColorTerm(tc.term); got != tc.want {
			t.Errorf("TrueColorTerm(%q) = %v, want %v", tc.term, got, tc.want)
		}
	}
}
