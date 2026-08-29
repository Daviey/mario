package main

import (
	"bytes"
	"testing"
	"time"
)

func TestBellSkipsConstantEvents(t *testing.T) {
	// jump/pause/tick would ring constantly during normal play.
	for _, ev := range []string{"jump", "tick", "pause"} {
		var buf bytes.Buffer
		b := newBell(&buf)
		b.ring(ev)
		b.ring(ev)
		if buf.Len() != 0 {
			t.Errorf("event %q rang the bell: %q", ev, buf.Bytes())
		}
	}
}

func TestBellRingsForGameplayEvents(t *testing.T) {
	for _, ev := range []string{"coin", "stomp", "kick", "bump", "brick", "fire"} {
		var buf bytes.Buffer
		b := newBell(&buf)
		b.ring(ev)
		if !bytes.Equal(buf.Bytes(), []byte{7}) {
			t.Errorf("event %q: output %q, want one BEL", ev, buf.Bytes())
		}
	}
}

func TestBellThrottlesCoinCascades(t *testing.T) {
	// A coin burst emits one event per tick; within one throttle window
	// only the first may ring (7 ticks ≈ 116ms < bellMinGap).
	var buf bytes.Buffer
	b := newBell(&buf)
	now := time.Now()
	b.now = func() time.Time { return now }
	for range 7 {
		b.ring("coin")
		now = now.Add(16 * time.Millisecond)
	}
	if n := bytes.Count(buf.Bytes(), []byte{7}); n != 1 {
		t.Errorf("coin cascade rang %d times in one window, want 1", n)
	}
}

func TestBellForceEventsBypassThrottle(t *testing.T) {
	// Milestones ring even immediately after a coin.
	var buf bytes.Buffer
	b := newBell(&buf)
	now := time.Now()
	b.now = func() time.Time { return now }
	b.ring("coin")
	b.ring("oneup")
	if n := bytes.Count(buf.Bytes(), []byte{7}); n != 2 {
		t.Errorf("forced event was throttled: %d BELs, want 2", n)
	}
}
