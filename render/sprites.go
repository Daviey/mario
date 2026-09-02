package render

import "math"

// Pixel-grid geometry. Every world tile is Pix×Pix square pixels; a screen
// cell carries two vertically stacked pixels via the half block '▀'
// (fg = upper pixel, bg = lower pixel), which is the finest full-color
// grid a terminal can express.
const (
	Pix = 6 // pixels per tile, both axes
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
	if x < 0 || y < 0 || x >= f.W || y >= f.H {
		return
	}
	f.px[y*f.W+x] = c
}

// At reads one pixel (zero Color out of bounds).
func (f *Frame) At(x, y int) Color {
	if x < 0 || y < 0 || x >= f.W || y >= f.H {
		return Color{}
	}
	return f.px[y*f.W+x]
}

// Fill paints a rectangle, clipped to the frame in one pass — the tile
// and sprite painters stamp thousands of small rects per frame, and the
// old per-pixel Set re-checked the bounds for every pixel.
func (f *Frame) Fill(x, y, w, h int, c Color) {
	x1, y1 := min(x+w, f.W), min(y+h, f.H)
	for ty := max(y, 0); ty < y1; ty++ {
		base := ty * f.W
		for tx := max(x, 0); tx < x1; tx++ {
			f.px[base+tx] = c
		}
	}
}

// DrawSprite stamps rune art at (x, y) with the top-left anchor. '.' pixels
// are transparent. flip mirrors horizontally (for left-facing entities);
// scale multiplies the pixel size (for title art).
func (f *Frame) DrawSprite(art []string, cols map[rune]Color, x, y int, flip bool, scale int) {
	w := sprW(art)
	for row, line := range art {
		for col, r := range line {
			if r == '.' {
				continue
			}
			c, ok := cols[r]
			if !ok {
				continue
			}
			sx := col
			if flip {
				sx = w - 1 - col
			}
			if scale == 1 { // every entity sprite: one clipped Set, no rect math
				f.Set(x+sx, y+row, c)
				continue
			}
			f.Fill(x+sx*scale, y+row*scale, scale, scale, c)
		}
	}
}

// drawSpriteVFlip stamps rune art vertically flipped — the rows stamp in
// reverse order, the columns untouched — at (x, y), top-left anchored.
// The upside-down corpse pose for bosses killed mid-air; flipH mirrors
// horizontally, as in DrawSprite.
func (f *Frame) drawSpriteVFlip(art []string, cols map[rune]Color, x, y int, flipH bool) {
	w := sprW(art)
	h := len(art)
	for row, line := range art {
		for col, r := range line {
			if r == '.' {
				continue
			}
			c, ok := cols[r]
			if !ok {
				continue
			}
			sx := col
			if flipH {
				sx = w - 1 - col
			}
			f.Set(x+sx, y+h-1-row, c)
		}
	}
}

// sprW is the sprite width in pixels (art columns).
func sprW(art []string) int {
	if len(art) == 0 {
		return 0
	}
	return len([]rune(art[0]))
}

// sprH is the sprite height in pixels (art rows).
func sprH(art []string) int { return len(art) }

//
// Sprite art. All sprites face RIGHT; the renderer mirrors for left.
// Runes index the palette via runeColors (see palette.go).
//

var sprMarioSmall = []string{ // 7×7 on a 1×1-tile hitbox
	"..RRR..",
	".RRRRR.",
	".SDSDS.",
	".SSSSS.",
	"RRBBBRR",
	".BBBBB.",
	".DD.DD.",
}

var sprMarioSuper = []string{ // 7×13 on a 1×2-tile hitbox
	"..RRR..",
	".RRRRR.",
	".SDSDS.",
	".SSSSS.",
	".RRRRR.",
	"RRBBBRR",
	"RRBBBRR",
	".BBBBB.",
	".BB.BB.",
	".BB.BB.",
	".BB.BB.",
	".DD.DD.",
	".DD.DD.",
}

var sprMarioSmallStride = []string{ // legs spread wide
	"..RRR..",
	".RRRRR.",
	".SDSDS.",
	".SSSSS.",
	".RBBBR.",
	".BBBBB.",
	"DD...DD",
}

