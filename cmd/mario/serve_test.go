package main

// Unit coverage for the session-side plumbing of playSession: the frame
// writer's dead-connection contract, the bell channel's drop-when-busy
// contract, and the terminal-fit math shared with the native runner.
// The SSH E2E exercises all three through a real client; these pin the
// units without the transport.

import (
	"errors"
	"testing"
	"time"
)

// stubSession stands in for *sshd.Session: it records writes, and
// writeErr makes every Write fail like a transport the SSH layer has
// given up on.
type stubSession struct {
	writeErr error
	closed   int
}

func (s *stubSession) Write(p []byte) (int, error) {
	if s.writeErr != nil {
		return 0, s.writeErr
	}
	return len(p), nil
}

func (s *stubSession) Close() error { s.closed++; return nil }

// A failed frame write must close the session exactly once: Close is
// what makes Session.Done fire and end the play loop — without it the
// tick loop spins forever on a broken pipe.
func TestSessWriterClosesSessionOnWriteError(t *testing.T) {
	s := &stubSession{writeErr: errors.New("broken pipe")}
	w := &sessWriter{s: s}
	if _, err := w.Write([]byte("frame")); err == nil {
		t.Fatal("Write reported success on a failing session")
	}
	if s.closed != 1 {
		t.Errorf("failed write closed the session %d times, want 1", s.closed)
	}
}

// A healthy write must never close the session: Close tears down a
// live connection.
func TestSessWriterKeepsHealthySessionOpen(t *testing.T) {
	s := &stubSession{}
	w := &sessWriter{s: s}
	if _, err := w.Write([]byte("frame")); err != nil {
		t.Fatalf("healthy write failed: %v", err)
	}
	if s.closed != 0 {
		t.Errorf("healthy write closed the session %d times, want 0", s.closed)
	}
}

// A full bell channel means the writer goroutine is stuck on flow
// control; Write must return at once, reporting success, without
// disturbing the queued bells.
func TestBellChanWriterDropsWhenBusy(t *testing.T) {
	bells := make(chan []byte, 8)
	w := &bellChanWriter{c: bells}
	for i := range 8 {
		bells <- []byte{7, byte(i)} // tag each queued payload
	}

	type result struct {
		n   int
		err error
	}
	returned := make(chan result, 1)
	go func() {
		n, err := w.Write([]byte{7})
		returned <- result{n, err}
	}()
	select {
	case r := <-returned:
		if r.err != nil || r.n != 1 {
			t.Fatalf("Write = (%d, %v), want (1, nil)", r.n, r.err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Write blocked on a full bell channel instead of dropping")
	}

	if len(bells) != 8 {
		t.Fatalf("dropped write changed the queue: len = %d, want 8", len(bells))
	}
	for i := range 8 {
		b := <-bells
		if len(b) != 2 || b[1] != byte(i) {
			t.Fatalf("queued bell %d corrupted: %v", i, b)
		}
	}
}

// viewFor is the one fit rule every surface uses: tiles from cells,
// two HUD/status rows off the height, half-block rows to pixels. The
// degenerate pty flows through unreduced — the clamps downstream
// (App.Resize: 16..60 wide, 4+ tall; the engine's level bounds) are
// what keep a tiny terminal playable.
func TestViewFor(t *testing.T) {
	for _, tc := range []struct {
		name         string
		cols, rows   int
		wantW, wantH int
	}{
		{"classic 80x24", 80, 24, 13, 7},
		{"roomy 120x32", 120, 32, 20, 10},
		{"degenerate 20x5 pty", 20, 5, 3, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, h := viewFor(tc.cols, tc.rows)
			if w != tc.wantW || h != tc.wantH {
				t.Errorf("viewFor(%d, %d) = %dx%d, want %dx%d",
					tc.cols, tc.rows, w, h, tc.wantW, tc.wantH)
			}
		})
	}
}
