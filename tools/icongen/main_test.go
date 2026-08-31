package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	mariorender "github.com/Daviey/mario/render"
)

// iconSizes mirrors the set baked into the committed icon.ico.
var iconSizes = []int{256, 128, 64, 48, 32, 24, 16}

type icoEntry struct {
	size   int
	length uint32
	offset uint32
	png    []byte
}

func parseICO(t *testing.T, b []byte) []icoEntry {
	t.Helper()
	if len(b) < 6 {
		t.Fatalf("ico too short: %d bytes", len(b))
	}
	if got := binary.LittleEndian.Uint16(b[2:]); got != 1 {
		t.Fatalf("ico type = %d, want 1 (icon)", got)
	}
	n := int(binary.LittleEndian.Uint16(b[4:]))
	if n != len(iconSizes) {
		t.Fatalf("ico entry count = %d, want %d", n, len(iconSizes))
	}
	entries := make([]icoEntry, 0, n)
	for i := range n {
		e := b[6+16*i : 6+16*i+16]
		size := int(e[0])
		if size == 0 {
			size = 256
		}
		if int(e[1]) != size%256 && e[1] != 0 && int(e[1]) != size {
			t.Fatalf("entry %d: width %d != height %d", i, e[0], e[1])
		}
		length := binary.LittleEndian.Uint32(e[8:])
		offset := binary.LittleEndian.Uint32(e[12:])
		if int(offset)+int(length) > len(b) {
			t.Fatalf("entry %d: payload out of bounds (offset %d length %d, file %d)", i, offset, length, len(b))
		}
		entries = append(entries, icoEntry{size: size, length: length, offset: offset, png: b[offset : offset+length]})
	}
	return entries
}

func decodeEntry(t *testing.T, e icoEntry) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(e.png))
	if err != nil {
		t.Fatalf("entry size %d: png decode: %v", e.size, err)
	}
	return img
}

// TestIconMatchesCommittedArtifact regenerates the .ico in a temp dir and
// pins it against the committed icon.ico: same directory entries and
// pixel-identical payloads. icon.ico is the regen source of the committed
// mario_windows_amd64.syso, so any art/palette drift in this tool must be a
// conscious regeneration (then windres + commit), never a silent change.
// Compared structurally + pixel-wise (not by raw bytes) so a Go PNG-encoder
// change cannot false-trip the pin.
func TestIconMatchesCommittedArtifact(t *testing.T) {
	out := filepath.Join(t.TempDir(), "icon.ico")
	if err := writeICO(out, iconSizes); err != nil {
		t.Fatalf("writeICO: %v", err)
	}
	fresh, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	committed, err := os.ReadFile(filepath.Join("..", "..", "icon.ico"))
	if err != nil {
		t.Fatalf("committed icon.ico unreadable: %v", err)
	}

	fe := parseICO(t, fresh)
	ce := parseICO(t, committed)
	if len(fe) != len(ce) {
		t.Fatalf("entry count fresh %d vs committed %d", len(fe), len(ce))
	}
	for i := range fe {
		if fe[i].size != ce[i].size {
			t.Errorf("entry %d: size fresh %d vs committed %d", i, fe[i].size, ce[i].size)
		}
		if fe[i].length != ce[i].length {
			t.Errorf("entry %d (size %d): payload length fresh %d vs committed %d", i, fe[i].size, fe[i].length, ce[i].length)
		}
		fi, ci := decodeEntry(t, fe[i]), decodeEntry(t, ce[i])
		fb, cb := fi.Bounds(), ci.Bounds()
		if fb != cb {
			t.Errorf("entry %d: bounds fresh %v vs committed %v", i, fb, cb)
			continue
		}
		for y := fb.Min.Y; y < fb.Max.Y; y++ {
			for x := fb.Min.X; x < fb.Max.X; x++ {
				fr, fga, fbb, fa := fi.At(x, y).RGBA()
				cr, cga, cbb, ca := ci.At(x, y).RGBA()
				if fr != cr || fga != cga || fbb != cbb || fa != ca {
					t.Errorf("entry %d: pixel (%d,%d) fresh %v vs committed %v", i, x, y,
						color.RGBA{uint8(fr >> 8), uint8(fga >> 8), uint8(fbb >> 8), uint8(fa >> 8)},
						color.RGBA{uint8(cr >> 8), uint8(cga >> 8), uint8(cbb >> 8), uint8(ca >> 8)})
					return
				}
			}
		}
	}
}

// TestPaletteMatchesRender pins icongen's four hexes to the game palette so
// the icon can never drift from the in-game colors (same guard as
// internal/art's parity test).
func TestPaletteMatchesRender(t *testing.T) {
	pal := mariorender.NewPalette(mariorender.Colors24)
	want := map[byte]uint32{
		'R': uint32(pal.Player.RGB),
		'S': uint32(pal.Skin.RGB),
		'D': uint32(pal.Dark.RGB),
		'B': uint32(pal.Overall.RGB),
	}
	for ch, rgb := range want {
		c := cols[ch]
		got := uint32(c.R)<<16 | uint32(c.G)<<8 | uint32(c.B)
		if got != rgb {
			t.Errorf("cols[%q] = %#06X, render palette = %#06X", ch, got, rgb)
		}
		if c.A != 0xFF {
			t.Errorf("cols[%q] alpha = %#02X, want opaque", ch, c.A)
		}
	}
}
