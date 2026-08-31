package sshd

// The control lane: the connection's non-frame write path.
//
// Concurrency model (one reader goroutine per conn — see serveConn):
// once the session may be producing frames, the reader goroutine NEVER
// writes to the socket. A synchronous write there — a window adjust, a
// keepalive reply — would queue behind the wmu-held frame chunk stuck
// on a full kernel send buffer (up to writeTimeoutSec), stalling every
// later inbound packet too: keystrokes, the client's own window
// adjusts, disconnects. Replies go through ctlQ, owed adjusts through
// ch.owe (both lossless — a dropped adjust would wedge the client
// after 2MB, a dropped ChannelSuccess hangs ssh(1) on a want-reply
// request), both sent by pumpControl. Input handling stays pure:
// read → feed.
//
// The writers once play has started: pumpControl here (control
// packets, window adjusts) and the session's frame writer (ch.write).
// Every other goroutine hands packets over; none of them touch the
// socket directly.

import (
	"sync"
	"time"
)

// controlLane is the queue machinery behind the control path above.
// conn embeds it; the methods driving the lane live on *conn (they
// need the transport and the server log).
type controlLane struct {
	ctlMu sync.Mutex
	ctlQ  []ctlMsg // FIFO of replies, sync markers, keepalives

	// ctlWake wakes the pump (cap 1, edge). adjustSig is the
	// "owed ≥ adjustFloor" edge and hands the pump the channel
	// pointer itself, so no goroutine ever reads c.ch across the
	// reader's single write of it (openChannel).
	ctlWake   chan struct{}
	adjustSig chan *channel
	dead      chan struct{} // closed when serveConn returns
}

func newControlLane() controlLane {
	return controlLane{
		ctlWake:   make(chan struct{}, 1),
		adjustSig: make(chan *channel, 1),
		dead:      make(chan struct{}),
	}
}

// ctlMsg is one control-lane item: a packet to write, or (pkt nil) a
// sync marker — closed by the pump once every earlier item is written.
type ctlMsg struct {
	pkt  []byte
	done chan struct{}
	drop bool // keepalive-class: droppable at capacity
}

// ctlKeepaliveCap bounds queued keepalive-class items: losing a
// keepalive reply costs a disconnect at worst (recoverable), and once
// a round of them sits unwritten, TCP itself is the backpressure.
// ctlMax is the hard FIFO bound for everything lossless; reaching it
// means the link is dead in practice, so the connection is torn down
// (a reconnect) rather than risking unbounded memory.
const (
	ctlKeepaliveCap = 8
	ctlMax          = 1024
)

// writeControl hands a reply packet to the control pump: lossless.
// writeControlDrop is the keepalive-class variant, droppable at
// capacity. Window adjusts use neither; they ride the lossless owed
// counter.
func (c *conn) writeControl(pkt []byte) {
	c.sendControl(ctlMsg{pkt: pkt})
}

func (c *conn) writeControlDrop(pkt []byte) {
	c.sendControl(ctlMsg{pkt: pkt, drop: true})
}

// sendControl enqueues one item, never blocking. Keepalive-class items
// are dropped once the queue holds a full round of them; everything
// else queues without loss until ctlMax, where the connection is
// closed instead — ssh(1) waits forever on a missing reply to a
// want-reply request (a silent hang, worse than a disconnect), so the
// reply lane must never drop. The dying connection also releases the
// backlog: ctlMax queued packets (and their buffers) are trimmed and
// sync-marker waiters unblocked immediately, not held until the
// handlers wind down.
func (c *conn) sendControl(m ctlMsg) {
	c.ctlMu.Lock()
	if m.drop && len(c.ctlQ) >= ctlKeepaliveCap {
		c.ctlMu.Unlock()
		return
	}
	if len(c.ctlQ) >= ctlMax {
		for _, old := range c.ctlQ {
			if old.done != nil {
				close(old.done)
			}
		}
		c.ctlQ = nil
		c.ctlMu.Unlock()
		c.srv.Log.Printf("session %s: control queue overflow, closing", c.t.conn.RemoteAddr())
		c.t.conn.Close()
		return
	}
	c.ctlQ = append(c.ctlQ, m)
	c.ctlMu.Unlock()
	select {
	case c.ctlWake <- struct{}{}:
	default:
	}
}

// syncControl waits until the pump has written every item queued before
// it. Used only at session start (shell/exec), where the wire is quiet:
// it pins request replies ahead of the handler's first output without
// ever putting the reader back on the socket. Post-session-start use
// would be a bug — a congested pump would stall inbound processing.
func (c *conn) syncControl() {
	done := make(chan struct{})
	c.sendControl(ctlMsg{done: done})
	select {
	case <-done:
	case <-c.dead:
	}
}

// drainControl is syncControl with a bound: the teardown path may not
// wait on a wedged pump forever, so after d it gives up and lets the
// caller close the connection anyway. Used by serveConn after the
// session ends, to give the queued teardown burst a moment to reach
// the wire before the socket closes.
func (c *conn) drainControl(d time.Duration) {
	done := make(chan struct{})
	c.sendControl(ctlMsg{done: done})
	select {
	case <-done:
	case <-time.After(d):
	case <-c.dead:
	}
}

// pumpControl is the connection's control writer — with the session's
// frame writer, the only goroutine that writes once play has started.
// It dies with the connection: serveConn closes dead, and a failing
// write (conn already closing elsewhere) also tears it down.
func (c *conn) pumpControl() {
	for {
		select {
		case <-c.dead:
			return
		case ch := <-c.adjustSig:
			if pkt := ch.takeOwed(); pkt != nil {
				if err := c.t.writePacket(pkt); err != nil {
					c.t.conn.Close()
					return
				}
			}
		case <-c.ctlWake:
			c.ctlMu.Lock()
			q := c.ctlQ
			c.ctlQ = nil
			c.ctlMu.Unlock()
			for _, m := range q {
				if m.done != nil {
					close(m.done)
					continue
				}
				if m.pkt == nil {
					continue
				}
				if err := c.t.writePacket(m.pkt); err != nil {
					c.t.conn.Close()
					return
				}
			}
		}
	}
}
