//go:build linux

package main

// Keyboard input via evdev: open every /dev/input/eventN that looks like a
// keyboard, then poll their queues each game tick and translate EV_KEY
// press/release events into the mapper's byte sequences. Autorepeat
// (value 2) is skipped — kitty-protocol holds don't need it.

import (
	"os"
	"syscall"
	"unsafe"
)

const (
	evSyn = 0x00
	evKey = 0x01
)

// input_event matches the kernel's 64-bit layout (struct input_event with
// __s64 time fields since 4.16): 24 bytes.
type inputEvent struct {
	Sec   int64
	Usec  int64
	Type  uint16
	Code  uint16
	Value int32
	_     [4]byte // explicit tail padding; sizeof stays 24
}

// ioc computes an _IOC(dir, type, nr, size) request word.
func ioc(dir, typ, nr, size uintptr) uintptr {
	return dir<<30 | size<<16 | typ<<8 | nr
}

// eviocgbit returns the EVIOCGBIT request for a buffer of len(buf).
func eviocgbit(ev uintptr, buf []byte) uintptr {
	return ioc(2 /*_IOC_READ*/, 'E', 0x20+ev, uintptr(len(buf)))
}

func ioctlPtr(fd, req uintptr, arg unsafe.Pointer) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg))
	if errno != 0 {
		return errno
	}
	return nil
}

// hasBit reports whether bit n is set in a little-endian bitmap.
func hasBit(buf []byte, n int) bool {
	return buf[n/8]&(1<<(n%8)) != 0
}

// keyboard is one open evdev device.
type keyboard struct {
	fd    int
	shift bool
	buf   [24 * 16]byte // a handful of events per drain
}

// openKeyboards returns every /dev/input/eventN that produces the keys we
// route. Mice (BTN_MOUSE) are excluded; pure modifier pads without any
// routable key are skipped too.
func openKeyboards() []*keyboard {
	entries, err := os.ReadDir("/dev/input")
	if err != nil {
		return nil
	}
	var kbs []*keyboard
	for _, e := range entries {
		fd, err := syscall.Open("/dev/input/"+e.Name(), syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
		if err != nil {
			continue
		}
		if !isGameKeyboard(fd) {
			syscall.Close(fd)
			continue
		}
		kbs = append(kbs, &keyboard{fd: fd})
	}
	return kbs
}

// isGameKeyboard probes an open evdev fd: it must support EV_KEY with at
// least one routable key and must not be a mouse.
func isGameKeyboard(fd int) bool {
	var evbits [8]byte
	if err := ioctlPtr(uintptr(fd), eviocgbit(0, evbits[:]), unsafe.Pointer(&evbits[0])); err != nil {
		return false
	}
	if !hasBit(evbits[:], evKey) {
		return false
	}
	var keybits [256]byte
	if err := ioctlPtr(uintptr(fd), eviocgbit(evKey, keybits[:]), unsafe.Pointer(&keybits[0])); err != nil {
		return false
	}
	if hasBit(keybits[:], 0x110) { // BTN_MOUSE
		return false
	}
	for code := range charForCode {
		if hasBit(keybits[:], int(code)) {
			return true
		}
	}
	for _, code := range []int{evEnter, evEsc, evLeft, evRight, evUp, evDown} {
		if hasBit(keybits[:], code) {
			return true
		}
	}
	return false
}

// drain reads everything queued on the fd, feeding each translated key
// sequence to feed. Non-blocking: returns immediately when empty.
func (k *keyboard) drain(feed func([]byte)) {
	for {
		n, err := syscall.Read(k.fd, k.buf[:])
		if n <= 0 {
			if err == syscall.EAGAIN || err == syscall.EINTR {
				return
			}
			// EWOULDBLOCK == EAGAIN; anything else means the device is
			// gone — stop reading it this tick either way.
			return
		}
		for off := 0; off+24 <= n; off += 24 {
			ev := (*inputEvent)(unsafe.Pointer(&k.buf[off]))
			if ev.Type != evKey {
				continue
			}
			switch ev.Value {
			case 1: // press
				if isShift(ev.Code) {
					k.shift = true
					continue
				}
				if press, _ := keySeqs(ev.Code, k.shift); press != "" {
					feed([]byte(press))
				}
			case 0: // release
				if isShift(ev.Code) {
					k.shift = false
					continue
				}
				if _, release := keySeqs(ev.Code, k.shift); release != "" {
					feed([]byte(release))
				}
			}
		}
		if n < len(k.buf) {
			return // short read: queue drained
		}
	}
}
