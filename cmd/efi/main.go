//go:build linux

package main

// mario's EFI-stub entry: this static binary IS the initramfs /init — the
// only userspace program in the single-file mario.efi boot image (a Linux
// kernel with the EFI stub and an embedded initramfs). As PID 1 it:
//
//	mounts devtmpfs            (the kernel does not automount it into initramfs)
//	opens /dev/ttyS0 for logs  (serial console; QEMU -serial captures it)
//	blits render.RenderPixels into the firmware framebuffer (/dev/fb0)
//	feeds evdev keyboard events to the game as kitty-protocol pairs
//	powers the machine off on quit (reboot(2) from PID 1)
//
// The game loop is the same shape as the browser build: own the clock,
// App.Step() per tick, rasterize via RenderPixels.

import (
	"fmt"
	"os"
	"syscall"
	"time"

	"github.com/Daviey/mario"
	"github.com/Daviey/mario/engine"
	"github.com/Daviey/mario/render"
)

func main() {
	mustMountDev()

	log := bootLog()
	logFile = log
	logf("mario-efi: init starting (pid %d)", syscall.Getpid())
	fb, err := openFramebuffer("/dev/fb0")
	if err != nil {
		logf("mario-efi: fatal: %v", err)
		halt()
	}
	kbs := openKeyboards()
	logf("mario-efi: framebuffer %dx%dx%d, keyboards: %d", fb.w, fb.h, fb.bpp, len(kbs))
	if len(kbs) == 0 {
		logf("mario-efi: warning: no keyboards found")
	}

	app := mario.New(nil)
	g := app.Game
	pal := render.NewPalette(true)
	fb.layout(g.ViewW*render.Pix, render.HudBandPx+g.ViewH*render.Pix+render.StatusBandPx)
	logf("mario-efi: world %dx%d px, scale %d, origin (%d,%d)",
		g.ViewW*render.Pix, g.ViewH*render.Pix, fb.scale, fb.ox, fb.oy)

	ticker := time.NewTicker(time.Second / time.Duration(engine.TicksPerSecond))
	defer ticker.Stop()
	for range ticker.C {
		for _, k := range kbs {
			k.drain(app.Feed)
		}
		app.Step()
		frame := render.RenderPixels(g, pal)
		if u := app.UI(); u != nil {
			frame = uiFrame(frame, pal, u, g.Tick)
		}
		fb.blit(frame.RGBBytes(), frame.W, frame.H)
		if app.Quit() {
			logf("mario-efi: quit requested, powering off")
			powerOff(log)
		}
	}
}

// mustMountDev mounts devtmpfs at /dev. The kernel only automounts it
// over a real root; initramfs must do it itself. All device nodes
// (fb0, ttyS0, input/*) appear as the drivers register.
func mustMountDev() {
	os.MkdirAll("/dev", 0o755)
	if err := syscall.Mount("devtmpfs", "/dev", "devtmpfs", 0, ""); err != nil && err != syscall.EBUSY {
		// Nowhere to log yet — the serial console needs /dev first.
		halt()
	}
}

// bootLog returns a serial write-only log file. The kernel gives PID 1
// no environment and (in initramfs) no stdio, so everything goes through
// an explicitly opened device.
func bootLog() *os.File {
	for _, dev := range []string{"/dev/ttyS0", "/dev/console"} {
		if f, err := os.OpenFile(dev, os.O_WRONLY, 0); err == nil {
			return f
		}
	}
	return os.NewFile(0, "/dev/null") // keep logf best-effort
}

var logFile *os.File

func logf(format string, args ...any) {
	if logFile == nil {
		logFile = bootLog()
	}
	logFile.WriteString(time.Now().UTC().Format("15:04:05.000 ") + fmt.Sprintf(format, args...) + "\n")
}

// powerOff powers the machine down (PID 1 holds CAP_SYS_BOOT). Falls
// back to halt if the platform rejects the poweroff.
func powerOff(log *os.File) {
	if err := syscall.Reboot(syscall.LINUX_REBOOT_CMD_POWER_OFF); err != nil {
		logf("mario-efi: poweroff failed (%v), halting", err)
		halt()
	}
	// Not reached on success.
	for {
		syscall.Reboot(syscall.LINUX_REBOOT_CMD_HALT)
	}
}

func halt() {
	for {
		syscall.Reboot(syscall.LINUX_REBOOT_CMD_HALT)
	}
}
