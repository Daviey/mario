package render

import (
	"fmt"
	"testing"

	"mario/engine"
)

// dumpFrame prints a frame as letters (one per palette swatch) for visual
// inspection with `go test -run TestVisualDump -v`.
func dumpFrame(f *Frame) string {
	legend := map[Color]byte{
		testPal.Sky: '.', testPal.GroundLight: 'o', testPal.GroundMid: 'O',
		testPal.GroundDark: 'x', testPal.BrickLight: 'B', testPal.BrickDark: 'b',
		testPal.QuestionBG: 'Q', testPal.QuestionDim: 'd', testPal.QuestionHi: 'h', testPal.QuestionFG: 'q',
		testPal.QuestionMark: '?', testPal.UsedBG: 'U', testPal.PipeLight: 'E',
		testPal.PipeMid: 'G', testPal.PipeDark: 'g', testPal.Pole: 'P',
		testPal.FlagRed: 'F', testPal.Coin: 'Y', testPal.GoldLight: 'L',
		testPal.Player: 'R', testPal.Skin: 'S', testPal.Overall: 'V',
		testPal.Dark: 'D', testPal.Goomba: 'n', testPal.Green: 'G',
		testPal.GreenLight: 'E', testPal.GreenDark: 'g', testPal.KoopaSkin: 'K',
		testPal.Cloud: 'W', testPal.White: 'W', testPal.Door: 'D',
		testPal.Window: 'D', testPal.OverlayBG: '#', testPal.OverlayFG: '!',
	}
	var sb string
	for y := 0; y < f.H; y++ {
		row := make([]byte, f.W)
		for x := 0; x < f.W; x++ {
			c := f.At(x, y)
			if ch, ok := legend[c]; ok {
				row[x] = ch
			} else {
				row[x] = '~'
			}
		}
		sb += fmt.Sprintf("%2d %s\n", y, string(row))
	}
	return sb
}

func TestVisualDump(t *testing.T) {
	if testing.Short() {
		t.Skip("visual dump")
	}
	rc := runeColors(testPal)
	f := NewFrame(120, 54, testPal.Sky)

	// Sky dressing.
	f.DrawSprite(sprCloud, rc, 8, 2, false, 1)
	drawFlagTop(f, testPal, 26, 4)
	for ty := 5; ty <= 6; ty++ {
		drawFlagPole(f, testPal, 26, ty*Pix)
	}

	// Tile parade at rows 14-19, standing on a ground strip at 44.
	drawQuestion(f, testPal, 2, 14, true)
	drawUsed(f, testPal, 9, 14)
	drawBrick(f, testPal, 16, 14, 0)
	drawBrick(f, testPal, 23, 14, 1)
	drawPipe(f, testPal, 30, 14, 0, true)
	drawPipe(f, testPal, 36, 14, 1, true)
	drawPipe(f, testPal, 30, 20, 0, false)
	drawPipe(f, testPal, 36, 20, 1, false)
	drawCastle(f, testPal, 56, 20)
	f.DrawSprite(sprHill, rc, 40, 44-sprH(sprHill), false, 1)
	f.DrawSprite(sprBush, rc, 46, 44-sprH(sprBush), false, 1)
	for i := range 20 { // ground strip under everything
		drawGround(f, testPal, 2+i*Pix, 44, i, 13, true)
	}

	// Sprite parade standing on the ground line.
	x := 2
	for _, a := range []struct {
		art  []string
		flip bool
	}{
		{sprMarioSmall, false}, {sprMarioSuper, false}, {sprGoomba, false},
		{sprKoopa, false}, {sprShell, false}, {sprMushroom, false},
		{sprCoin, false}, {sprCoinEdge, false}, {sprMarioSuper, true}, {sprKoopa, true},
		{sprCoinPop, false}, {sprSparkle, false},
	} {
		f.DrawSprite(a.art, rc, x, 44-sprH(a.art), a.flip, 1)
		x += sprW(a.art) + 3
	}
	t.Log("\n" + dumpFrame(f))
}

func TestVisualGameplayDump(t *testing.T) {
	if testing.Short() {
		t.Skip("visual dump")
	}
	g := engine.NewGame(engine.DefaultLevels(), 20, engine.LevelHeight)
	g.State = engine.StatePlaying
	for i := 0; i < 320; i++ {
		g.Update(engine.Input{Right: true, Run: true, Up: i%80 < 18})
	}
	s := Render(g, testPal)
	// Rebuild the frame from the screen cells for dumping (fg=upper, bg=lower).
	f := NewFrame(s.W, (s.H-2)*2, testPal.Sky)
	for cy := 0; cy < f.H/2; cy++ {
		for x := 0; x < f.W; x++ {
			c := s.At(x, 1+cy)
			f.Set(x, cy*2, c.Fg)
			f.Set(x, cy*2+1, c.Bg)
		}
	}
	t.Log("\n" + dumpFrame(f))
}
