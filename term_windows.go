//go:build windows

package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

// Win32 console plumbing (no external dependencies): raw + VT input, VT
// output processing, UTF-8 codepage, and QuickEdit disabled so selecting
// console text cannot freeze the game.

var kernel32 = syscall.NewLazyDLL("kernel32.dll")

var (
	procGetStdHandle       = kernel32.NewProc("GetStdHandle")
	procGetConsoleMode     = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode     = kernel32.NewProc("SetConsoleMode")
	procGetConsoleCP       = kernel32.NewProc("GetConsoleCP")
	procSetConsoleCP       = kernel32.NewProc("SetConsoleCP")
	procGetCSBI            = kernel32.NewProc("GetConsoleScreenBufferInfo")
	procSetConsoleOutputCP = kernel32.NewProc("SetConsoleOutputCP")
)

const (
	stdInputHandle  = -10
	stdOutputHandle = -11

	enableProcessedInput = 0x0001
	enableLineInput      = 0x0002
	enableEchoInput      = 0x0004
	enableQuickEditMode  = 0x0040
	enableExtendedFlags  = 0x0080
	enableVTInput        = 0x0200
	enableVTProcessing   = 0x0004
)

type coord struct{ X, Y int16 }

type smallRect struct{ Left, Top, Right, Bottom int16 }

type consoleScreenBufferInfo struct {
	Size              coord
	CursorPosition    coord
	Attributes        uint16
	Window            smallRect
	MaximumWindowSize coord
}

func stdHandle(id int) syscall.Handle {
	h, _, _ := procGetStdHandle.Call(uintptr(uint32(id)))
	return syscall.Handle(h)
}

func getMode(h syscall.Handle) (uint32, bool) {
	var mode uint32
	r, _, _ := procGetConsoleMode.Call(uintptr(h), uintptr(unsafe.Pointer(&mode)))
	return mode, r != 0
}

func setMode(h syscall.Handle, mode uint32) bool {
	r, _, _ := procSetConsoleMode.Call(uintptr(h), uintptr(mode))
	return r != 0
}

// rawMode switches the console to raw VT input with escape processing on
// output and UTF-8 on both ends, returning a restore function.
func rawMode() (func(), error) {
	hIn := stdHandle(stdInputHandle)
	hOut := stdHandle(stdOutputHandle)

	inMode, ok := getMode(hIn)
	if !ok {
		return nil, fmt.Errorf("stdin is not a console")
	}
	outMode, ok := getMode(hOut)
	if !ok {
		return nil, fmt.Errorf("stdout is not a console")
	}

	// Raw, no echo, no signal processing; QuickEdit off (console selection
	// would otherwise pause all output); VT input delivers arrow keys and
	// Ctrl+C as ANSI/byte sequences our input mapper understands.
	newIn := inMode &^ (enableProcessedInput | enableLineInput | enableEchoInput | enableQuickEditMode)
	newIn |= enableExtendedFlags | enableVTInput

	// Enable escape-sequence processing on output; pre-Windows-10 consoles
	// reject the flag, in which case ANSI simply will not render.
	newOut := outMode | enableVTProcessing

	oldCP, _, _ := procGetConsoleCP.Call()

	if !setMode(hIn, newIn) {
		return nil, fmt.Errorf("SetConsoleMode(input) failed")
	}
	setMode(hOut, newOut)
	procSetConsoleOutputCP.Call(65001) // UTF-8 glyphs
	procSetConsoleCP.Call(65001)

	return func() {
		// Restoring the original input mode also re-enables QuickEdit if
		// the user had it on; both codepages go back to their originals.
		setMode(hIn, inMode)
		setMode(hOut, outMode)
		procSetConsoleOutputCP.Call(oldCP)
		procSetConsoleCP.Call(oldCP)
	}, nil
}

func consoleInfo() (rows, cols int) {
	var csbi consoleScreenBufferInfo
	r, _, _ := procGetCSBI.Call(uintptr(stdHandle(stdOutputHandle)), uintptr(unsafe.Pointer(&csbi)))
	if r == 0 {
		return 0, 0
	}
	return int(csbi.Window.Bottom - csbi.Window.Top + 1),
		int(csbi.Window.Right - csbi.Window.Left + 1)
}

// termWidth returns the console width in columns (0 when unknown).
func termWidth() int { _, c := consoleInfo(); return c }

// termHeight returns the console height in rows (0 when unknown).
func termHeight() int { r, _ := consoleInfo(); return r }
