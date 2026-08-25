package render

// Pixel-grid geometry. Every world tile is Pix×Pix square pixels; a screen
// cell carries two vertically stacked pixels via the half block '▀'
// (fg = upper pixel, bg = lower pixel), which is the finest full-color
// grid a terminal can express.
const (
	Pix       = 4 // pixels per tile, both axes
	ViewTiles = 9 // visible world height in tiles
)

// Frame is a W×H grid of square pixels.
type Frame struct {
	W, H int
	px   []Color
}

// NewFrame returns a frame filled with one color.
func NewFrame(w, h int, bg Color) *Frame {
	f := &Frame{W: w, H: h, px: make([]Color, w*h)}
	for i := range f.px {
		f.px[i] = bg
	}
	return f
}

// Set writes one pixel, clipping out of bounds.
func (f *Frame) Set(x, y int, c Color) {
	if x < 0 || x >= f.W || y < 0 || y >= f.H {
		return
	}
	f.px[y*f.W+x] = c
}

// At reads one pixel (zero Color out of bounds).
func (f *Frame) At(x, y int) Color {
	if x < 0 || x >= f.W || y < 0 || y >= f.H {
		return Color{}
	}
	return f.px[y*f.W+x]
}

// Fill paints a rectangle.
func (f *Frame) Fill(x, y, w, h int, c Color) {
	for yy := y; yy < y+h; yy++ {
		for xx := x; xx < x+w; xx++ {
			f.Set(xx, yy, c)
		}
	}
}

// DrawSprite stamps rune art at (x, y) with the top-left anchor. '.' pixels
// are transparent. flip mirrors horizontally (for left-facing entities);
// scale multiplies the pixel size (for title art).
func (f *Frame) DrawSprite(art []string, cols map[rune]Color, x, y int, flip bool, scale int) {
	for ry, row := range art {
		for rx, r := range row {
			if r == '.' {
				continue
			}
			c, ok := cols[r]
			if !ok {
				continue
			}
			px := rx
			if flip {
				px = len(row) - 1 - rx
			}
			if scale == 1 {
				f.Set(x+px, y+ry, c)
			} else {
				f.Fill(x+px*scale, y+ry*scale, scale, scale, c)
			}
		}
	}
}

// sprW is the sprite width in pixels (art columns).
func sprW(art []string) int {
	if len(art) == 0 {
		return 0
	}
	return len(art[0])
}

// sprH is the sprite height in pixels (art rows).
func sprH(art []string) int { return len(art) }

//
// Sprite art. All sprites face RIGHT; the renderer mirrors for left.
// Runes index the palette via runeColors (see render.go).
//

var sprMarioSmall = []string{ // 5×5 on a 1×1-tile hitbox
	".RRR.",
	"RRRRR",
	"SSDSS",
	"RBBBR",
	".D.D.",
}

var sprMarioSuper = []string{ // 5×8 on a 1×2-tile hitbox
	".RRR.",
	"RRRRR",
	"SSDSS",
	"SDDDS",
	"RRRRR",
	"RBBBR",
	".BBB.",
	"DD.DD",
}

var sprGoomba = []string{ // 5×4 on a 0.9×0.9 hitbox
	".nnn.",
	"nnnnn",
	"WD.DW",
	"DD.DD",
}

var sprKoopa = []string{ // 5×5 on a 0.9×1.3 hitbox
	"..DKK",
	".KKKK",
	".GGGG",
	"GGgGG",
	".K.K.",
}

var sprShell = []string{ // 5×3
	".GGG.",
	"GGgGG",
	"KKKKK",
}

var sprMushroom = []string{ // 5×4
	".RRR.",
	"RRWRR",
	".SDS.",
	".SSS.",
}

var sprCoin = []string{ // 3×4, full face
	".Y.",
	"YLY",
	"YLY",
	".Y.",
}

var sprCoinEdge = []string{ // 3×4, edge-on (spin frame)
	".L.",
	".Y.",
	".Y.",
	".L.",
}

var sprCoinPop = []string{ // 2×2 particle
	"YL",
	"LY",
}

var sprSparkle = []string{ // 3×3 particle
	".W.",
	"WWW",
	".W.",
}

var sprCloud = []string{ // 12×4
	"....WWWW....",
	"..WWWWWWWW..",
	".WWWWWWWWWW.",
	"..WWWWWWWW..",
}

var sprHill = []string{ // 8×3
	"...EE...",
	".EGGGGE.",
	"GGGGGGGG",
}

var sprBush = []string{ // 6×2
	".EGGE.",
	"GGGGGG",
}

//
// Tile pixel patterns. Each paints one tile (Pix×Pix) at pixel (x, y);
// (tx, ty) are the tile's world coordinates, used for variation.
//

func drawGround(f *Frame, p *Palette, x, y, tx, ty int, openTop bool) {
	f.Fill(x, y, Pix, Pix, p.GroundMid)
	if openTop {
		f.Fill(x, y, Pix, 1, p.GroundLight) // sunlit top edge
	}
	f.Fill(x+Pix-1, y+1, 1, Pix-1, p.GroundDark)
	f.Set(x+((tx+ty)%2)+1, y+2, p.GroundDark) // soil speckle
	f.Set(x+((tx*3+ty)%2), y+3, p.GroundDark)
}

