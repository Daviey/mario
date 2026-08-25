package input

import (
	"strconv"
	"strings"
)

// PlainDecoder re-encodes a kitty-protocol byte stream as the plain legacy
// bytes that byte-oriented consumers expect. The runner pushes kitty flags
// 1|2|8, so with a protocol-capable terminal even letters, Enter and
// Backspace arrive as CSI-u sequences instead of text — but the leaderboard
// UI and the title-screen 'l' trigger consume single plain bytes. Press and
// repeat events decode to their codepoint byte; release events are dropped
// (legacy streams have no releases, and the UI treats each byte as an edge).
// Non-text CSI/SS3 sequences (arrows, function keys) decode to nothing.
// Terminals without the protocol send plain bytes already; those pass
// through untouched.
type PlainDecoder struct {
	buf []byte
}

// NewPlainDecoder returns a decoder with no partial sequence buffered.
func NewPlainDecoder() *PlainDecoder { return &PlainDecoder{} }

// Feed buffers raw terminal bytes and returns the plain-byte view of every
// complete event they contained. A sequence split across reads is held back
// until its final byte arrives.
func (d *PlainDecoder) Feed(b []byte) []byte {
	d.buf = append(d.buf, b...)
	var out []byte
	for len(d.buf) > 0 {
		n, ev, ok := plainSeq(d.buf)
		if !ok {
			break // incomplete escape sequence; wait for more bytes
		}
		d.buf = d.buf[n:]
		if ev != 0 {
			out = append(out, ev)
		}
	}
	if len(d.buf) > 16 { // garbage guard: never wedged on noise
		d.buf = nil
	}
	return out
}

// plainSeq decodes one event from the head of b into the plain byte it
// would have been on a legacy terminal (0 = nothing to report). It returns
// ok=false when more bytes are needed to complete an escape sequence.
func plainSeq(b []byte) (int, byte, bool) {
	c := b[0]
	if c != 0x1b {
		return 1, c, true
	}
	if len(b) == 1 {
		return 0, 0, false // lone ESC: may be a sequence prefix
	}
	switch b[1] {
	case '[': // CSI
		j := 2
		for j < len(b) && !(b[j] >= 0x40 && b[j] <= 0x7e) {
			j++
		}
		if j >= len(b) {
			return 0, 0, false
		}
		if b[j] != 'u' {
			return j + 1, 0, true // arrows, edits, reports: not text keys
		}
		return j + 1, kittyText(string(b[2:j])), true
	case 'O': // SS3 (function keys): not text
		if len(b) < 3 {
			return 0, 0, false
		}
		return 3, 0, true
	default: // ESC + byte: alt-modified key, report the plain byte
		if b[1] == 0 {
			return 2, 0, true
		}
		return 2, b[1], true
	}
}

// kittyText maps a CSI-u parameter list to the legacy byte the key stood
// for, or 0 when it should be dropped (release events, unprintables).
func kittyText(params string) byte {
	parts := strings.Split(params, ";")
	if len(parts) == 0 {
		return 0
	}
	sub := strings.Split(parts[len(parts)-1], ":")
	if len(sub) > 1 {
		if t, err := strconv.Atoi(sub[len(sub)-1]); err == nil && t == 3 {
			return 0 // release
		}
	}
	code, err := strconv.Atoi(strings.Split(parts[0], ":")[0])
	if err != nil {
		return 0
	}
	switch {
	case code == '\r' || code == '\n':
		return '\r'
	case code == 0x7f:
		return 0x7f
	case code == 0x1b:
		return 0x1b
	case code >= 0x20 && code < 0x7f:
		return byte(code)
	}
	return 0
}
