// Package input maps raw terminal bytes to engine.Input.
//
//   - Legacy: plain keydown bytes only (no release events). Held keys are
//     inferred from OS key-repeat: a key counts as held while repeats keep
//     arriving, and expires after holdWindow ticks of silence. One wrinkle:
//     single-key-repeat terminals (every Wayland compositor — and tmux
//     passthrough lands the game in this regime) silence the older key's
//     repeat stream the instant a newer key is pressed, so a proven hold
//     must also survive its stream going quiet while other keys are live
//     (see live/demotedHeld).
//   - Kitty keyboard protocol: CSI-u sequences with explicit press/repeat/
//     release events. Releases clear the key immediately. The runner pushes
//     flags 1|2|8 so every key — including plain letters and space —
//     reports release events; the legacy regime only remains for terminals
//     without the protocol.
//
// This file is the decode half (parseSeq/csiEvent/mappedKey and the byte
// buffer they feed from); the hold half — repeat inference, demotion and
// the learned calibration — is the holdModel in holds.go, embedded in
// Mapper below.
package input

import (
	"strconv"
	"strings"
	"sync"

	"github.com/Daviey/mario/engine"
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
	spKill
	spAny
)

// staleSeqPolls is the number of quiet polls an incomplete escape
// sequence may sit in the buffer before it is dropped (a truncated
// sequence would otherwise swallow every later key).
const staleSeqPolls = 3

type keyEvent struct {
	k       key
	hasKey  bool
	special int
	evType  int  // 0 legacy press, 1 kitty press, 2 kitty repeat, 3 release
	src     int  // kitty source id: codepoint, or 0x10000|key-final; 0 = untracked
	csi     bool // arrived as a CSI sequence (not SS3 or a plain byte)
	kittyU  bool // CSI-u final: only the kitty protocol ever sends these
}

// Mapper converts a byte stream into per-tick engine.Input values.
type Mapper struct {
	mu sync.Mutex

	// holdModel carries the per-key hold lifecycle state and its
	// queries (see holds.go); embedded so the state reads as mapper
	// state. Guarded by mu.
	holdModel

	buf      []byte
	feedAge  int  // polls since the last Feed delivered bytes
	sawKitty bool // kitty protocol detected (CSI-u final or explicit event type)

	pendQuit    bool
	pendPause   bool
	pendRestart bool
	pendKill    bool
	pendAny     bool
}

// NewMapper returns a mapper with no keys held.
func NewMapper() *Mapper {
	return &Mapper{holdModel: holdModel{sources: make(map[int]key)}}
}

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
	m.feedAge++
	m.tick() // the hold-model clock advance + demotion/expiry sweeps (holds.go)
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
		if m.ks[kLeft].pressedAt > m.ks[kRight].pressedAt {
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
		Suicide: m.pendKill,
		AnyKey:  m.pendAny,
	}
	m.pendQuit, m.pendPause, m.pendRestart, m.pendKill, m.pendAny = false, false, false, false, false
	for k := range m.ks {
		m.ks[k].latched = false // each press edge is visible to exactly one Poll
	}
	return in
}

// Calibration returns the learned state, safe to persist and hand to a
// future mapper via ApplyCalibration.
func (m *Mapper) Calibration() Calibration {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calibration()
}

// SawKitty reports whether the input stream has spoken the kitty keyboard
// protocol (a CSI-u final or an explicit press/repeat/release event type) —
// the session's input regime, surfaced for play-context logging.
func (m *Mapper) SawKitty() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.sawKitty
}

// ReleaseAll drops every held key and pending press edge, keeping the
// learned calibration. Call it whenever input stops reaching the mapper —
// while a leaderboard screen captures the keyboard, releases are routed to
// the UI alone, so any hold from the dying run would leak into the next
// one (a held Right at game over runs the restarted game on).
func (m *Mapper) ReleaseAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.releaseAll()
}

