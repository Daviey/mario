package sshd

// Control-lane behavior: queueing, drop policy and overflow teardown,
// exercised on a bare conn over a net.Pipe (no listener, no session).

import (
	"io"
	"log"
	"net"
	"testing"
)

// Control-lane drop policy: keepalive-class replies may drop once a
// round of them is queued (a lost keepalive costs a disconnect at
// worst), but channel-request replies must never drop — ssh(1) waits
// forever on a missing ChannelSuccess (a silent hang, worse than a
// disconnect). Lossless items queue up to ctlMax; overflow tears the
// connection down rather than growing without bound.
func TestControlQueueDropPolicy(t *testing.T) {
	c1, c2 := net.Pipe()
	defer c2.Close()
	c := &conn{
		t:           newTransport(c1),
		srv:         &Server{Log: log.New(io.Discard, "", 0)},
		controlLane: newControlLane(),
	}
	qLen := func() int {
		c.ctlMu.Lock()
		defer c.ctlMu.Unlock()
		return len(c.ctlQ)
	}

	for range ctlKeepaliveCap + 4 {
		c.writeControlDrop([]byte{msgRequestSuccess})
	}
	if n := qLen(); n != ctlKeepaliveCap {
		t.Fatalf("queued %d keepalives, want capped at %d", n, ctlKeepaliveCap)
	}

	for range ctlMax - ctlKeepaliveCap {
		c.writeControl([]byte{msgChannelSuccess})
	}
	if n := qLen(); n != ctlMax {
		t.Fatalf("queued %d replies, want exactly %d (lossless to the cap)", n, ctlMax)
	}

	// One lossless item over the cap: the connection closes, the item
	// is rejected, and the backlog is trimmed on the spot — the dying
	// connection must not sit on ctlMax queued packets (plus their
	// buffers) while the handlers wind down.
	c.writeControl([]byte{msgChannelSuccess})
	if _, err := c2.Read(make([]byte, 1)); err == nil {
		t.Fatal("control-queue overflow must close the connection")
	}
	if n := qLen(); n != 0 {
		t.Fatalf("overflow left %d items queued, want the queue trimmed", n)
	}
}
