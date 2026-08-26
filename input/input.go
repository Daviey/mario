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
	spAny
)

const holdWindow = 12    // ~0.2s of key-repeat silence before a legacy key expires
const maxHoldWindow = 52 // upper bound for calibrated grace and cadence windows (~0.9s)
// defaultOSDelay is the assumed keydown→repeat delay on a terminal whose
// real delay has not been measured yet (500-660ms covers the common
// GNOME/niri/macOS/Windows defaults).
const defaultOSDelay = 36
const staleSeqPolls = 3 // quiet polls before an incomplete escape sequence is dropped
// upExtendTicks bounds the demotion extension for the jump key: past the
// full rise of a jump a held jump key no longer changes physics (the jump
// cut is over), and it must be expired again well before landing (~2x the
// rise) so the retap edge still exists.
const upExtendTicks = 24 // full jump rise ≈ -JumpVel/Gravity ≈ 20.5 ticks; slack after, still < landing (~41)
// resurrectWindow is how long after a demoted hold finally expires a byte
// for another key may resurrect it: while a direction is held and jump is
// tapped, the direction is dead for roughly the airborne time (~41 ticks)
// between taps; past this window, silence means released.
const resurrectWindow = 2*maxHoldWindow - 2*holdWindow // 80 ticks ≈ 1.3s

type keyEvent struct {
	k       key
	hasKey  bool
	special int
	evType  int // 0 legacy press, 1 kitty press, 2 kitty repeat, 3 release
	src     int // kitty source id: codepoint, or 0x10000|key-final; 0 = untracked
}

// Mapper converts a byte stream into per-tick engine.Input values.
type Mapper struct {
	mu           sync.Mutex
	now          int
	lastSeen     [keyCount]int
	pressedAt    [keyCount]int // tick of the newest PRESS (repeats don't refresh it)
	sticky       [keyCount]bool
	latched      [keyCount]bool // press edges awaiting their first Poll
	win          [keyCount]int  // per-key expiry window; 0 = holdWindow
	sawRepeat    [keyCount]bool // this keypress received repeat bytes
	heldHabit    [keyCount]bool // last keypress of this key was a hold (repeats seen)
	osDelay      int            // measured keydown→first-repeat delay, in ticks (0 = uncalibrated)
	pendingDelay int            // delay candidate from the newest resumed keypress
	sources      map[int]key    // kitty press sources still held, for exact releases
	demoted      [keyCount]bool // proven hold silenced by a newer key press (see demotedHeld)
	wasLive      [keyCount]bool // live at the previous Poll; drives one-shot expiry
	deadAt       [keyCount]int  // tick a demoted hold finally expired; 0 = none
	lastByte     int            // tick of the most recent decoded event, any key
	buf          []byte
	feedAge      int // polls since the last Feed delivered bytes
	pendQuit     bool
	pendPause    bool
	pendRestart  bool
	pendAny      bool
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
	// Demotion sweep: a proven hold whose own window just lapsed while
	// another key is demonstrably live was silenced by the terminal, not
	// released — single-key-repeat compositors give the repeat stream to
	// the newest key and never resume it for older ones. Such a hold stays
	// live (see demotedHeld) instead of expiring on its own cadence.
	for k := key(0); k < keyCount; k++ {
		if m.demoted[k] || m.sticky[k] || m.lastSeen[k] == 0 || !m.sawRepeat[k] {
			continue
		}
		if m.liveOwn(k) || !m.anyOtherLiveOwn(k) {
			continue
		}
		m.demoted[k] = true
	}
	// Silent expiry closes a keypress exactly once (live → dead): whether
	// it ends as "this key is usually held" is exactly whether repeats
	// arrived while it was live. That habit decides the grace the NEXT
	// keydown of that key gets. A hold that dies while demoted leaves a
	// resurrection marker: its release was never confirmed, so a later
	// press of another key can reopen the question (see apply).
	for k := key(0); k < keyCount; k++ {
		if m.live(k) {
			m.wasLive[k] = true
			continue
		}
		if !m.wasLive[k] {
			continue
		}
		m.wasLive[k] = false
		m.heldHabit[k] = m.sawRepeat[k]
		m.deadAt[k] = 0
		if m.demoted[k] {
			m.deadAt[k] = m.now
		}
		m.sawRepeat[k] = false
		m.demoted[k] = false
	}
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
	m.latched = [keyCount]bool{} // each press edge is visible to exactly one Poll
	return in
}

