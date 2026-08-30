package sshd

// Session admission with a fair FIFO queue. When MaxSessions is full,
// excess players complete the handshake and wait on a live screen showing
// their position and an honest estimate instead of being cut off. The
// estimate is Little's law over an EWMA of observed session lengths:
// wait ≈ position × mean-session / capacity. Waiting sessions cost
// nothing but the transport — no game loop runs until admission.

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

const (
	defaultMaxQueue     = 32
	defaultQueueTimeout = 10 * time.Minute

	// ewmaSeed is the prior for mean session length (seconds) before the
	// first real observation: generous, so early ETAs err high.
	ewmaSeed  = 240.0
	ewmaAlpha = 0.2
)

// errQueueClosed is returned to a waiting session that gave up (client
// left or timed out); the caller shuts the channel down without running
// the Handler.
var errQueueClosed = errors.New("sshd: left the admission queue")

// waiter is one queued session. pos is its 1-based place in line.
type waiter struct {
	sess  *Session
	ready chan struct{}
	pos   int
}

// admission tracks active sessions and the wait line. Every state
// transition happens under mu, which makes the line strictly FIFO and a
// released slot transfer atomically to the next waiter — a fresh
// connection can never steal a slot the line is owed.
type admission struct {
	mu      sync.Mutex
	active  int
	waiters []*waiter
	ewmaSec float64 // EWMA of admitted session length, seconds
	samples int64
}

func newAdmission() *admission {
	return &admission{ewmaSec: ewmaSeed}
}

// room reports whether one more connection may be admitted or queued.
func (a *admission) room(maxSessions, maxQueue int) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if maxQueue < 0 {
		return a.active < maxSessions
	}
	return a.active+len(a.waiters) < maxSessions+maxQueue
}

// enter blocks until the session is admitted (returning its admission
// time for the duration EWMA) or the wait ends without a slot. The queue
// screen on the session is kept current while waiting.
func (a *admission) enter(s *Session, maxSessions, maxQueue int, timeout time.Duration, now func() time.Time) (time.Time, error) {
	if now == nil {
		now = time.Now
	}
	w := &waiter{sess: s, ready: make(chan struct{})}

	a.mu.Lock()
	if len(a.waiters) == 0 && a.active < maxSessions {
		a.active++
		a.mu.Unlock()
		return now(), nil
	}
	if maxQueue < 0 || len(a.waiters) >= maxQueue {
		a.mu.Unlock()
		return time.Time{}, errQueueClosed
	}
	w.pos = len(a.waiters) + 1
	a.waiters = append(a.waiters, w)
	screen := a.queueScreenLocked(w, maxSessions)
	a.mu.Unlock()
	if _, err := s.Write(screen); err != nil {
		a.remove(w)
		return time.Time{}, errQueueClosed
	}

	deadline := now().Add(timeout)
	a.mu.Lock()
	lastPos := w.pos // under mu: exit/remove shift positions concurrently
	a.mu.Unlock()
	// One timer per arm, reset each loop: a fresh time.After per
	// iteration parked an unreleasable timer (and its 1s wake) for
	// every second of the whole wait — hundreds per queued player.
	drop := time.NewTimer(time.Until(deadline))
	poll := time.NewTimer(time.Second)
	defer drop.Stop()
	defer poll.Stop()
	for {
		select {
		case <-w.ready:
			return now(), nil
		case <-s.Done():
			a.remove(w)
			return time.Time{}, errQueueClosed
		case <-drop.C:
			a.remove(w)
			s.Write([]byte("\r\n\r\nThe line moved too slowly — disconnected. Come back soon!\r\n"))
			return time.Time{}, errQueueClosed
		case <-poll.C:
			a.mu.Lock()
			pos := w.pos
			if pos == 0 { // dequeued by a timeout path racing us
				a.mu.Unlock()
				poll.Reset(time.Second)
				continue
			}
			var upd []byte
			if pos != lastPos {
				lastPos = pos
				upd = a.queueScreenLocked(w, maxSessions)
			}
			a.mu.Unlock()
			if upd != nil {
				if _, err := s.Write(upd); err != nil {
					a.remove(w)
					return time.Time{}, errQueueClosed
				}
			}
			poll.Reset(time.Second)
		}
	}
}

// exit records the session length and hands the freed slot straight to
// the head of the line, if any.
func (a *admission) exit(dur time.Duration) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.active--
	a.samples++
	d := dur.Seconds()
	if d < 1 {
		d = 1 // an instant reconnect shouldn't drag the ETA to zero
	}
	a.ewmaSec += ewmaAlpha * (d - a.ewmaSec)
	if len(a.waiters) > 0 {
		w := a.waiters[0]
		a.waiters = a.waiters[1:]
		for i, x := range a.waiters {
			x.pos = i + 1
		}
		a.active++ // the slot transfers; no window where it can be stolen
		close(w.ready)
	}
}

// remove drops a waiter that gave up.
func (a *admission) remove(w *waiter) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for i, x := range a.waiters {
		if x == w {
			a.waiters = append(a.waiters[:i], a.waiters[i+1:]...)
			for j, y := range a.waiters[i:] {
				y.pos = i + j + 1
			}
			break
		}
	}
}

// queueScreenLocked renders the waiting screen for w; call with mu held.
func (a *admission) queueScreenLocked(w *waiter, maxSessions int) []byte {
	const view = "\x1b[?25l\x1b[2J\x1b[H" +
		"\x1b[1;36m  SUPER CLI MARIO\x1b[0m\r\n\r\n" +
		"\x1b[1m  The server is full\x1b[0m — %d players are in the game.\r\n\r\n" +
		"  You are \x1b[1;33m#%d in line\x1b[0m.\r\n" +
		"  Estimated wait: \x1b[1m~%d min\x1b[0m\r\n\r\n" +
		"  Keep this window open — your game starts\r\n" +
		"  the moment a slot frees up.\r\n\r\n" +
		"  \x1b[2mCtrl-C to leave the line.\x1b[0m\r\n"
	mins := int(float64(w.pos)*a.ewmaSec/float64(maxSessions)/60) + 1
	return []byte(fmt.Sprintf(view, maxSessions, w.pos, mins))
}