var sprMarioSmallPass = []string{ // feet together under the body
	"..RRR..",
	".RRRRR.",
	".SDSDS.",
	".SSSSS.",
	"RRBBBRR",
	".BBBBB.",
	"..DD...",
}

var sprMarioSmallJump = []string{ // fists up, legs split mid-air
	"..RRR..",
	".RRRRR.",
	"RSDSDSR",
	".SSSSS.",
	".RBBBB.",
	".BB..D.",
	".DD....",
}

var sprMarioSmallSkid = []string{ // leaning back, arm braced forward
	".RRR...",
	"RRRRR..",
	"SDSDS..",
	"SSSSS..",
	"..RBBBR",
	".BBBB..",
	"DD.....",
}

var sprMarioSuperStride = []string{
	"..RRR..",
	".RRRRR.",
	".SDSDS.",
	".SSSSS.",
	".RRRRR.",
	".RBBBR.",
	".RBBBR.",
	".BBBBB.",
	".BB.BB.",
	"BB...BB",
	"BB...BB",
	".D...D.",
	"DD...DD",
}

var sprMarioSuperPass = []string{
	"..RRR..",
	".RRRRR.",
	".SDSDS.",
	".SSSSS.",
	".RRRRR.",
	"RRBBBRR",
	"RRBBBRR",
	".BBBBB.",
	".BBBB..",
	".BBBB..",
	"..BB...",
	"..DD...",
	"..DD...",
}

var sprMarioSuperJump = []string{
	"..RRR..",
	".RRRRR.",
	"RSDSDSR",
	".SSSSS.",
	".RRRRR.",
	"RRBBBRR",
	"RRBBBBR",
	".BBBB..",
	".BB.D..",
	".BB.D..",
	".DD....",
	".DD....",
	".......",
}

var sprMarioSuperSkid = []string{
	".RRR...",
	"RRRRR..",
	"SDSDS..",
	"SSSSS..",
	"RRRRR..",
	"..RBBBR",
	"..RBBBR",
	".BBBB..",
	".BBB...",
	".BBB...",
	".BB....",
	"DD.....",
	"DD.....",
}

// Mario walk cycles, indexed by distance travelled (see WalkFrameLen):
// contact (the stand pose), wide stride, passing. All face RIGHT.
var (
	marioSmallWalk = [][]string{sprMarioSmall, sprMarioSmallStride, sprMarioSmallPass}
	marioSuperWalk = [][]string{sprMarioSuper, sprMarioSuperStride, sprMarioSuperPass}
)

var sprMarioSmallSquash = []string{ // crouched flat: hard-landing bounce
	".......",
	"..RRR..",
	".RRRRR.",
	".SDSDS.",
	"SSSSSSS",
	"RBBBBBR",
	"DDDDDDD",
}

var sprMarioSmallStretch = []string{ // elongated: jump liftoff
	"..RRR..",
	".RRRRR.",
	".SDSDS.",
	".SSSSS.",
	".BBBB..",
	".BB....",
	".DD....",
}

var sprMarioDead = []string{ // dying: flat on his back, arms flung out
	"..RRR..",
	".RRRRR.",
	".SDSDS.",
	"SSSSSSS",
	".RBBBR.",
	".BBBBB.",
	"DD...DD",
}

var sprMarioSuperSquash = []string{
	".......",
	".......",
	"..RRR..",
	".RRRRR.",
	".SDSDS.",
	".SSSSS.",
	".RRRRR.",
	"RRBBBRR",
	"RBBBBBR",
	".BBBBB.",
	"BBBBBBB",
	"DDDDDDD",
	"DDDDDDD",
}

var sprMarioSuperStretch = []string{
	"..RRR..",
	".RRRRR.",
	".SDSDS.",
	".SSSSS.",
	".RRRRR.",
	"RRBBBRR",
	"RRBBBRR",
	".BBBB..",
	".BBB...",
	".BBB...",
	".BB....",
	".DD....",
	".DD....",
}

var sprGoombaWalk = []string{ // waddle: feet splayed
	"..nnn..",
	".nnnnn.",
	"nnnnnnn",
	"nWD.DWn",
	".nnnnn.",
	"DD...DD",
}

