// icongen renders the game's Mario sprites (render/sprites.go, verbatim)
// into a multi-resolution Windows .ico using PNG-compressed entries —
// no ImageMagick, no Apple tooling, stdlib only. The .ico feeds windres,
// which produces the committed mario_windows_amd64.syso that every
// windows/amd64 go build links automatically:
//
//	go run ./tools/icongen -o icon.ico
//	printf '1 ICON "icon.ico"\n' > icon.rc
//	x86_64-w64-mingw32-windres --input icon.rc -O coff -o mario_windows_amd64.syso
//
// Regenerate only when the sprite art changes; the syso is committed so
// CI (which has no mingw toolchain) keeps producing identical exes.
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
)

// Palette hexes mirror render.go NewPalette / the web loader's PC map.
var cols = map[byte]color.RGBA{
	'R': {0xFF, 0x3B, 0x30, 0xFF}, // player red
	'S': {0xFF, 0xC8, 0x9E, 0xFF}, // skin
	'D': {0x1A, 0x0E, 0x04, 0xFF}, // dark
	'B': {0x2B, 0x5D, 0xD7, 0xFF}, // overalls
}

// Sprite art — render/sprites.go verbatim ('.' transparent).
var sprMarioSuper = []string{ // 7×13 standing pose
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

var sprMarioSmall = []string{ // 7×7 stand pose (better read at tiny sizes)
	"..RRR..",
	".RRRRR.",
	".SDSDS.",
	".SSSSS.",
	"RRBBBRR",
	".BBBBB.",
	".DD.DD.",
}

// render draws art centered on a size×size canvas at integer scale,
// transparent background. The super pose carries ≥48 px sizes; the small
// one reads better at 16–32 px.
func render(art []string, size int) *image.RGBA {
	w, h := len(art[0]), len(art)
	sc := size / w
	if sh := size / h; sh < sc {
		sc = sh
	}
	if sc < 1 {
		sc = 1
	}
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	ox, oy := (size-w*sc)/2, (size-h*sc)/2
	for row := 0; row < h; row++ {
		for col := 0; col < w; col++ {
			c, ok := cols[art[row][col]]
			if !ok {
				continue // '.' transparent
			}
			for dy := 0; dy < sc; dy++ {
				for dx := 0; dx < sc; dx++ {
					img.SetRGBA(ox+col*sc+dx, oy+row*sc+dy, c)
				}
			}
		}
	}
	return img
}

// pngBytes encodes img as a PNG blob for embedding in the .ico.
func pngBytes(img *image.RGBA) ([]byte, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeICO emits a Vista+-style .ico: a 6-byte header, one 16-byte
// directory entry per size, then raw PNG payloads.
func writeICO(path string, sizes []int) error {
	type entry struct {
		data []byte
	}
	entries := make([]entry, len(sizes))
	for i, s := range sizes {
		art := sprMarioSmall
		if s >= 48 {
			art = sprMarioSuper
		}
		b, err := pngBytes(render(art, s))
		if err != nil {
			return err
		}
		entries[i].data = b
	}

	var out bytes.Buffer
	header := make([]byte, 6)
	binary.LittleEndian.PutUint16(header[0:], 0) // reserved
	binary.LittleEndian.PutUint16(header[2:], 1) // type: icon
	binary.LittleEndian.PutUint16(header[4:], uint16(len(sizes)))
	out.Write(header)

	offset := 6 + 16*len(sizes)
	for i, s := range sizes {
		e := make([]byte, 16)
		if s < 256 {
			e[0], e[1] = byte(s), byte(s)
		} // else 0 == 256
		e[2] = 0                                 // palette count (PNG entries carry none)
		e[3] = 0                                 // reserved
		binary.LittleEndian.PutUint16(e[4:], 1)  // planes
		binary.LittleEndian.PutUint16(e[6:], 32) // bits per pixel
		binary.LittleEndian.PutUint32(e[8:], uint32(len(entries[i].data)))
		binary.LittleEndian.PutUint32(e[12:], uint32(offset))
		out.Write(e)
		offset += len(entries[i].data)
	}
	for _, en := range entries {
		out.Write(en.data)
	}
	return os.WriteFile(path, out.Bytes(), 0o644)
}

func main() {
	out := flag.String("o", "icon.ico", "output .ico path")
	flag.Parse()
	sizes := []int{256, 128, 64, 48, 32, 24, 16}
	if err := writeICO(*out, sizes); err != nil {
		fmt.Fprintln(os.Stderr, "icongen:", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s (%d sizes)\n", *out, len(sizes))
}
