package main

// Calibration warm-start tests: the SSH host remembers, per client host
// and in memory only, what a repeat player's terminal taught the mapper
// (input/feel_test.go TestCalibrationSurvivesRestart models the mapper
// side of the contract). The observable property: a reconnecting player's
// FIRST hold of a movement key survives the OS repeat delay instead of
// stuttering for ~0.5s — the cold-mapper cost every connect paid before.

import (
	"fmt"
	"testing"

	"github.com/Daviey/mario/input"
)

// teachRightHold walks a mapper through one full legacy hold of 'd' with
// a `delay`-tick OS repeat delay: press, silent expiry, resumed press
// (the first repeat stages the delay), a confirming repeat (the mapper
// adopts it and the hold habit closes at the second expiry).
func teachRightHold(m *input.Mapper, delay int) {
	m.Feed([]byte{'d'})
	for range delay + 2 { // silent through the repeat delay: press expires
		m.Poll()
	}
	m.Feed([]byte{'d'}) // first OS repeat after the gap
	m.Poll()
	m.Feed([]byte{'d'}) // confirming repeat: delay adopted, cadence measured
	m.Poll()
	for range 60 { // hold ends, habits close
		m.Poll()
	}
}

func TestSessionMapperWarmStartKillsColdStutter(t *testing.T) {
	cals := &calCache{}
	m1, save1 := sessionMapper(cals, "203.0.113.7:51234")
	teachRightHold(m1, 20)
	save1()

	// Same host reconnects (new port, same calKey): the first hold of
	// the very next session must ride the learned grace all the way
	// through the repeat delay.
	m2, _ := sessionMapper(cals, "203.0.113.7:9999")
	m2.Feed([]byte{'d'})
	heldFor := 0
	for range 30 {
		if !m2.Poll().Right {
			break
		}
		heldFor++
	}
	if heldFor < 22 { // measured delay (22 after teach) + grace
		t.Errorf("warm-started first hold died after %d ticks — cold stutter survived the warm start", heldFor)
	}

	// A different host is still cold: an unmeasured first press must
	// stay bounded by the plain hold window (taps must not overrun).
	m3, _ := sessionMapper(cals, "198.51.100.4:1111")
	m3.Feed([]byte{'d'})
	coldHeld := 0
	for range 30 {
		if !m3.Poll().Right {
			break
		}
		coldHeld++
	}
	if coldHeld > 14 { // holdWindow (12) + slack
		t.Errorf("cold first hold lived %d ticks; unmeasured grace must stay short", coldHeld)
	}
}

func TestCalCacheRoundtripAndIdleGuard(t *testing.T) {
	c := &calCache{}
	if _, ok := c.get("h"); ok {
		t.Fatal("empty cache returned an entry")
	}
	c.put("h", input.Calibration{OSDelay: 30})
	if got, ok := c.get("h"); !ok || got.OSDelay != 30 {
		t.Fatalf("get after put = %+v, %v", got, ok)
	}
	// A session that learned nothing (idle connect) must not clobber a
	// repeat player's good entry.
	c.put("h", input.Calibration{})
	if got, _ := c.get("h"); got.OSDelay != 30 {
		t.Fatalf("idle put clobbered the entry: %+v", got)
	}
}

func TestCalCacheCapClears(t *testing.T) {
	c := &calCache{}
	for i := range calCacheMax {
		c.put(fmt.Sprintf("h%d", i), input.Calibration{OSDelay: i + 1})
	}
	if got, ok := c.get("h0"); !ok || got.OSDelay != 1 {
		t.Fatalf("entry missing before cap: %+v %v", got, ok)
	}
	c.put("hnew", input.Calibration{OSDelay: 9}) // over cap: drop wholesale
	if _, ok := c.get("h0"); ok {
		t.Fatal("cache over cap kept old entries")
	}
	if got, ok := c.get("hnew"); !ok || got.OSDelay != 9 {
		t.Fatalf("post-cap entry missing: %+v %v", got, ok)
	}
}

func TestCalKeyDropsPort(t *testing.T) {
	if k := calKey("203.0.113.7:51234"); k != "203.0.113.7" {
		t.Fatalf("calKey = %q, want host only", k)
	}
	if k := calKey("garbage"); k != "garbage" {
		t.Fatalf("calKey fallback = %q", k)
	}
}