var sprKoopaWalk = []string{ // stepping: feet spread
	"..DKKK",
	".KKKKK",
	".GGGGG",
	".GGGGG",
	"GGGGGG",
	"GgGGgG",
	"GgGGgG",
	".K..K.",
	"KK..KK",
}

var sprGoomba = []string{ // 7×6 on a 0.9×0.9 hitbox
	"..nnn..",
	".nnnnn.",
	"nnnnnnn",
	"nWD.DWn",
	".nnnnn.",
	".DD.DD.",
}

var sprKoopa = []string{ // 6×9 on a 0.9×1.3 hitbox
	"..DKKK",
	".KKKKK",
	".GGGGG",
	".GGGGG",
	"GGGGGG",
	"GgGGgG",
	"GgGGgG",
	".KK.K.",
	".KK.K.",
}

var sprPara = []string{ // 8×9: koopa with wings raised (frame A)
	"W..DKKK.",
	"WW.KKKKK",
	"W.GGGGG.",
	".WGGGGG.",
	".GGGGGG.",
	".GgGGgG.",
	".GgGGgG.",
	"..KK.K..",
	"..KK.K..",
}

var sprParaWalk = []string{ // wings flat on the downbeat (frame B)
	"...DKKK.",
	"..KKKKK.",
	"W.GGGGG.",
	"WWGGGGG.",
	".GGGGGG.",
	".GgGGgG.",
	".GgGGgG.",
	"..KK.K..",
	"..KK.K..",
}

var sprShell = []string{ // 6×4
	".GGGG.",
	"GGGGGG",
	"GgGGgG",
	"KKKKKK",
}

var sprMushroom = []string{ // 6×5 on a 0.9×0.9 hitbox
	".RRRR.",
	"RRWWRR",
	"RRWWRR",
	".SDDS.",
	".SSSS.",
}

var sprMushroom1UP = []string{ // 6×5: the green-capped extra life
	".GGGG.",
	"GWWGGG",
	"GGWWGG",
	".SDDS.",
	".SSSS.",
}

var sprStar = []string{ // 5×5: star power, eyes wide
	"..Y..",
	".YYY.",
	"YYYYY",
	"YDYDY",
	".Y.Y.",
}

var sprCoin = []string{ // 4×6, full face
	".YY.",
	"YLLY",
	"YLLY",
	"YLLY",
	"YLLY",
	".YY.",
}

var sprCoinEdge = []string{ // 2×6, edge-on (spin frame)
	".L",
	".Y",
	".Y",
	".Y",
	".Y",
	".L",
}

var sprCoinPop = []string{ // 3×4 particle, mini coin
	".Y.",
	"YLY",
	"YLY",
	".Y.",
}

var sprSparkle = []string{ // 5×5 particle
	"..W..",
	".WWW.",
	"WWWWW",
	".WWW.",
	"..W..",
}

var sprCloud = []string{ // 18×6
	"......CCCCCC......",
	"....CCCCCCCCCC....",
	"..CCCCCCCCCCCCCC..",
	".CCCCCCCCCCCCCCCC.",
	"..CCCCCCCCCCCCCC..",
	"....CCCCCCCCCC....",
}

var sprHill = []string{ // 12×4
	"....EEEE....",
	"..EGGGGGGE..",
	".EGGGGGGGGE.",
	"GGGGGGGGGGGG",
}

var sprFireFlower = []string{ // 5×5 on a 0.9×0.9 hitbox
	".RRR.",
	"RYYYR",
	"RYYYR",
	".RRR.",
	".E.E.",
}

var sprFireball = []string{ // 3×3, spin frame A
	".R.",
	"RYR",
	".R.",
}

var sprFireballSpin = []string{ // 3×3, spin frame B
	"R.R",
	".Y.",
	"R.R",
}

var sprPlant = []string{ // 5×8 on a 0.7×1.35 hitbox; stem sinks into the pipe
	".RRR.",
	"RWWRW",
	"RRRRR",
	"WWWWW",
	".EGE.",
	".EGE.",
	".EGE.",
	".EGE.",
}

var sprBush = []string{ // 9×2
	".EGGGGGE.",
	"EGGGGGGGG",
}

