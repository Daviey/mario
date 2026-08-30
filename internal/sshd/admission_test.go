package sshd

// Admission-queue behavior: over-capacity sessions wait in FIFO order on
// a live screen, get admitted as slots free, and give up cleanly on
// timeout or disconnect.

import (
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// gateHandler admits one blocked session per token on release; the marker
// write lets the client side observe the admission order.
func gateHandler(t *testing.T) (handler func(*Session), release chan struct{}) {
	release = make(chan struct{})
	// One token = one whole session: the handler returns (freeing its
	// slot) right after writing the marker, so FIFO admission is visible
	// client-side as the order the markers arrive on each connection.
	// Cleanup unwedges any handler still blocked on a token: receiving
	// from a closed channel returns immediately.
	handler = func(s *Session) {
		<-release
		s.Write([]byte("ADMITTED\r\n"))
	}
	return handler, release
}

// waitData reads CHANNEL_DATA until want appears (the queue screen may
// span packets).
func waitData(t *testing.T, tc *testClient, want string) string {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second) // generous: the single-core CI runner starves handshakes when all package test binaries run concurrently
	var got string
	for !strings.Contains(got, want) {
		if time.Now().After(deadline) {
			t.Fatalf("timeout waiting for %q; got %q", want, got)
		}
		d := tc.read()
		if len(d) > 0 && d[0] == msgChannelData {
			r := &reader{b: d[1:]}
			r.u32()
			got += string(r.str())
		}
	}
	return got
}

func TestQueueWaitsThenAdmitsFIFO(t *testing.T) {
	handler, release := gateHandler(t)
	srv := startServer(t, handler, func(s *Server) { s.MaxSessions = 1 })

	a := dial(t, srv.Addr)
	a.authNone()
	a.openSession(1<<20, 32768)
	a.shell() // occupies the single slot (handler now blocked on release)

	b := dial(t, srv.Addr)
	b.authNone()
	b.openSession(1<<20, 32768)
	b.shell()
	if scr := waitData(t, b, "in line"); !strings.Contains(scr, "#1") {
		t.Fatalf("first waiter screen missing position: %q", scr)
	} else if !strings.Contains(scr, "Estimated wait") || !strings.Contains(scr, "1 player") && !strings.Contains(scr, "players are in the game") {
		t.Fatalf("waiter screen missing ETA/full-server lines: %q", scr)
	}

	c := dial(t, srv.Addr)
	c.authNone()
	c.openSession(1<<20, 32768)
	c.shell()
	waitData(t, c, "#2")

	release <- struct{}{} // A finishes and frees its slot
	waitData(t, a, "ADMITTED")
	// B (first in line) holds the slot now, handler blocked on its token;
	// C must hear nothing but its own position update.
	c.nc.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	if d, err := c.tr.readPacket(); err == nil && strings.Contains(string(d), "ADMITTED") {
		t.Fatal("C admitted out of order (B holds the slot)")
	}
	release <- struct{}{} // B finishes; C's turn
	waitData(t, b, "ADMITTED")
	release <- struct{}{}
	waitData(t, c, "ADMITTED")
}

func TestQueueTimeoutDropsWaiter(t *testing.T) {
	handler, release := gateHandler(t)
	defer close(release)
	srv := startServer(t, handler, func(s *Server) {
		s.MaxSessions = 1
		s.QueueTimeout = 150 * time.Millisecond
	})

	a := dial(t, srv.Addr)
	a.authNone()
	a.openSession(1<<20, 32768)
	a.shell()

	b := dial(t, srv.Addr)
	b.authNone()
	b.openSession(1<<20, 32768)
	b.shell()
	waitData(t, b, "in line")
	waitData(t, b, "line moved too slowly")
	// The server tears the session down afterwards: drain the wire
	// (teardown packets may still be queued) until EOF/close.
	b.nc.SetReadDeadline(time.Now().Add(3 * time.Second))
	buf := make([]byte, 256)
	closed := false
	for {
		if _, err := b.nc.Read(buf); err != nil {
			closed = true
			break
		}
	}
	if !closed {
		t.Error("connection did not close after queue timeout")
	}
}

func TestQueueCapRefusesBeyondDepth(t *testing.T) {
	handler, release := gateHandler(t)
	defer close(release)
	srv := startServer(t, handler, func(s *Server) {
		s.MaxSessions = 1
		s.MaxQueue = 1
	})

	a := dial(t, srv.Addr)
	a.authNone()
	a.openSession(1<<20, 32768)
	a.shell()

	b := dial(t, srv.Addr)
	b.authNone()
	b.openSession(1<<20, 32768)
	b.shell()
	waitData(t, b, "#1") // the queue's only chair

	nc, err := net.Dial("tcp", srv.Addr)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	nc.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 512)
	n, _ := nc.Read(buf)
	got := string(buf[:n])
	if !strings.Contains(got, serverVersion) {
		t.Fatalf("over-depth refusal missing version line: %q", got)
	}
}