func drawBrick(f *Frame, p *Palette, x, y, tx int) {
	f.Fill(x, y, Pix, Pix, p.BrickLight)
	f.Fill(x, y+Pix-1, Pix, 1, p.BrickDark) // mortar bed
	j := 1
	if tx%2 == 1 {
		j = 3
	}
	f.Fill(x+j, y, 1, Pix, p.BrickDark) // staggered head joint
	f.Set(x, y, p.QuestionHi)           // top-left highlight
}

func drawQuestion(f *Frame, p *Palette, x, y int, bright bool) {
	f.Fill(x, y, Pix, Pix, p.QuestionBG)
	f.Fill(x, y, Pix, 1, p.QuestionHi) // beveled top/left
	f.Fill(x, y, 1, Pix, p.QuestionHi)
	f.Fill(x+Pix-1, y, 1, Pix, p.BrickDark) // shaded bottom/right
	f.Fill(x, y+Pix-1, Pix, 1, p.BrickDark)
	q := p.QuestionFG
	if bright {
		q = p.QuestionMark
	}
	// '?': top bar, right stem, dot on the base.
	f.Set(x+1, y+1, q)
	f.Set(x+2, y+1, q)
	f.Set(x+2, y+2, q)
	f.Set(x+1, y+3, q)
}

func drawUsed(f *Frame, p *Palette, x, y int) {
	f.Fill(x, y, Pix, Pix, p.UsedBG)
	f.Fill(x, y, Pix, 1, p.Dark)
	f.Fill(x, y+Pix-1, Pix, 1, p.Dark)
	f.Set(x, y, p.Dark)
	f.Set(x+Pix-1, y, p.Dark)
	f.Set(x, y+Pix-1, p.Dark)
	f.Set(x+Pix-1, y+Pix-1, p.Dark)
}

// drawPipe paints one 4px column of a pipe. col is the column within the
// two-tile pipe (0 left, 1 right). Lip rows (the pipe's top) draw an
// 8px-wide rim from the left column only.
func drawPipe(f *Frame, p *Palette, x, y, col int, lip bool) {
	if lip {
		if col != 0 {
			return // the left column paints the whole rim
		}
		f.Fill(x, y, 2*Pix, 1, p.GreenLight)
		f.Fill(x+4, y, 4, 1, p.Green)
		f.Fill(x, y+1, 2*Pix, 1, p.GreenDark) // rim shadow
		f.Set(x, y, p.Green)
		f.Set(x+7, y, p.GreenDark)
		return
	}
	switch col {
	case 0:
		f.Fill(x, y, 2, Pix, p.GreenLight) // lit left edge
		f.Fill(x+2, y, 2, Pix, p.Green)
	case 1:
		f.Fill(x, y, 3, Pix, p.Green)
		f.Fill(x+3, y, 1, Pix, p.GreenDark) // shaded edge; col 4 stays sky
	}
}

// drawFlagPole paints one pole tile; drawFlagTop paints the finial and
// pennant of the goal.
func drawFlagPole(f *Frame, p *Palette, x, y int) {
	f.Fill(x+1, y, 1, Pix, p.Pole)
}

func drawFlagTop(f *Frame, p *Palette, x, y int) {
	f.Set(x+1, y, p.GreenLight)        // ball
	f.Fill(x+1, y+1, 1, Pix-1, p.Pole) // pole
	f.Set(x, y+1, p.FlagRed)           // pennant, pointing down-left
	f.Set(x-1, y+2, p.FlagRed)
	f.Set(x, y+2, p.FlagRed)
	f.Set(x-2, y+3, p.FlagRed)
	f.Set(x-1, y+3, p.FlagRed)
	f.Set(x, y+3, p.FlagRed)
}

// drawCastle paints the goal castle: a 20×16 px keep with tower,
// crenellations, windows and an arched door.
func drawCastle(f *Frame, p *Palette, x0, y0 int) {
	wall := func(x, y, w, h int) {
		for ty := y; ty < y+h; ty += Pix {
			for tx := x; tx < x+w; tx += Pix {
				drawBrick(f, p, tx, ty, (tx-x)/Pix)
			}
		}
	}
	// Lower crenellations along the keep roof.
	for i := 0; i < 5; i++ {
		wall(x0+i*4, y0+6, 2, 2)
	}
	wall(x0, y0+8, 20, 8) // keep
	// Tower crenellations and tower.
	wall(x0+6, y0, 2, 2)
	wall(x0+10, y0, 2, 2)
	wall(x0+6, y0+2, 8, 6)
	// Arched door and windows.
	f.Fill(x0+9, y0+9, 2, 1, p.Door)
	f.Fill(x0+8, y0+10, 4, 6, p.Door)
	f.Fill(x0+3, y0+9, 2, 2, p.Window)
	f.Fill(x0+15, y0+9, 2, 2, p.Window)
}