// Bowser, the bridge boss: a 12×12 read of the SMB silhouette on a
// 1.9×1.9 hitbox — domed green shell (G, E highlights, g shade) with a
// top spike on the back-left, twin W horns, tan K hide, pale L belly
// plates over an n seam, W claws, and a dark D brow over the eye. Faces
// RIGHT; the renderer mirrors for his usual left-facing stance. Two
// frames: mouth shut, and the open-jaw fire telegraph — D cavity with W
// teeth top and bottom.
var sprBowser = []string{ // mouth shut: D mouth line under the snout
	"..W.....W.W.",
	".GEGG..KKKKK",
	"GEEGGGKDDKKK",
	"GEGGGGKWDKKK",
	"GGGGggKKKKKD",
	"ggggg.KKDDDD",
	"ggKKLLLLKKK.",
	".gKLLLLLKKWW",
	"..KKnnnnKK..",
	"...KLLLLK...",
	"...KKK.KKK..",
	"...KKWW.KKWW",
}

var sprBowserFire = []string{ // mouth open: W teeth top and bottom of a D cavity
	"..W.....W.W.",
	".GEGG..KKKKK",
	"GEEGGGKDDKKK",
	"GEGGGGKWDKKK",
	"GGGGggKKKKKD",
	"ggggg.KKKWWW",
	"ggKKLLDWDWDD",
	".gKLLLKKKK..",
	"..KKnnnnKK..",
	"...KLLLLK...",
	"...KKK.KKK..",
	"...KKWW.KKWW",
}

var sprBossFire = []string{ // 5×4: R rim, Y core, W streak — spin frame A
	".RRR.",
	"RYYWR",
	"RYWYR",
	".RRR.",
}

var sprBossFireSpin = []string{ // 5×4: streak flipped — spin frame B
	".RRR.",
	"RYWYR",
	"RYYWR",
	".RRR.",
}

var sprAxe = []string{ // 5×6: wide silver blade on a brown wood handle
	"WWWW.",
	"LWWW.",
	".WWW.",
	"..nn.",
	"..nn.",
	"..nn.",
}

//
// SMB1-fidelity entity art (contract S9). All face RIGHT unless noted;
// the renderer mirrors for left. Rune palette as everywhere else: see
// runeColors plus the per-sprite color maps in world.go (gray cheep,
// red koopa, pink princess, buzzy shell).
//

var sprPodoboo = []string{ // 6×6: lava fireball with a flame tail
	".RRRR.",
	"RYYYYR",
	"RYYYYR",
	".RRRR.",
	"..YY..",
	"...Y..",
}

var sprCheep = []string{ // 7×5 red, facing right: fin, eye, white belly
	"..RRR..",
	".RRRRWD",
	"RRRRRRR",
	"RRWWW..",
	".RR....",
}

// sprCheepGray is the same fish in gray (cheepGrayColors supplies the
// cooler body tone; the manual pins gray as the slower variant).
var sprCheepGray = sprCheep

var sprBloober = []string{ // 6×7: white squid, eyes and wavy tentacles
	".WWWW.",
	"WWWWWW",
	"WDWWDW",
	"WWWWWW",
	".WWWW.",
	".W.W.W",
	"W.W.W.",
}

var sprHammerBro = []string{ // 7×13 frame A: hammer cocked overhead
	"..GGG..",
	".GGGGG.",
	".GGGGGW",
	".KKKDKW",
	".KKKKWn",
	"GgGGGKn",
	"GgGGGK.",
	"GgGGGK.",
	"GgGGGK.",
	".KKKK..",
	".KK.KK.",
	".KK.KK.",
	"KK...KK",
}

var sprHammerBroWalk = []string{ // 7×13 frame B: stride, hammer forward
	"..GGG..",
	".GGGGG.",
	".GGGGG.",
	".KKKDK.",
	".KKKKK.",
	"GgGGGKn",
	"GgGGKWW",
	"GgGGGK.",
	"GgGGGK.",
	".KKKK..",
	".KK.KK.",
	"KK..KK.",
	"KK...KK",
}

var sprHammer = []string{ // 5×5: spinning hammer; flip alternates on Rot
	"WWW..",
	"WWW..",
	"..nn.",
	"...nn",
	"....n",
}

