package input

// The mapper's hold model: the repeat-inference/demotion half of input
// mapping, split from the pure decode half in input.go (parseSeq/
// csiEvent/mappedKey). Decoded keyEvents are folded in by apply; tick
// runs the per-tick demotion and silent-expiry sweeps; held/live/
// liveOwn/demotedHeld answer the per-tick hold queries. The model is
// embedded in Mapper and every method runs under the Mapper's mutex.
//
// The demotion machinery (demotedHeld, the resurrection in apply, the
// upExtendTicks cap) exists to keep proven holds alive through real
// Wayland/tmux single-key-repeat behavior — do not "simplify" it away;
// it is pinned by demote_test.go + feel_test.go.

// holdWindow is the key-repeat silence (~0.2s) after which a legacy key
// expires. It is the floor every calibrated grace and cadence window is
// clamped to (clampWindow).
const holdWindow = 12

// maxHoldWindow is the upper bound (~0.9s) for calibrated grace and
// cadence windows: no learned terminal timing may keep a key live
// longer than this after its last byte.
const maxHoldWindow = 52

// defaultOSDelay is the assumed keydown→repeat delay on a terminal whose
// real delay has not been measured yet (500-660ms covers the common
// GNOME/niri/macOS/Windows defaults).
const defaultOSDelay = 36

// upExtendTicks bounds the demotion extension for the jump key: past the
// full rise of a jump a held jump key no longer changes physics (the jump
// cut is over), and it must be expired again well before landing (~2x the
// rise) so the retap edge still exists.
const upExtendTicks = 24 // rise ≈ -engine.JumpVel/engine.Gravity ≈ 20.5 ticks; slack after, still < the ≈41-tick landing

// resurrectWindow is how long after a demoted hold finally expires a byte
// for another key may resurrect it: while a direction is held and jump is
// tapped, the direction is dead for roughly the airborne time (~41 ticks)
// between taps; past this window, silence means released.
const resurrectWindow = 2*maxHoldWindow - 2*holdWindow // 80 ticks ≈ 1.3s

// keyState is one mapped key's hold lifecycle. The per-key invariants
// (the owning transition named in parens; the prose lives in tick's
// sweeps, demotedHeld and apply):
//   - live → demoted ⇒ wasLive: the demotion sweep only demotes a
//     proven hold, and the expiry sweep that follows it in the same
//     tick marks it wasLive (resurrection in apply sets both too).
//   - deadAt is set exactly at the live→dead transition of a DEMOTED
//     hold (tick's silent-expiry sweep) and cleared by the three paths
//     that settle the release question: an explicit release (apply),
//     resurrection (apply) and ReleaseAll.
//   - win is meaningful only while lastSeen ≠ 0: a fresh press
//     re-derives it (apply) before lastSeen is stamped, and ReleaseAll
//     clears both together.
type keyState struct {
	lastSeen  int  // tick of the key's most recent byte; 0 = never seen
	pressedAt int  // tick of the newest PRESS (repeats don't refresh it)
	sticky    bool // kitty press held until its explicit release byte
	latched   bool // press edges awaiting their first Poll
	win       int  // per-key expiry window; 0 = holdWindow
	sawRepeat bool // this keypress received repeat bytes
	heldHabit bool // last keypress of this key was a hold (repeats seen)
	demoted   bool // proven hold silenced by a newer key press (see demotedHeld)
	wasLive   bool // live at the previous Poll; drives one-shot expiry
	deadAt    int  // tick a demoted hold finally expired; 0 = none
}

// holdModel is the mapper's per-key hold lifecycle state: the legacy
// repeat inference (windows, habits, the measured OS delay), the
// single-key-repeat demotion markers, and the kitty source map for
// exact releases. It is embedded in Mapper; the mutex guarding it lives
// there.
type holdModel struct {
	now          int                // tick clock, advanced by tick; apply stamps against it
	ks           [keyCount]keyState // per-key hold lifecycle (see keyState)
	osDelay      int                // measured keydown→first-repeat delay, in ticks (0 = uncalibrated)
	pendingDelay int                // delay candidate from the newest resumed keypress
	sources      map[int]key        // kitty press sources still held, for exact releases
	lastByte     int                // tick of the most recent decoded event, any key
}

