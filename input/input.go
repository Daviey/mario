// Package input maps raw terminal bytes to engine.Input.
//
//   - Legacy: plain keydown bytes only (no release events). Held keys are
//     inferred from OS key-repeat: a key counts as held while repeats keep
//     arriving, and expires after holdWindow ticks of silence.
//   - Kitty keyboard protocol: CSI-u sequences with explicit press/repeat/
//     release events. Releases clear the key immediately. The runner pushes
//     flags 1|2|8 so every key — including plain letters and space —
//     reports release events; the legacy regime only remains for terminals
//     without the protocol.
package input

import (
	"strconv"
	"strings"
	"sync"

	"mario/engine"
)

type key int

const (
	kLeft key = iota
	kRight
	kUp
	kDown
	kRun
	keyCount
)

// Edge-triggered special keys.
const (
	spNone = iota
	spQuit
	spPause
	spRestart
	spAny
)

const holdWindow = 12   // ~0.2s of key-repeat silence before a legacy key expires
const staleSeqPolls = 3 // quiet polls before an incomplete escape sequence is dropped

type keyEvent struct {
	k       key
	hasKey  bool
	special int
	evType  int // 0 legacy press, 1 kitty press, 2 kitty repeat, 3 release
	src     int // kitty source id: codepoint, or 0x10000|key-final; 0 = untracked
}

// Mapper converts a byte stream into per-tick engine.Input values.
type Mapper struct {
	mu          sync.Mutex
	now         int
	lastSeen    [keyCount]int
	pressedAt   [keyCount]int // tick of the newest PRESS (repeats don't refresh it)
	sticky      [keyCount]bool
	sources     map[int]key // kitty press sources still held, for exact releases
	buf         []byte
	feedAge     int // polls since the last Feed delivered bytes
	pendQuit    bool
	pendPause   bool
	pendRestart bool
	pendAny     bool
}

// NewMapper returns a mapper with no keys held.
func NewMapper() *Mapper { return &Mapper{sources: make(map[int]key)} }

// Feed delivers raw bytes read from the terminal.
func (m *Mapper) Feed(data []byte) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(data) > 0 {
		m.feedAge = 0
	}
	m.buf = append(m.buf, data...)
	m.drain()
}

// Poll advances the mapper clock and returns the input for this tick.
func (m *Mapper) Poll() engine.Input {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.now++
	m.feedAge++
	// An escape sequence that never completed (bytes lost or split by a
	// slow link) would otherwise swallow every following key, because any
	// letter can parse as the sequence's final byte. Flush it once it has
	// gone quiet for a few ticks. A lone ESC held this long is the ESC key
	// on legacy terminals: quit.
	if len(m.buf) > 0 && m.feedAge >= staleSeqPolls {
		if len(m.buf) == 1 && m.buf[0] == 0x1b {
			m.pendQuit = true
		}
		m.buf = nil
	}
	left, right := m.held(kLeft), m.held(kRight)
	if left && right {
		// Both directions at once: the newest PRESS wins, so a fresh tap
		// reverses instantly (SMB-style) instead of the two cancelling to
		// a standstill — and neither a stale legacy phantom hold nor the
		// repeat stream of a genuinely held key can outrank a newer press.
		if m.pressedAt[kLeft] > m.pressedAt[kRight] {
			right = false
		} else {
			left = false
		}
	}
	in := engine.Input{
		Left:    left,
		Right:   right,
		Up:      m.held(kUp),
		Down:    m.held(kDown),
		Run:     m.held(kRun),
		Quit:    m.pendQuit,
		Pause:   m.pendPause,
		Restart: m.pendRestart,
		AnyKey:  m.pendAny,
	}
	m.pendQuit, m.pendPause, m.pendRestart, m.pendAny = false, false, false, false
	return in
}

func (m *Mapper) held(k key) bool {
	if m.sticky[k] {
		return true
	}
	ls := m.lastSeen[k]
	return ls > 0 && m.now-ls <= holdWindow
}

func (m *Mapper) drain() {
	for len(m.buf) > 0 {
		n, ev, ok := parseSeq(m.buf)
		if !ok {
			break // incomplete escape sequence; wait for more bytes
		}
		m.buf = m.buf[n:]
		m.apply(ev)
	}
}

func (m *Mapper) apply(ev keyEvent) {
	if ev.evType == 3 { // release
		if !ev.hasKey || ev.k >= keyCount {
			return
		}
		// Clear the key only when its last held source was released:
		// arrow-right and 'd' both mean Right, and releasing one must
		// not drop the other.
		if ev.src != 0 {
			if _, ok := m.sources[ev.src]; ok {
				delete(m.sources, ev.src)
				for _, k := range m.sources {
					if k == ev.k {
						return // another source still holds it
					}
				}
			}
		}
		m.sticky[ev.k] = false
		m.lastSeen[ev.k] = 0
		return
	}
	if ev.hasKey && ev.k < keyCount {
		if ev.evType >= 1 {
			m.sticky[ev.k] = true
			if ev.src != 0 {
				m.sources[ev.src] = ev.k
			}
		}
		if ev.evType != 2 { // repeats don't refresh press recency
			m.pressedAt[ev.k] = m.now + 1
		}
		m.lastSeen[ev.k] = m.now + 1
	}
	if ev.evType == 0 || ev.evType == 1 { // edges fire on press only
		switch ev.special {
		case spQuit:
			m.pendQuit = true
		case spPause:
			m.pendPause = true
		case spRestart:
			m.pendRestart = true
		case spAny:
			m.pendAny = true
		}
	}
}