var sprBuzzy = []string{ // 6×8: indigo dome shell, black hide (B rune)
	"..WDDD",
	".DDDDD",
	".BBBB.",
	"BBBBBB",
	"BBBBBB",
	".DDDD.",
	".DD.DD",
	".DD.DD",
}

var sprBuzzyWalk = []string{ // 6×8: feet spread for the waddle
	"..WDDD",
	".DDDDD",
	".BBBB.",
	"BBBBBB",
	"BBBBBB",
	".DDDD.",
	".DD.DD",
	"DD..DD",
}

// Red koopa / paratroopa: the koopa art re-skinned through
// koopaRedColors (shell G→red family). Frame pairs mirror the green
// set so walk cycles keep their cadence.
var (
	sprKoopaRed     = sprKoopa
	sprKoopaRedWalk = sprKoopaWalk
	sprParaRed      = sprPara
	sprParaRedWalk  = sprParaWalk
)

var sprToad = []string{ // 7×8: mushroom cap with spots, vest, dark shoes
	".RRRRR.",
	"RRWRWRR",
	".SSSSS.",
	".SDSDS.",
	".SSSSS.",
	".WWBWW.",
	".WBBBW.",
	".D...D.",
}

var sprPrincess = []string{ // 7×12: crown, hair, gown (R maps to pink)
	"..Y.Y..",
	"..YYY..",
	".nnnnn.",
	"nnSSSnn",
	"nSDSDSn",
	"nnSSSnn",
	".nnnnn.",
	".RRRRR.",
	".RRRRR.",
	"RRRRRRR",
	"RRRRRRR",
	"RRRRRRR",
}

var sprSpring = []string{ // 7×4: red top plate over an open coil
	".RRRRR.",
	".D.R.D.",
	".D.R.D.",
	".DDDDD.",
}

var sprSpringDown = []string{ // 7×4: compressed coil (Compress > 0)
	".RRRRR.",
	".DRDRD.",
	".DRDRD.",
	".DDDDD.",
}

// Lift platform art (S9): a fixed 4×2 mushroom-platform chunk, tiled
// across the lift's width by drawLifts. The flimsy variant's pale plank
// reads as the cheaper platform that falls out from under you.
var sprLift = []string{ // 4×2
	"LLLL",
	"nnnn",
}

var sprLiftFlimsy = []string{ // 4×2
	"WWWW",
	"WDDW",
}

//
// Tile pixel patterns. Each paints one tile (Pix×Pix) at pixel (x, y);
// (tx, ty) are the tile's world coordinates, used for variation.
//

func drawGround(f *Frame, p *Palette, x, y, tx, ty int, openTop bool) {
	f.Fill(x, y, Pix, Pix, p.GroundMid)
	if openTop {
		f.Fill(x, y, Pix, 2, p.GroundLight) // sunlit crust
	}
	f.Fill(x+Pix-1, y+2, 1, Pix-2, p.GroundDark) // soil seam
	f.Set(x+(tx+ty)%3+1, y+3, p.GroundDark)      // soil speckle
	f.Set(x+(tx*2+ty)%3+2, y+4, p.GroundDark)
}

func drawBrick(f *Frame, p *Palette, x, y, tx int) {
	f.Fill(x, y, Pix, Pix, p.BrickLight)
	f.Fill(x, y+2, Pix, 1, p.BrickDark)     // upper mortar bed
	f.Fill(x, y+Pix-1, Pix, 1, p.BrickDark) // lower mortar bed
	j1, j2 := 1, 4
	if tx%2 == 1 {
		j1, j2 = 4, 1
	}
	f.Fill(x+j1, y, 1, 2, p.BrickDark)   // upper head joint
	f.Fill(x+j2, y+3, 1, 2, p.BrickDark) // lower head joint
	f.Set(x, y, p.QuestionHi)            // top-left highlight
}