func (m *Mapper) held(k key) bool {
	return m.latched[k] || m.live(k)
}

// live reports the key held right now, ignoring the one-tick press latch:
// either by its own byte evidence, or — for holds demoted by a newer key
// press — by the demotion extension below.
func (m *Mapper) live(k key) bool {
	return m.liveOwn(k) || m.demotedHeld(k)
}

// liveOwn reports the key held by its own byte evidence alone: a kitty
// press/repeat still sticky, or a legacy stream whose repeats keep
// lastSeen inside its expiry window.
func (m *Mapper) liveOwn(k key) bool {
	if m.sticky[k] {
		return true
	}
	ls := m.lastSeen[k]
	if ls == 0 {
		return false
	}
	w := m.win[k]
	if w == 0 {
		w = holdWindow
	}
	return m.now-ls <= w
}

// demotedHeld keeps a proven hold alive after the terminal silenced its
// repeat stream in favour of a newer key. While any other key is still
// demonstrably live (its own bytes arriving), the older hold stands: the
// "hold right and jump" combo must not drop the direction mid-jump. When
// the whole keyboard goes quiet it lingers only for the hold grace (the
// same trust a fresh keydown of a proven-hold key gets), then expires.
// The jump key is capped at upExtendTicks so a demoted Up can never eat
// the retap edge of the next jump.
func (m *Mapper) demotedHeld(k key) bool {
	if !m.demoted[k] {
		return false
	}
	if k == kUp {
		return m.now-m.lastSeen[k] <= upExtendTicks
	}
	if m.anyOtherLiveOwn(k) {
		return true
	}
	return m.now-m.lastByte <= m.graceFor(k)
}

// anyOtherLiveOwn reports whether some other key is held by its own byte
// evidence right now.
func (m *Mapper) anyOtherLiveOwn(k key) bool {
	for o := key(0); o < keyCount; o++ {
		if o != k && m.liveOwn(o) {
			return true
		}
	}
	return false
}

// Calibration is the mapper's learned terminal timing: the measured OS
// key-repeat delay and the per-key hold habits. The runner persists it
// across runs so a fresh process doesn't forget everything — otherwise the
// first hold of each key stalls for the repeat delay every single session,
// and a player who lets go during the stall never gets smooth holds at all.
type Calibration struct {
	OSDelay   int    `json:"os_delay"`   // ticks; 0 = unmeasured
	HeldHabit []bool `json:"held_habit"` // per key: last press was a hold
}

// Calibration returns the learned state, safe to persist and hand to a
// future mapper via ApplyCalibration.
func (m *Mapper) Calibration() Calibration {
	m.mu.Lock()
	defer m.mu.Unlock()
	habit := m.heldHabit
	return Calibration{
		OSDelay:   m.osDelay,
		HeldHabit: habit[:],
	}
}

// ApplyCalibration restores persisted learning. Values are clamped to
// sane bounds and mismatched key arrays are ignored.
func (m *Mapper) ApplyCalibration(c Calibration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c.OSDelay < 0 {
		c.OSDelay = 0
	}
	if c.OSDelay > maxHoldWindow {
		c.OSDelay = maxHoldWindow
	}
	m.osDelay = c.OSDelay
	if len(c.HeldHabit) == int(keyCount) {
		m.heldHabit = [keyCount]bool(c.HeldHabit)
	}
}

// graceFor returns the silence window a FRESH keydown of k gets before it
// is considered released. On keydown-only terminals a tap and the start of
// a hold are byte-identical for the OS repeat delay (~500-600ms): any window
// that covers the delay overruns taps, any shorter one stutters holds. The
// escape: calibrate the delay once from a real hold (osDelay), remember
// per key whether its last press was held (heldHabit), and give held keys
// a grace covering the delay. Repeats collapse the window onto the
// measured cadence, so releasing still stops fast. Jump (kUp) never gets
// the long grace — a phantom Up eats retap edges, the missed-jump bug.
func (m *Mapper) graceFor(k key) int {
	if k == kUp {
		return holdWindow
	}
	// Only keys proven to be held get the repeat-delay-covering grace —
	// taps must not overrun after release. An unmeasured delay falls
	// back to the common default so a learned hold habit still covers
	// the delay even if the machine never produced a measurable gap.
	if !m.heldHabit[k] {
		return holdWindow
	}
	d := m.osDelay
	if d == 0 {
		d = defaultOSDelay
	}
	return clampWindow(d + 2)
}

