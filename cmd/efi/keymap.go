//go:build linux

package main

// evdev key codes (linux/input-event-codes.h) for every key the game
// listens to, translated into the same terminal byte sequences the input
// mapper already parses — exactly the contract the browser page uses
// (web/index.html). Game keys MUST go out as explicit kitty-protocol
// press/release pairs: a press stays held until its release arrives, while
// a bare legacy byte decays after the mapper's ~0.2s silence window.
// One-shot keys (ENTER, ESC, BACKSPACE) are plain bytes, as on the page.

import "strconv"

// Evdev key codes we route (values from linux/input-event-codes.h).
const (
	evEsc        = 1
	evMinus      = 12
	evBackspace  = 14
	evEnter      = 28
	evKey1       = 2
	evKey0       = 11
	evQ          = 16
	evW          = 17
	evE          = 18
	evR          = 19
	evY          = 21
	evP          = 25
	evA          = 30
	evS          = 31
	evD          = 32
	evLeftShift  = 42
	evX          = 45
	evK          = 37
	evL          = 38
	evN          = 49
	evDot        = 52
	evRightShift = 54
	evSpace      = 57
	evUp         = 103
	evLeft       = 105
	evRight      = 106
	evDown       = 108
)

// charForCode maps evdev codes to their unicode codepoint (lower-case
// letters; the caller upper-cases while shift is held). Everything the
// name-entry charset accepts (A-Z 0-9 . -) plus the game's keys.
var charForCode = map[uint16]byte{
	evQ: 'q', evW: 'w', evE: 'e', evR: 'r', evY: 'y', evP: 'p', evA: 'a', evSpace: ' ',
	evS: 's', evD: 'd', evL: 'l', evN: 'n', evX: 'x', evK: 'k',
	evKey1: '1', 3: '2', 4: '3', 5: '4', 6: '5', 7: '6', 8: '7', 9: '8', 10: '9', evKey0: '0',
	evMinus: '-', evDot: '.',
}

// keySeqs returns the press and release byte sequences for one evdev
// code. Empty strings mean the key is not routed to the game (modifier
// or unmapped). release == "" with a non-empty press means one-shot.
func keySeqs(code uint16, shift bool) (press, release string) {
	switch code {
	case evEnter:
		return "\r", ""
	case evEsc:
		return "\x1b", ""
	case evBackspace:
		return "\x7f", ""
	case evLeft:
		return "\x1b[1;1:1D", "\x1b[1;1:3D"
	case evRight:
		return "\x1b[1;1:1C", "\x1b[1;1:3C"
	case evUp:
		return "\x1b[1;1:1A", "\x1b[1;1:3A"
	case evDown:
		return "\x1b[1;1:1B", "\x1b[1;1:3B"
	}
	if ch, ok := charForCode[code]; ok {
		if shift && ch >= 'a' && ch <= 'z' {
			ch -= 'a' - 'A'
		}
		cp := strconv.Itoa(int(ch))
		return "\x1b[" + cp + ";1:1u", "\x1b[" + cp + ";1:3u"
	}
	return "", ""
}

// isShift reports whether an evdev code is a shift modifier.
func isShift(code uint16) bool { return code == evLeftShift || code == evRightShift }
