package main

// Terminal-bell sound: engine sound events become BEL (0x07) bytes on
// the output stream — the one audio channel every terminal carries.
// Terminals map BEL to a beep, a window flash or an urgency hint
// according to their own settings; over SSH it rides the session
// channel, and mosh forwards it to the client terminal with the frame
// stream.

import (
	"io"
	"time"
)

// bellMinGap throttles transient feedback: a coin cascade or a stomp
// chain emits every tick, which would machine-gun the bell.
const bellMinGap = 120 * time.Millisecond

// bellNever lists events that never ring: jump fires constantly during
// play, tick is the score-count run at level end, pause is UI chrome.
var bellNever = map[string]bool{"jump": true, "tick": true, "pause": true}

// bellForce lists rare one-shots that bypass the throttle — milestones
// worth hearing even mid coin cascade.
var bellForce = map[string]bool{
	"die": true, "gameover": true, "win": true, "flag": true,
	"clear": true, "oneup": true, "powerup": true, "star": true, "hurry": true,
}

// bellRinger turns sound events into BEL bytes on w.
type bellRinger struct {
	w    io.Writer
	last time.Time        // last ring, for throttling
	now  func() time.Time // injectable clock for tests
}

func newBell(w io.Writer) *bellRinger {
	return &bellRinger{w: w, now: time.Now}
}

// ring emits one BEL for ev, curated and throttled. Write errors are
// ignored: a dead writer is the session teardown's business.
func (b *bellRinger) ring(ev string) {
	if bellNever[ev] {
		return
	}
	now := b.now()
	if !bellForce[ev] && now.Sub(b.last) < bellMinGap {
		return
	}
	b.last = now
	b.w.Write([]byte{7})
}