func clampWindow(w int) int {
	if w < holdWindow {
		return holdWindow
	}
	if w > maxHoldWindow {
		return maxHoldWindow
	}
	return w
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
	m.lastByte = m.now + 1
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
		m.demoted[ev.k] = false
		m.deadAt[ev.k] = 0
		return
	}
	if ev.hasKey && ev.k < keyCount {
		// Resurrection: a hold that expired while demoted was never
		// confirmed released — on a single-key-repeat terminal its stream
		// simply never comes back. A byte for another key within the
		// resurrection window reopens the question and stands the hold
		// back up (the jump key never resurrects: a phantom Up eats
		// retap edges).
		// (releases never reach this block: they return above)
		for k := key(0); k < keyCount; k++ {
			if k == ev.k || k == kUp || m.deadAt[k] == 0 {
				continue
			}
			if m.now+1-m.deadAt[k] <= resurrectWindow {
				m.demoted[k] = true
				m.deadAt[k] = 0
				m.wasLive[k] = true
			}
		}
		if ev.evType >= 1 {
			m.sticky[ev.k] = true
			if ev.src != 0 {
				m.sources[ev.src] = ev.k
			}
		}
		if ev.evType != 2 { // repeats don't refresh press recency
			m.pressedAt[ev.k] = m.now + 1
		}
		if m.liveOwn(ev.k) {
			// Continuing byte of a live keypress (an OS repeat): the
			// press is proven a hold, and silence should now expire it
			// on the observed cadence.
			if !m.sawRepeat[ev.k] {
				m.sawRepeat[ev.k] = true
				// First repeat of a resumed press carries the measured
				// keydown→repeat delay for the whole terminal.
				if m.pendingDelay > holdWindow && m.pendingDelay > m.osDelay {
					m.osDelay = m.pendingDelay
				}
			}
			if prev := m.lastSeen[ev.k]; prev > 0 {
				if d := m.now - prev + 1; d > 0 {
					w := 3 * d
					if w < 3 {
						w = 3
					}
					if w > maxHoldWindow {
						w = maxHoldWindow
					}
					m.win[ev.k] = w
				}
			}
		} else if m.demoted[ev.k] {
			// Own bytes resumed after demotion: either the terminal
			// handed the repeat stream back or the player re-pressed.
			// The hold was never disproven, so keep its repeat status,
			// but don't measure a cadence across the demotion gap.
			m.demoted[ev.k] = false
			m.deadAt[ev.k] = 0
			m.win[ev.k] = m.graceFor(ev.k)
		} else {
			// Fresh or resumed keypress. A resumed one (byte after the
			// previous press expired) is either the first OS repeat of a
			// hold or a quick retap — indistinguishable, so only stage
			// the gap as a delay candidate; it is adopted when repeats
			// actually follow.
			if prev := m.lastSeen[ev.k]; prev > 0 {
				m.pendingDelay = m.now - prev + 1
				// If the gap is short, this is just a jittery repeat that
				// barely missed a collapsed cadence window. Keep the hold's
				// repeat status intact so the habit saves correctly.
				if m.pendingDelay > holdWindow {
					m.sawRepeat[ev.k] = false
				}
			} else {
				m.sawRepeat[ev.k] = false
			}
			m.win[ev.k] = m.graceFor(ev.k)
		}
		// Latch the press edge: if a frame hitch lets the release land
		// before the next Poll, the press must still be visible once.
		m.latched[ev.k] = true
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
	case 'l', 'L':
		// Leaderboard key: a UI trigger (title screen 'l' opens the
		// board via ui.UI.Note). Deliberately a mapped no-op so its
		// AnyKey edge can never start the game — gameio routes the raw
		// byte to the UI before the mapper sees it.
		return keyEvent{}, true
	}
	return keyEvent{special: spAny}, true
}