// tick advances the hold-model clock and runs the two per-Poll sweeps.
func (h *holdModel) tick() {
	h.now++
	// Demotion sweep: a proven hold whose own window just lapsed while
	// another key is demonstrably live was silenced by the terminal, not
	// released — single-key-repeat compositors give the repeat stream to
	// the newest key and never resume it for older ones. Such a hold stays
	// live (see demotedHeld) instead of expiring on its own cadence.
	for k := key(0); k < keyCount; k++ {
		ks := &h.ks[k]
		if ks.demoted || ks.sticky || ks.lastSeen == 0 || !ks.sawRepeat {
			continue
		}
		if h.liveOwn(k) || !h.anyOtherLiveOwn(k) {
			continue
		}
		ks.demoted = true
	}
	// Silent expiry closes a keypress exactly once (live → dead): whether
	// it ends as "this key is usually held" is exactly whether repeats
	// arrived while it was live. That habit decides the grace the NEXT
	// keydown of that key gets. A hold that dies while demoted leaves a
	// resurrection marker: its release was never confirmed, so a later
	// press of another key can reopen the question (see apply).
	for k := key(0); k < keyCount; k++ {
		ks := &h.ks[k]
		if h.live(k) {
			ks.wasLive = true
			continue
		}
		if !ks.wasLive {
			continue
		}
		ks.wasLive = false
		ks.heldHabit = ks.sawRepeat
		ks.deadAt = 0
		if ks.demoted {
			ks.deadAt = h.now
		}
		ks.sawRepeat = false
		ks.demoted = false
	}
}

func (h *holdModel) held(k key) bool {
	return h.ks[k].latched || h.live(k)
}

// live reports the key held right now, ignoring the one-tick press latch:
// either by its own byte evidence, or — for holds demoted by a newer key
// press — by the demotion extension below.
func (h *holdModel) live(k key) bool {
	return h.liveOwn(k) || h.demotedHeld(k)
}

// liveOwn reports the key held by its own byte evidence alone: a kitty
// press/repeat still sticky, or a legacy stream whose repeats keep
// lastSeen inside its expiry window.
func (h *holdModel) liveOwn(k key) bool {
	ks := &h.ks[k]
	if ks.sticky {
		return true
	}
	if ks.lastSeen == 0 {
		return false
	}
	w := ks.win
	if w == 0 {
		w = holdWindow
	}
	return h.now-ks.lastSeen <= w
}

// demotedHeld keeps a proven hold alive after the terminal silenced its
// repeat stream in favour of a newer key. While any other key is still
// demonstrably live (its own bytes arriving), the older hold stands: the
// "hold right and jump" combo must not drop the direction mid-jump. When
// the whole keyboard goes quiet it lingers only for the hold grace (the
// same trust a fresh keydown of a proven-hold key gets), then expires.
// The jump key is capped at upExtendTicks so a demoted Up can never eat
// the retap edge of the next jump.
func (h *holdModel) demotedHeld(k key) bool {
	ks := &h.ks[k]
	if !ks.demoted {
		return false
	}
	if k == kUp {
		return h.now-ks.lastSeen <= upExtendTicks
	}
	if h.anyOtherLiveOwn(k) {
		return true
	}
	return h.now-h.lastByte <= h.graceFor(k)
}

// anyOtherLiveOwn reports whether some other key is held by its own byte
// evidence right now.
func (h *holdModel) anyOtherLiveOwn(k key) bool {
	for o := key(0); o < keyCount; o++ {
		if o != k && h.liveOwn(o) {
			return true
		}
	}
	return false
}

// Calibration is the mapper's learned terminal timing: the measured OS
// key-repeat delay and the per-key hold habits. It is never stored on
// the player's machine — the keys.json-era disk store is gone, a
// deliberate privacy rule; the one keeper is the SSH host's in-memory
// per-client-host cache (calCache in cmd/mario/serve.go), which warm-
// starts a reconnecting player. Without warm-starting, the first hold
// of each key stalls for the repeat delay every single session, and a
// player who lets go during the stall never gets smooth holds at all.
type Calibration struct {
	OSDelay   int    `json:"os_delay"`   // ticks; 0 = unmeasured
	HeldHabit []bool `json:"held_habit"` // per key: last press was a hold
}

// calibration snapshots the learned state (see Mapper.Calibration).
func (h *holdModel) calibration() Calibration {
	var habit [keyCount]bool
	for k := range h.ks {
		habit[k] = h.ks[k].heldHabit
	}
	return Calibration{
		OSDelay:   h.osDelay,
		HeldHabit: habit[:],
	}
}

// applyCalibration restores learned state (see Mapper.ApplyCalibration).
// Values are clamped to sane bounds and mismatched key arrays are ignored.
func (h *holdModel) applyCalibration(c Calibration) {
	if c.OSDelay < 0 {
		c.OSDelay = 0
	}
	if c.OSDelay > maxHoldWindow {
		c.OSDelay = maxHoldWindow
	}
	h.osDelay = c.OSDelay
	if len(c.HeldHabit) == int(keyCount) {
		for k, hh := range c.HeldHabit {
			h.ks[k].heldHabit = hh
		}
	}
}