// drawQuestion paints a ? block. The '?' is an asymmetric stroke with true
// '?' topology: a top bar between two dark corner rivets, a left shoulder
// that stops after one row, the right side riding the edge down, a
// diagonal hook toward the centre — then a full gap row and a dot that
// floats on body orange between the corner rivets (a dot flanked by dark
// reads as a nick in the border, not as the mark). The rivets plus the
// shaded right edge carry the classic SMB "block" read. Like the
// original, the BODY flashes between two oranges while the mark itself
// never dims.
func drawQuestion(f *Frame, p *Palette, x, y int, bright bool) {
	body := p.QuestionDim
	if bright {
		body = p.QuestionBG
	}
	f.Fill(x, y, Pix, Pix, body)
	dk := p.BrickDark
	last := x + Pix - 1
	// corner rivets
	f.Set(x, y, dk)
	f.Set(last, y, dk)
	f.Set(x, y+Pix-1, dk)
	f.Set(last, y+Pix-1, dk)
	// shaded right edge below the stroke
	f.Set(last, y+4, dk)
	m := p.QuestionMark
	f.Fill(x+1, y, 4, 1, m) // top bar between the rivets
	f.Set(x, y+1, m)        // left shoulder, stops here
	f.Set(last, y+1, m)     // right shoulder
	f.Set(last, y+2, m)     // right side descends
	f.Set(x+4, y+3, m)      // hook toward centre
	f.Set(x+3, y+3, m)
	f.Fill(x+2, y+5, 2, 1, m) // dot on orange, separated by the gap row
}

// drawUsed paints a spent ? block: dull brown with a dark border seam
// and four inner rivets.
func drawUsed(f *Frame, p *Palette, x, y int) {
	f.Fill(x, y, Pix, Pix, p.UsedBG)
	f.Fill(x, y, Pix, 1, p.Dark)
	f.Fill(x, y+Pix-1, Pix, 1, p.Dark)
	f.Fill(x, y, 1, Pix, p.Dark)
	f.Fill(x+Pix-1, y, 1, Pix, p.Dark)
	f.Set(x+1, y+1, p.Dark) // inner rivets
	f.Set(x+Pix-2, y+1, p.Dark)
	f.Set(x+1, y+Pix-2, p.Dark)
	f.Set(x+Pix-2, y+Pix-2, p.Dark)
}

// drawPipe paints a pipe span from its left column only (col 0); the
// right column is a no-op. The lip (pipe top) is a full-width 12px rim
// over an underside shadow, with the shaft starting inside the same tile
// and running unbroken to the ground — the shaft is inset 1px per side,
// so the rim overhangs it like the classic pipe silhouette.
func drawPipe(f *Frame, p *Palette, x, y, col int, lip bool) {
	if col != 0 {
		return
	}
	if lip {
		f.Fill(x, y, 2*Pix, 1, p.GreenLight)
		f.Set(x, y, p.Green)
		f.Fill(x+2*Pix-2, y, 2, 1, p.GreenDark)
		f.Fill(x, y+1, 2*Pix, 1, p.GreenDark) // rim underside
		drawPipeShaft(f, p, x, y+2, Pix-2)
		return
	}
	drawPipeShaft(f, p, x, y, Pix)
}

// drawPipeShaft paints the 10px-wide pipe shaft (pixels x+1..x+10), lit
// from the left.
func drawPipeShaft(f *Frame, p *Palette, x, y, h int) {
	f.Fill(x+1, y, 3, h, p.GreenLight)
	f.Fill(x+4, y, 5, h, p.Green)
	f.Fill(x+9, y, 2, h, p.GreenDark)
}

// drawLava paints a castle lava tile: molten red with an animated crust on
// the surface tile (bobbing bubbles keyed off the world tick).
func drawLava(f *Frame, p *Palette, x, y, tx int, surface bool, tick int) {
	f.Fill(x, y, Pix, Pix, p.FlagRed)
	f.Fill(x+Pix-1, y, 1, Pix, p.Dark) // depth shading at the pool edge
	if !surface {
		return
	}
	f.Fill(x, y, Pix, 2, p.Coin)
	f.Fill(x, y, Pix, 1, p.GoldLight)
	if (tx+tick/10)%3 == 0 {
		f.Set(x+2, y+2, p.GoldLight) // rising bubble
	}
}

