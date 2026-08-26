//go:build linux

package main

// Linear framebuffer output via fbdev (/dev/fb0): under the EFI-stub boot
// the firmware's GOP framebuffer is exposed by efifb (OVMF/QEMU), and the
// direct-boot dev loop uses vesafb — both surface the same fbdev API. The
// game frame (tight RGB, world pixels) is scaled up by an integer factor
// with nearest-neighbor sampling and letterboxed on black.

import (
	"encoding/binary"
	"strconv"
	"syscall"
	"unsafe"
)

// fbBitfield matches linux/fb.h (three u32s).
type fbBitfield struct {
	Offset, Length, MsbRight uint32
}

// fbVarScreeninfo matches linux/fb.h on LP64 (all u32 — no padding).
type fbVarScreeninfo struct {
	Xres, Yres, XresVirtual, YresVirtual, Xoffset, Yoffset      uint32
	BitsPerPixel, Grayscale                                     uint32
	Red, Green, Blue, Transp                                    fbBitfield
	Nonstd, Activate, Height, Width, AccelFlags                 uint32
	Pixclock, LeftMargin, RightMargin, UpperMargin, LowerMargin uint32
	HsyncLen, VsyncLen, Sync, Vmode, Rotate, Colorspace         uint32
	Reserved                                                    [4]uint32
}

// fbFixScreeninfo matches linux/fb.h on LP64 (natural alignment, 72 bytes).
type fbFixScreeninfo struct {
	ID           [16]byte
	SmemStart    uint64
	SmemLen      uint32
	Type         uint32
	TypeAux      uint32
	Visual       uint32
	XpanStep     uint16
	YpanStep     uint16
	YWrapStep    uint16
	LineLength   uint32
	MmioStart    uint64
	MmioLen      uint32
	Accel        uint32
	Capabilities uint16
	Reserved     [2]uint16
}

const (
	fbiogetVScreeninfo = 0x4600
	fbiogetFScreeninfo = 0x4602
)

// framebuffer is one open, mapped fbdev device plus the computed blit
// geometry (integer scale, letterbox origin).
type framebuffer struct {
	fd      int
	mem     []byte
	w, h    int
	bpp     int
	lineLen int
	red     fbBitfield
	green   fbBitfield
	blue    fbBitfield

	scale int // src pixel -> scale×scale dst pixels
	ox    int // dst x of the blit region
	oy    int // dst y of the blit region
}

func openFramebuffer(path string) (*framebuffer, error) {
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}

	var v fbVarScreeninfo
	if err := ioctlPtr(uintptr(fd), fbiogetVScreeninfo, unsafe.Pointer(&v)); err != nil {
		syscall.Close(fd)
		return nil, err
	}
	var f fbFixScreeninfo
	if err := ioctlPtr(uintptr(fd), fbiogetFScreeninfo, unsafe.Pointer(&f)); err != nil {
		syscall.Close(fd)
		return nil, err
	}
	if v.BitsPerPixel != 32 && v.BitsPerPixel != 16 {
		syscall.Close(fd)
		return nil, &unsupportedFBError{bpp: v.BitsPerPixel}
	}
	size := int(f.LineLength) * int(v.Yres)
	if size > int(f.SmemLen) && f.SmemLen > 0 {
		size = int(f.SmemLen)
	}
	mem, err := syscall.Mmap(fd, 0, size, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		syscall.Close(fd)
		return nil, err
	}
	// Do NOT close fd — simpledrm's fbdev emulation defio needs the file open to process page faults.
	return &framebuffer{
		fd: fd, mem: mem, w: int(v.Xres), h: int(v.Yres),
		bpp: int(v.BitsPerPixel), lineLen: int(f.LineLength),
		red: v.Red, green: v.Green, blue: v.Blue,
	}, nil
}

type unsupportedFBError struct{ bpp uint32 }

func (e *unsupportedFBError) Error() string {
	return "framebuffer: unsupported bits per pixel (want 16 or 32, got " + strconv.Itoa(int(e.bpp)) + ")"
}

// layout computes the integer scale and centered origin for a source
// frame of w×h pixels, and clears the whole framebuffer to black so the
// letterbox stays black for the session.
func (fb *framebuffer) layout(w, h int) {
	fb.scale = 1
	if w > 0 && h > 0 {
		for s := 2; s*w <= fb.w && s*h <= fb.h; s++ {
			fb.scale = s
		}
	}
	fb.ox = max(0, (fb.w-w*fb.scale)/2)
	fb.oy = max(0, (fb.h-h*fb.scale)/2)
	for i := range fb.mem {
		fb.mem[i] = 0
	}
}

// channelScale narrows an 8-bit channel to length bits (8/6/5/4).
func channelScale(v byte, length uint32) uint32 {
	switch length {
	case 8:
		return uint32(v)
	case 6:
		return uint32(v) >> 2
	case 5:
		return uint32(v) >> 3
		return uint32(v) >> 4
	}
	return uint32(v) >> 3
}

// packPixel packs 8-bit RGB into a framebuffer word per its bitfields.
func (fb *framebuffer) packPixel(r, g, b byte) uint32 {
	return channelScale(r, fb.red.Length)<<fb.red.Offset |
		channelScale(g, fb.green.Length)<<fb.green.Offset |
		channelScale(b, fb.blue.Length)<<fb.blue.Offset
}

// blit writes a tight-RGB frame (w×h) into the framebuffer using the
// layout computed by layout: nearest-neighbor upscaling, top-to-bottom.
// The letterbox region is never touched (it was cleared black once).
func (fb *framebuffer) blit(src []byte, w, h int) {
	if fb.scale < 1 || w < 1 || h < 1 {
		return
	}
	bypp := fb.bpp / 8
	for dy := range h * fb.scale {
		sy := dy / fb.scale
		row := fb.mem[(fb.oy+dy)*fb.lineLen+fb.ox*bypp:]
		srcRow := src[sy*w*3 : (sy+1)*w*3]
		switch bypp {
		case 4:
			for dx := range w * fb.scale {
				i := dx / fb.scale * 3
				binary.LittleEndian.PutUint32(row[dx*4:],
					fb.packPixel(srcRow[i], srcRow[i+1], srcRow[i+2]))
			}
		case 2:
			for dx := range w * fb.scale {
				i := dx / fb.scale * 3
				binary.LittleEndian.PutUint16(row[dx*2:], uint16(fb.packPixel(srcRow[i], srcRow[i+1], srcRow[i+2])))
			}
		}
	}
}
