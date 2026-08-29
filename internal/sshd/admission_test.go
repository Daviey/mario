package sshd

// Admission-queue behavior: over-capacity sessions wait in FIFO order on
// a live screen, get admitted as slots free, and give up cleanly on
// timeout or disconnect.

import (
	"net"
	"strings"
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
	deadline := time.Now().Add(5 * time.Second)
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