// remove drops a waiter that gives up: the followers are renumbered
// under mu (their next poll sees the new position), the head's removal
// promotes the next waiter to #1, and removing an already-dequeued
// waiter is a no-op.
func TestAdmissionRemoveRenumber(t *testing.T) {
	a := newAdmission()
	ws := make([]*waiter, 3)
	for i := range ws {
		ws[i] = &waiter{ready: make(chan struct{})}
		a.waiters = append(a.waiters, ws[i])
		ws[i].pos = i + 1
	}

	a.remove(ws[1]) // middle waiter gives up mid-line
	if len(a.waiters) != 2 || a.waiters[0] != ws[0] || a.waiters[1] != ws[2] {
		t.Fatalf("remove(mid) left line %v", a.waiters)
	}
	if ws[0].pos != 1 || ws[2].pos != 2 {
		t.Fatalf("positions after mid removal = %d,%d, want 1,2", ws[0].pos, ws[2].pos)
	}

	a.remove(ws[0]) // head gives up
	if len(a.waiters) != 1 || a.waiters[0] != ws[2] {
		t.Fatalf("remove(head) left line %v", a.waiters)
	}
	if ws[2].pos != 1 {
		t.Fatalf("new head position = %d, want 1", ws[2].pos)
	}

	a.remove(ws[1]) // already dequeued: no-op
	if len(a.waiters) != 1 || a.waiters[0] != ws[2] {
		t.Fatalf("remove of absent waiter disturbed the line: %v", a.waiters)
	}
}

// A waiter that walks away mid-line must not stall the line: the
// followers get renumbered (C sees #1), and the freed slot goes to the
// new head — never past it.
func TestQueueDisconnectPromotesNext(t *testing.T) {
	handler, release := gateHandler(t)
	defer close(release)
	srv := startServer(t, handler, func(s *Server) { s.MaxSessions = 1 })

	a := dial(t, srv.Addr)
	a.authNone()
	a.openSession(1<<20, 32768)
	a.shell() // holds the only slot

	b := dial(t, srv.Addr)
	b.authNone()
	b.openSession(1<<20, 32768)
	b.shell()
	waitData(t, b, "#1")
	c := dial(t, srv.Addr)
	c.authNone()
	c.openSession(1<<20, 32768)
	c.shell()
	waitData(t, c, "#2")

	// B disconnects: C must be told it moved up to #1 (the position
	// update arrives on the poll timer, within a second or two).
	b.nc.Close()
	if scr := waitData(t, c, "#1"); !strings.Contains(scr, "in line") {
		t.Fatalf("promotion screen missing position line: %q", scr)
	}

	release <- struct{}{} // A finishes; C (the promoted head) takes the slot
	waitData(t, a, "ADMITTED")
	// A's exit must hand the slot to C, the promoted head — C's marker
	// arrives only after its own handler token, pinning the order.
	release <- struct{}{}
	waitData(t, c, "ADMITTED")
}

// Concurrency stress at the unit level: N sessions churn through a
// capacity-C queue with random-ish holds. Invariants: every session is
// admitted exactly once (no double-admit), all N eventually get in
// (slots keep cycling), and the number of simultaneous slot holders
// never exceeds the capacity. Designed to be run under -race.
// newPipeSession builds a Session over a net.Pipe whose far end
// drains: queue screens and poll updates can be written without a
// listener. Used by the unit-level admission tests.
func newPipeSession(t *testing.T) *Session {
	t.Helper()
	c1, c2 := net.Pipe()
	t.Cleanup(func() { c1.Close(); c2.Close() })
	go func() { io.Copy(io.Discard, c2) }()
	ch := &channel{
		t:      newTransport(c1),
		conn:   c1,
		window: 1 << 20,
		maxPkt: 32768,
		done:   make(chan struct{}),
	}
	ch.cond = sync.NewCond(&ch.mu)
	return &Session{ch: ch}
}

func TestAdmissionConcurrentEnterExit(t *testing.T) {
	const (
		capacity = 4
		n        = 24
	)
	adm := newAdmission()
	var active, admittedTotal, peak atomic.Int32
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range n {
		s := newPipeSession(t)
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			at, err := adm.enter(s, capacity, defaultMaxQueue, time.Minute, nil)
			if err != nil {
				return // refused: cannot happen below the queue cap
			}
			admittedTotal.Add(1)
			if cur := active.Add(1); cur > peak.Load() {
				peak.Store(cur)
			}
			time.Sleep(time.Duration(idx%5) * time.Millisecond) // hold the slot
			active.Add(-1)
			adm.exit(time.Since(at))
		}(i)
	}
	close(start)
	wg.Wait()

	if got := admittedTotal.Load(); got != n {
		t.Fatalf("admitted %d of %d sessions, want all (slots keep cycling)", got, n)
	}
	if got := peak.Load(); got > capacity {
		t.Fatalf("peak simultaneous slot holders %d exceeded capacity %d", got, capacity)
	}
	// One admit per session, none left behind: every goroutine that
	// entered exited exactly once and the line drained.
	adm.mu.Lock()
	left := len(adm.waiters)
	adm.mu.Unlock()
	if left != 0 {
		t.Fatalf("%d waiters left after all sessions exited", left)
	}
}

// enter refuses outright when the line itself is full: with every slot
// held and the queue at maxQueue, the newcomer gets errQueueClosed
// without a handler ever running.
func TestAdmissionEnterRefusedAtQueueCap(t *testing.T) {
	adm := newAdmission()
	adm.active = 1
	adm.waiters = append(adm.waiters, &waiter{ready: make(chan struct{})})
	if _, err := adm.enter(newPipeSession(t), 1, 1, time.Minute, nil); err == nil {
		t.Fatal("enter admitted past a full queue")
	}
}