// parseSeq decodes one event from the head of b. It returns ok=false when
// more bytes are needed to complete an escape sequence.
func parseSeq(b []byte) (int, keyEvent, bool) {
	c := b[0]
	if c != 0x1b {
		if c == 0 {
			return 1, keyEvent{}, true // ignore NUL
		}
		if c == 0x03 {
			return 1, keyEvent{special: spQuit}, true // Ctrl+C (VINTR byte in raw mode)
		}
		ev, _ := mappedKey(int(c))
		return 1, ev, true
	}
	if len(b) == 1 {
		return 0, keyEvent{}, false // may be a lone ESC or a sequence prefix
	}
	switch b[1] {
	case '[': // CSI
		j := 2
		for j < len(b) && !(b[j] >= 0x40 && b[j] <= 0x7e) {
			j++
		}
		if j >= len(b) {
			if len(b) > 16 {
				return len(b), keyEvent{}, true // garbage guard: drop
			}
			return 0, keyEvent{}, false
		}
		return j + 1, csiEvent(string(b[2:j]), b[j]), true
	case 'O': // SS3 (application cursor keys)
		if len(b) < 3 {
			return 0, keyEvent{}, false
		}
		var k key
		switch b[2] {
		case 'A':
			k = kUp
		case 'B':
			k = kDown
		case 'C':
			k = kRight
		case 'D':
			k = kLeft
		default:
			return 3, keyEvent{}, true // F1-F4 etc: consume silently
		}
		return 3, keyEvent{k: k, hasKey: true, src: 0x10000 | int(b[2])}, true
	default: // ESC + byte: treat as the plain (alt-) key
		if b[1] == 0 {
			return 2, keyEvent{}, true
		}
		ev, _ := mappedKey(int(b[1]))
		return 2, ev, true
	}
}

// csiEvent interprets a completed CSI sequence.
func csiEvent(params string, final byte) keyEvent {
	parts := strings.Split(params, ";")
	evType := 0
	if n := len(parts); n > 0 {
		sub := strings.Split(parts[n-1], ":")
		if len(sub) > 1 {
			if t, err := strconv.Atoi(sub[len(sub)-1]); err == nil {
				evType = t
			}
		}
	}
	switch final {
	case 'A':
		return keyEvent{k: kUp, hasKey: true, evType: evType, src: 0x10000 | 'A'}
	case 'B':
		return keyEvent{k: kDown, hasKey: true, evType: evType, src: 0x10000 | 'B'}
	case 'C':
		return keyEvent{k: kRight, hasKey: true, evType: evType, src: 0x10000 | 'C'}
	case 'D':
		return keyEvent{k: kLeft, hasKey: true, evType: evType, src: 0x10000 | 'D'}
	case 'u':
		code := 0
		if len(parts) > 0 {
			sub := strings.Split(parts[0], ":")
			code, _ = strconv.Atoi(sub[0])
		}
		// Ctrl+C via the kitty protocol: CSI 99;5:<event> u (ctrl = mod bit 2).
		if code == 'c' && csiCtrlHeld(parts) {
			return keyEvent{special: spQuit, evType: evType}
		}
		if ev, ok := mappedKey(code); ok {
			ev.evType = evType
			ev.src = code
			return ev
		}
		if evType == 0 || evType == 1 {
			return keyEvent{special: spAny, evType: evType}
		}
		return keyEvent{evType: evType}
	case '~':
		return keyEvent{} // edit/function keys: consume silently
	default:
		return keyEvent{} // cursor reports, unknown sequences: consume silently
	}
}

// csiCtrlHeld reports whether the CSI parameter list carries a ctrl
// modifier (modifier bitmask field, ctrl = bit 2, stored as mask+1).
func csiCtrlHeld(parts []string) bool {
	if len(parts) < 2 {
		return false
	}
	sub := strings.Split(parts[1], ":")
	mods, err := strconv.Atoi(sub[0])
	if err != nil || mods < 1 {
		return false
	}
	return (mods-1)&0x4 != 0
}

// mappedKey maps a key code (ASCII byte or kitty codepoint) to an event.
func mappedKey(code int) (keyEvent, bool) {
	switch code {
	case 'a', 'A':
		return keyEvent{k: kLeft, hasKey: true}, true
	case 'd', 'D':
		return keyEvent{k: kRight, hasKey: true}, true
	case 'w', 'W':
		return keyEvent{k: kUp, hasKey: true}, true
	case 's', 'S':
		return keyEvent{k: kDown, hasKey: true}, true
	case 'x', 'X':
		return keyEvent{k: kRun, hasKey: true}, true
	case ' ':
		return keyEvent{k: kUp, hasKey: true}, true
	case 'q', 'Q', 27:
		return keyEvent{special: spQuit}, true
	case 'p', 'P':
		return keyEvent{special: spPause}, true
	case 'r', 'R':
		return keyEvent{special: spRestart}, true
	case '\r', '\n':
		return keyEvent{special: spAny}, true
	}
	return keyEvent{special: spAny}, true
}
