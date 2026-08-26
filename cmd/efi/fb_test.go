//go:build linux

package main

import (
	"encoding/binary"
	"testing"
)

func testFB(w, h, bpp int, r, g, b fbBitfield) *framebuffer {
	return &framebuffer{
		mem:     make([]byte, w*h*bpp/8),
		w:       w,
		h:       h,
		bpp:     bpp,
		lineLen: w * bpp / 8,
		red:     r,
		green:   g,
		blue:    b,
	}
}

func px32(fb *framebuffer, x, y int) uint32 {
	off := y*fb.lineLen + x*4
	return binary.LittleEndian.Uint32(fb.mem[off:])
}

func px16(fb *framebuffer, x, y int) uint16 {
	off := y*fb.lineLen + x*2
	return binary.LittleEndian.Uint16(fb.mem[off:])
}

func TestBlitBGRA(t *testing.T) {
	fb := testFB(8, 8, 32,
		fbBitfield{Offset: 16, Length: 8},
		fbBitfield{Offset: 8, Length: 8},
		fbBitfield{Offset: 0, Length: 8})
	fb.layout(2, 2) // scale 4, origin (0,0)
	src := []byte{
		0xFF, 0, 0, 0, 0xFF, 0,
		0, 0, 0xFF, 0xFF, 0xFF, 0xFF,
	}
	fb.blit(src, 2, 2)

	red := uint32(0xFF) << 16
	green := uint32(0xFF) << 8
	blue := uint32(0xFF)
	white := red | green | blue

	if px32(fb, 0, 0) != red {
		t.Errorf("dst(0,0) = %x, want %x", px32(fb, 0, 0), red)
	}
	if px32(fb, 3, 3) != red {
		t.Errorf("dst(3,3) = %x, want %x", px32(fb, 3, 3), red)
	}
	if px32(fb, 7, 0) != green {
		t.Errorf("dst(7,0) = %x, want %x", px32(fb, 7, 0), green)
	}
	if px32(fb, 0, 7) != blue {
		t.Errorf("dst(0,7) = %x, want %x", px32(fb, 0, 7), blue)
	}
	if px32(fb, 7, 7) != white {
		t.Errorf("dst(7,7) = %x, want %x", px32(fb, 7, 7), white)
	}
}

func TestBlitRGB565(t *testing.T) {
	fb := testFB(8, 8, 16,
		fbBitfield{Offset: 11, Length: 5},
		fbBitfield{Offset: 5, Length: 6},
		fbBitfield{Offset: 0, Length: 5})
	fb.layout(2, 2)
	src := []byte{
		0xFF, 0, 0, 0, 0xFF, 0,
		0, 0, 0xFF, 0xFF, 0xFF, 0xFF,
	}
	fb.blit(src, 2, 2)

	red := uint16(0xF800)
	white := uint16(0xFFFF)
	if px16(fb, 0, 0) != red {
		t.Errorf("dst(0,0) = %x, want %x", px16(fb, 0, 0), red)
	}
	if px16(fb, 7, 7) != white {
		t.Errorf("dst(7,7) = %x, want %x", px16(fb, 7, 7), white)
	}
}

func TestBlitLetterbox(t *testing.T) {
	fb := testFB(13, 9, 32, fbBitfield{16, 8, 0}, fbBitfield{8, 8, 0}, fbBitfield{0, 8, 0})
	fb.layout(2, 2)
	// fb = 13x9; src = 2x2. scale max(s): 4x2=8<=13 && 4x2=8<=9 -> scale 4.
	// ox = (13-8)/2 = 2. oy = (9-8)/2 = 0.
	src := []byte{
		0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
		0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
	}
	fb.blit(src, 2, 2)

	if px32(fb, 0, 0) != 0 {
		t.Errorf("dst(0,0) = %x, want 0 (black letterbox)", px32(fb, 0, 0))
	}
	if px32(fb, 1, 0) != 0 {
		t.Errorf("dst(1,0) = %x, want 0", px32(fb, 1, 0))
	}
	if px32(fb, 2, 0) != 0xFFFFFF {
		t.Errorf("dst(2,0) = %x, want white", px32(fb, 2, 0))
	}
	if px32(fb, 9, 7) != 0xFFFFFF {
		t.Errorf("dst(9,7) = %x, want white", px32(fb, 9, 7))
	}
	if px32(fb, 10, 8) != 0 {
		t.Errorf("dst(10,8) = %x, want 0", px32(fb, 10, 8))
	}
}

func TestLayoutScale(t *testing.T) {
	fb := testFB(1024, 768, 32, fbBitfield{}, fbBitfield{}, fbBitfield{})
	fb.layout(240, 105)
	// min(1024/240=4, 768/105=7) -> 4
	if fb.scale != 4 {
		t.Errorf("scale = %d, want 4", fb.scale)
	}
	// ox = (1024 - 960)/2 = 32
	if fb.ox != 32 {
		t.Errorf("ox = %d, want 32", fb.ox)
	}
	// oy = (768 - 420)/2 = 174
	if fb.oy != 174 {
		t.Errorf("oy = %d, want 174", fb.oy)
	}
}