// ApplyCalibration restores learning — the SSH host's in-memory
// per-client-host cache hands a reconnecting player their previous
// session's timings (nothing is ever stored on the player's machine).
// Values are clamped to sane bounds and mismatched key arrays are ignored.
func (m *Mapper) ApplyCalibration(c Calibration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.applyCalibration(c)
}

func (m *Mapper) drain() {
	for len(m.buf) > 0 {
		n, ev, ok := parseSeq(m.buf)
		if !ok {
			break // incomplete escape sequence; wait for more bytes
		}
		m.buf = m.buf[n:]
		// Kitty-regime detection: a CSI-u final or any explicit event
		// type is unambiguous kitty — legacy terminals send neither.
		// Once the regime is known, a typeless CSI press (ghostty and
		// kitty omit ":1" — press is the default event type) is treated
		// as the press it is: held until its explicit release, never
		// subject to OS-repeat inference. Without this every first hold
		// stutters for the repeat delay (~0.5s) and taps overrun by the
		// calibrated grace — the exact "lag and run-on" feel.
		if ev.kittyU || ev.evType >= 1 {
			m.sawKitty = true
		}
		if ev.evType == 0 && ev.csi && m.sawKitty {
			ev.evType = 1
		}
		m.latchSpecial(m.apply(ev))
	}
}

// latchSpecial records an edge-triggered special key for the next Poll.
func (m *Mapper) latchSpecial(sp int) {
	switch sp {
	case spQuit:
		m.pendQuit = true
	case spPause:
		m.pendPause = true
	case spRestart:
		m.pendRestart = true
	case spKill:
		m.pendKill = true
	case spAny:
		m.pendAny = true
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
	csi := keyEvent{csi: true}
	switch final {
	case 'A':
		return keyEvent{k: kUp, hasKey: true, evType: evType, src: 0x10000 | 'A', csi: true}
	case 'B':
		return keyEvent{k: kDown, hasKey: true, evType: evType, src: 0x10000 | 'B', csi: true}
	case 'C':
		return keyEvent{k: kRight, hasKey: true, evType: evType, src: 0x10000 | 'C', csi: true}
	case 'D':
		return keyEvent{k: kLeft, hasKey: true, evType: evType, src: 0x10000 | 'D', csi: true}
	case 'u':
		code := 0
		if len(parts) > 0 {
			sub := strings.Split(parts[0], ":")
			code, _ = strconv.Atoi(sub[0])
		}
		// Ctrl+C via the kitty protocol: CSI 99;5:<event> u (ctrl = mod bit 2).
		if code == 'c' && csiCtrlHeld(parts) {
			return keyEvent{special: spQuit, evType: evType, csi: true, kittyU: true}
		}
		if ev, ok := mappedKey(code); ok {
			ev.evType = evType
			ev.src = code
			ev.csi = true
			ev.kittyU = true
			return ev
		}
		csi.kittyU = true
		if evType == 0 || evType == 1 {
			csi.special = spAny
		}
		csi.evType = evType
		return csi
	case '~':
		return csi // edit/function keys: consume silently
	default:
		return csi // cursor reports, unknown sequences: consume silently
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
	case 'k', 'K':
		// Suicide key: dying on demand when trapped beats waiting out
		// the clock. A mapped game key (not AnyKey), so pressing it on
		// the title screen cannot start a run.
		return keyEvent{special: spKill}, true
	case '\r', '\n':
		return keyEvent{special: spAny}, true
	case 'l', 'L':
		// Leaderboard key: a UI trigger (title screen 'l' opens the
		// board via ui.UI.Note). Deliberately a mapped no-op so its
		// AnyKey edge can never start the game — gameio routes the raw
		// byte to the UI before the mapper sees it.
		return keyEvent{}, true
	case 'i', 'I':
		// About key, same treatment as 'l': title screen 'i' opens the
		// about screen, and the no-op mapping keeps its AnyKey edge from
		// ever starting the game.
		return keyEvent{}, true
	}
	return keyEvent{special: spAny}, true
}
