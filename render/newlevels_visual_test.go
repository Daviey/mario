package render

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Daviey/mario/engine"
)

// dumpFramePal prints a frame as letters using the palette actually in
// effect (theme-aware), for visual inspection with -v.
func dumpFramePal(f *Frame, p *Palette) string {
	legend := map[Color]byte{
		p.Sky: '.', p.GroundLight: 'o', p.GroundMid: 'O',
		p.GroundDark: 'x', p.BrickLight: 'B', p.BrickDark: 'b',
		p.QuestionBG: 'Q', p.QuestionDim: 'd', p.QuestionHi: 'h',
		p.QuestionMark: '?', p.UsedBG: 'U', p.PipeLight: 'E',
		p.PipeMid: 'G', p.PipeDark: 'g', p.Pole: 'P',
		p.FlagRed: 'F', p.Coin: 'Y', p.GoldLight: 'L',
		p.Player: 'R', p.Skin: 'S', p.Overall: 'V',
		p.Dark: 'D', p.Goomba: 'n', p.Green: 'G',
		p.GreenLight: 'E', p.GreenDark: 'g', p.KoopaSkin: 'K',
		p.Cloud: 'W', p.White: 'W', p.Door: 'D', p.Window: 'D',
	}
	var sb strings.Builder
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
		sb.WriteString(fmt.Sprintf("%2d %s\n", y, string(row)))
	}
	return sb.String()
}

// TestVisualNewLevelsDump renders signature scenes from the three world-2
// levels for eyeball verification (`go test -run TestVisualNewLevelsDump -v`).
func TestVisualNewLevelsDump(t *testing.T) {
	if testing.Short() {
		t.Skip("visual dump")
	}
	levels := engine.DefaultLevels()
	scenes := []struct {
		idx  int
		camX float64
		tick int
		note string
	}{
		{4, 20, 60, "2-2 pipes, plant, paratroopa"},
		{5, 28, 60, "2-3 platforms, coin runs, para on high route"},
		{6, 16, 60, "2-4 entry: pillar bar, goombas"},
		{6, 60, 60, "2-4 lava pool (running jump)"},
		{6, 104, 60, "2-4 second lava pool and bar"},
	}
	for _, sc := range scenes {
		g := engine.NewGame([]*engine.Level{levels[sc.idx]}, 40, 12)
		g.State = engine.StatePlaying
		g.CameraX = sc.camX
		g.Tick = sc.tick
		// Park the player somewhere harmless on screen.
		g.Player.Pos = engine.Vec{X: sc.camX + 2, Y: 11}
		f := worldFrame(g, testPal)
		pal := paletteFor(g, testPal)
		t.Logf("=== %s (camX=%v tick=%d) ===\n%s", sc.note, sc.camX, sc.tick, dumpFramePal(f, pal))
	}
}