// releaseAll drops every held key and pending press edge, keeping the
// learned calibration (the Mapper.ReleaseAll contract: input has stopped
// reaching the mapper mid-hold).
func (h *holdModel) releaseAll() {
	// Settle the in-flight hold learning first, mirroring the silent-
	// expiry sweep: a key whose keypress showed repeats was held, so
	// the NEXT press of it deserves the hold grace — the sweep never
	// sees this live→dead transition, because ReleaseAll is exactly the
	// case where input stops reaching the mapper mid-hold.
	var habit [keyCount]bool
	for k := range h.ks {
		habit[k] = h.ks[k].heldHabit || h.ks[k].sawRepeat
	}
	// Whole-struct zero of every per-key field (holds, edges, windows,
	// markers) — then restore: the settled hold habits are the one
	// thing that survives ReleaseAll.
	h.ks = [keyCount]keyState{}
	for k := range h.ks {
		h.ks[k].heldHabit = habit[k]
	}
	h.sources = make(map[int]key)
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
func (h *holdModel) graceFor(k key) int {
	if k == kUp {
		return holdWindow
	}
	// Only keys proven to be held get the repeat-delay-covering grace —
	// taps must not overrun after release. An unmeasured delay falls
	// back to the common default so a learned hold habit still covers
	// the delay even if the machine never produced a measurable gap.
	if !h.ks[k].heldHabit {
		return holdWindow
	}
	d := h.osDelay
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

// apply folds one decoded event into the hold model and returns the
// edge-triggered special it carries (spNone when it carries none) for
// the mapper to latch.
func (h *holdModel) apply(ev keyEvent) int {
	h.lastByte = h.now + 1
	if ev.evType == 3 { // release
		if !ev.hasKey || ev.k >= keyCount {
			return spNone
		}
		// Clear the key only when its last held source was released:
		// arrow-right and 'd' both mean Right, and releasing one must
		// not drop the other.
		if ev.src != 0 {
			if _, ok := h.sources[ev.src]; ok {
				delete(h.sources, ev.src)
				for _, k := range h.sources {
					if k == ev.k {
						return spNone // another source still holds it
					}
				}
			}
		}
		ks := &h.ks[ev.k]
		ks.sticky = false
		ks.lastSeen = 0
		ks.demoted = false
		ks.deadAt = 0
		return spNone
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
			dk := &h.ks[k]
			if k == ev.k || k == kUp || dk.deadAt == 0 {
				continue
			}
			if h.now+1-dk.deadAt <= resurrectWindow {
				dk.demoted = true
				dk.deadAt = 0
				dk.wasLive = true
			}
		}
		ks := &h.ks[ev.k]
		if ev.evType >= 1 {
			ks.sticky = true
			if ev.src != 0 {
				h.sources[ev.src] = ev.k
			}
		}
		if ev.evType != 2 { // repeats don't refresh press recency
			ks.pressedAt = h.now + 1
		}
		if h.liveOwn(ev.k) {
			// Continuing byte of a live keypress (an OS repeat): the
			// press is proven a hold, and silence should now expire it
			// on the observed cadence.
			if !ks.sawRepeat {
				ks.sawRepeat = true
				// First repeat of a resumed press carries the measured
				// keydown→repeat delay for the whole terminal.
				if h.pendingDelay > holdWindow && h.pendingDelay > h.osDelay {
					h.osDelay = h.pendingDelay
				}
			}
			if prev := ks.lastSeen; prev > 0 {
				if d := h.now - prev + 1; d > 0 {
					w := 3 * d
					if w < 3 {
						w = 3
					}
					if w > maxHoldWindow {
						w = maxHoldWindow
					}
					ks.win = w
				}
			}
		} else if ks.demoted {
			// Own bytes resumed after demotion: either the terminal
			// handed the repeat stream back or the player re-pressed.
			// The hold was never disproven, so keep its repeat status,
			// but don't measure a cadence across the demotion gap.
			ks.demoted = false
			ks.deadAt = 0
			ks.win = h.graceFor(ev.k)
		} else {
			// Fresh or resumed keypress. A resumed one (byte after the
			// previous press expired) is either the first OS repeat of a
			// hold or a quick retap — indistinguishable, so only stage
			// the gap as a delay candidate; it is adopted when repeats
			// actually follow.
			if prev := ks.lastSeen; prev > 0 {
				h.pendingDelay = h.now - prev + 1
				// If the gap is short, this is just a jittery repeat that
				// barely missed a collapsed cadence window. Keep the hold's
				// repeat status intact so the habit saves correctly.
				if h.pendingDelay > holdWindow {
					ks.sawRepeat = false
				}
			} else {
				ks.sawRepeat = false
			}
			ks.win = h.graceFor(ev.k)
		}
		// Latch the press edge: if a frame hitch lets the release land
		// before the next Poll, the press must still be visible once.
		ks.latched = true
		ks.lastSeen = h.now + 1
	}
	if ev.evType == 0 || ev.evType == 1 { // edges fire on press only
		return ev.special
	}
	return spNone
}