// drawBridge paints one bridge plank over the lava: warm timber, not the
// castle's grey stone — a lit tan top edge, mid-brown body, dark seams
// staggered by column parity so adjacent tiles break joints, and a
// shadowed underside. The wood tones (Goomba/UsedBG) are untouched by
// castleTheme, so the bridge reads as the one organic thing in the stone
// room, exactly like SMB's bridge.
func drawBridge(f *Frame, p *Palette, x, y, tx int) {
	f.Fill(x, y, Pix, Pix, p.UsedBG)
	f.Fill(x, y, Pix, 1, p.Goomba)     // lit plank top
	f.Fill(x, y+Pix-1, Pix, 1, p.Dark) // rope shadow on the underside
	for i := 2*(tx%2) + 1; i < Pix-1; i += 2 {
		f.Fill(x+i, y+1, 1, Pix-2, p.Dark) // plank seams
	}
}

// drawFlagPole paints one pole tile; drawFlagTop paints the finial and
// pennant of the goal. drop slides the pennant down the pole during the
// pole slide (0 = top, 1 = base).
func drawFlagPole(f *Frame, p *Palette, x, y int) {
	f.Fill(x+2, y, 1, Pix, p.Pole)
}

// pennantTravelPx is how far the goal pennant rides down the pole over
// a full flag slide (drop 0→1): from the finial to the pole base, in
// pixels — 7 tile rows, finial (engine.FlagTopRow, 7) to the ground
// (engine.GroundTop, 13).
const pennantTravelPx = 7 * Pix

func drawFlagTop(f *Frame, p *Palette, x, y int, drop float64) {
	f.Fill(x+2, y, 2, 2, p.GreenLight) // ball
	f.Fill(x+2, y+2, 1, Pix-2, p.Pole) // pole
	// The pennant rides down with the player.
	dy := int(drop * float64(pennantTravelPx))
	f.Fill(x+1, y+2+dy, 1, 1, p.FlagRed) // pennant, pointing down-left
	f.Fill(x-1, y+3+dy, 3, 1, p.FlagRed)
	f.Fill(x-2, y+4+dy, 4, 1, p.FlagRed)
	f.Fill(x-2, y+5+dy, 4, 1, p.FlagRed)
}

// drawCastleFlag raises the little victory pennant on a mast above the
// castle door after the player walks in (rise 0..1). y0 is the castle's
// top row: the mast fills from y0-h up to the roof (no sky gap), and the
// pennant rises from `h-3` below the mast top — just clear of the
// crenellations — to the mast top itself.
func drawCastleFlag(f *Frame, p *Palette, x0, y0 int, rise float64) {
	if rise <= 0 {
		return
	}
	const h = 8
	mx := x0 + 2*Pix + 2
	f.Fill(mx, y0-h, 1, h, p.Pole)
	fy := y0 - 3 - int(math.Round(float64(h-3)*rise))
	f.Fill(mx+1, fy, 3, 3, p.FlagRed)
}

// drawCastle paints the goal castle: a 30×24 px keep (5×4 tiles — the
// castleRect footprint) with tower, crenellations, windows and an
// arched door.
func drawCastle(f *Frame, p *Palette, x0, y0 int) {
	half := Pix / 2
	wall := func(x, y, w, h int) {
		for ty := y; ty < y+h; ty += half {
			for tx := x; tx < x+w; tx += half {
				drawBrick(f, p, tx, ty, (tx-x)/half)
			}
		}
	}
	// Lower crenellations along the keep roof.
	for i := range 5 {
		wall(x0+i*Pix, y0+Pix+half, half, half)
	}
	wall(x0, y0+2*Pix, 5*Pix, 2*Pix) // keep
	// Tower crenellations and tower.
	wall(x0+Pix+half, y0, half, half)
	wall(x0+3*Pix, y0, half, half)
	wall(x0+Pix+half, y0+half, 2*Pix, Pix+half)
	// Arched door and windows.
	f.Fill(x0+2*Pix+1, y0+2*Pix+half, Pix-2, 1, p.Door)
	f.Fill(x0+2*Pix, y0+2*Pix+half+1, Pix, 2*Pix-half-1, p.Door)
	f.Fill(x0+half+1, y0+2*Pix+half, half+1, half+1, p.Window)
	f.Fill(x0+4*Pix-2, y0+2*Pix+half, half+1, half+1, p.Window)
}
