package sshd

// The session channel: the server side of the single SSH session
// channel a connection may open, plus the Session API the game handler
// sees. Everything here is the channel's own state and accessors; the
// protocol loop that drives it lives in server.go.
import (
	"errors"
	"net"
	"sync"
	"time"

	"github.com/Daviey/mario/render"
)

// Session is one connected player.
type Session struct {
	ch *channel
}

// Write sends bytes to the client's terminal (respecting the SSH flow
// control window; blocks until the client drains).
func (s *Session) Write(p []byte) (int, error) { return s.ch.write(p) }

// Logf emits an operator line on the server's logger (stderr by
// default): session lifecycle telemetry from handlers.
func (s *Session) Logf(format string, args ...any) {
	s.ch.srv.Log.Printf(format, args...)
}

// RemoteAddr returns the connected client's address (host:port) — for
// hosts that key per-player state such as input-calibration warm-start
// by origin.
func (s *Session) RemoteAddr() string { return s.ch.conn.RemoteAddr().String() }

// Size returns the pty size in character cells (80x24 if no pty-req).
func (s *Session) Size() (cols, rows int) {
	s.ch.mu.Lock()
	defer s.ch.mu.Unlock()
	return s.ch.cols, s.ch.rows
}

// Term returns the TERM value from pty-req, or "" without one.
func (s *Session) Term() string { return s.ch.termValue() }

// Env returns an environment variable sent via env requests.
func (s *Session) Env(name string) string {
	s.ch.mu.Lock()
	defer s.ch.mu.Unlock()
	return s.ch.env[name]
}

// OnFeed installs the keystroke handler (raw terminal bytes from the
// client, e.g. app.Feed). It also releases the color-depth probe's
// buffer: keystrokes typed while the probe waited for the terminal's
// DA2/DA3 reply are replayed into f, and later input flows directly
// (the probe keeps only stripping any late replies).
func (s *Session) OnFeed(f func([]byte)) {
	s.ch.mu.Lock()
	probe := s.ch.probe
	s.ch.feed = f
	s.ch.mu.Unlock()
	if probe != nil {
		if held := probe.drain(); len(held) > 0 {
			f(held)
		}
	}
}

// TrueColor reports whether the client's terminal renders 24-bit
// color. Resolution order: a COLORTERM the client forwarded as an env
// request (ssh -o SendEnv=COLORTERM), then the TERM family, then the
// DA2/DA3 probe fired at pty-req (waits up to the server's ProbeWait
// for the terminal to identify itself; silent terminals fall back to
// the TERM rules). See termprobe.go.
func (s *Session) TrueColor() bool {
	return s.ch.decideColorTerm() != ""
}

// ColorTerm returns the DECIDED color-depth signal for this session —
// env request > TERM family > DA probe (see TrueColor). Empty means the
// 16-color palette. Telemetry should report this, not the raw env: the
// probe decides most real sessions.
func (s *Session) ColorTerm() string {
	return s.ch.decideColorTerm()
}

// ColorDepth reports the SGR color mode this session's client renders
// (render.Colors24/256/16): 24-bit when TrueColor holds (env request >
// TERM family > DA probe), otherwise the fixed 256-color cube whenever
// the pty TERM advertises 256 colors — every -256color terminal and
// mosh's cell model honors it — and base-16 only for terminals that
// claim neither.
func (s *Session) ColorDepth() render.ColorMode {
	return render.ColorDepthFor(s.Term(), s.ColorTerm())
}

// ClientVersion returns the client's SSH identification string (e.g.
// "SSH-2.0-OpenSSH_10.4") from the RFC 4253 banner exchange — the
// ssh-surface user agent for telemetry.
func (s *Session) ClientVersion() string {
	return string(s.ch.t.vPeer)
}

// OnResize installs the pty-size handler (fired on every window-change
// request, e.g. app.Resize).
func (s *Session) OnResize(f func(cols, rows int)) {
	s.ch.mu.Lock()
	defer s.ch.mu.Unlock()
	s.ch.onResize = f
}

// Done is closed when the client goes away (disconnect, channel close or
// transport error).
func (s *Session) Done() <-chan struct{} { return s.ch.done }

// Close ends the session: EOF + channel close, then the connection.
func (s *Session) Close() error { return s.ch.shutdown() }

var errChannelClosed = errors.New("sshd: channel closed")

// channel is the server side of the single session channel. Locking:
// mu guards every mutable field — window, owe, cols/rows, term, env,
// feed, onResize and probe — plus the FIFO handoffs that hang off them
// (cond wakes window-blocked writers; closeOnce+done make shutdown
// idempotent and observable). Fields read before the session starts
// (t, srv, conn, maxPkt, wait) are set once at open and never written
// again, so they need no lock.
type channel struct {
	t    *transport
	srv  *Server
	conn net.Conn
	lane *conn // control lane for the teardown burst; nil without a pump

	mu          sync.Mutex
	cond        *sync.Cond
	window      int // client's receive window for our CHANNEL_DATA
	maxPkt      int // client's max packet size
	owe         int // inbound bytes consumed but not yet window-adjusted
	feed        func([]byte)
	onResize    func(cols, rows int)
	term        string
	cols        int
	rows        int
	env         map[string]string
	shelled     bool // shell request seen
	execStarted bool // mosh-handshake exec seen
	remoteGone  bool
	sendErr     bool

	// Color-depth probe state (termprobe.go): created at the shell
	// request (not pty-req — see channelRequest), when the session may
	// first talk to the terminal.
	probe *termProbe
	wait  time.Duration // per-session ProbeWait, copied at channel open

	closeOnce sync.Once
	done      chan struct{}
}

// finGrace is the post-teardown pause before the FIN: slamming TCP
// shut in the same instant as the channel close makes ssh(1) report
// "connection closed by remote host" instead of exiting cleanly.
const finGrace = 300 * time.Millisecond

// rxWindow is the 2MB receive window advertised at channel open —
// generous for a keystrokes-only input path. adjustFloor is exactly
// half of it: owed inbound bytes are replenished once they reach the
// halfway mark (OpenSSH's own adjust-at-half policy), so the client
// always retains ≥1MB of credit. A human typing never crosses the
// floor — zero adjust packets for ordinary play — while a multi-
// megabyte paste still gets its window back long before it could stall.
const (
	rxWindow    = 1 << 21
	adjustFloor = rxWindow / 2
)

// addOwed records inbound bytes the handler consumed and reports
// whether the receive window needs replenishing now.
func (c *channel) addOwed(n int) bool {
	c.mu.Lock()
	c.owe += n
	flush := c.owe >= adjustFloor
	c.mu.Unlock()
	return flush
}

// takeOwed claims every owed byte as one WINDOW_ADJUST payload (nil if
// nothing is owed). Called only from the control pump.
func (c *channel) takeOwed() []byte {
	c.mu.Lock()
	n := c.owe
	c.owe = 0
	c.mu.Unlock()
	if n <= 0 {
		return nil
	}
	w := &buf{}
	w.u8(msgWindowAdjust)
	w.u32(0)
	w.u32(uint32(n))
	return w.b
}

func (c *channel) shutdown() error {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.remoteGone = true
		c.sendErr = true
		c.cond.Broadcast()
		c.mu.Unlock()
		// A well-behaved session ends with an exit status (ssh(1)
		// exits 255 without one), then EOF and the channel close. The
		// burst rides the control lane (lossless FIFO), so shutdown —
		// which runs on the reader goroutine when the client closes —
		// never blocks behind a frame chunk stuck on a full kernel
		// buffer: the exact head-of-line stall the lane exists to
		// prevent. done closes only after the burst is queued, and the
		// FIN grace below (plus serveConn's drainControl) gives the
		// pump time to write it before the socket goes away.
		for _, pkt := range c.teardown() {
			if c.lane != nil {
				c.lane.writeControl(pkt)
			} else {
				// No pump exists (unit-test pipes): write directly.
				c.t.writePacket(pkt)
			}
		}
		close(c.done)
		// Give the client a moment to process the channel close before
		// the FIN — slamming TCP shut at the same instant makes ssh(1)
		// report "connection closed by remote host" instead of exiting
		// cleanly. If the client closes first, the reader notices.
		go func() {
			time.Sleep(finGrace)
			c.conn.Close()
		}()
	})
	return nil
}

// teardown builds the end-of-session wire choreography: exit-status
// request, EOF, then CHANNEL_CLOSE.
func (c *channel) teardown() [][]byte {
	es := &buf{}
	es.u8(msgChannelRequest)
	es.u32(0)
	es.cstr("exit-status")
	es.boolean(false)
	es.u32(0)
	eof := []byte{msgChannelEOF, 0, 0, 0, 0}
	w := &buf{}
	w.u8(msgChannelClose)
	w.u32(0)
	return [][]byte{es.b, eof, w.b}
}

func (c *channel) write(p []byte) (int, error) {
	sent := 0
	for len(p) > 0 {
		c.mu.Lock()
		for c.window <= 0 && !c.remoteGone {
			c.cond.Wait()
		}
		if c.remoteGone || c.sendErr {
			c.mu.Unlock()
			return sent, errChannelClosed
		}
		n := min(len(p), c.maxPkt, c.window)
		c.window -= n
		c.mu.Unlock()

		w := &buf{}
		w.u8(msgChannelData)
		w.u32(0) // our channel id
		w.str(p[:n])
		if err := c.t.writePacket(w.b); err != nil {
			c.mu.Lock()
			c.sendErr = true
			c.cond.Broadcast()
			c.mu.Unlock()
			return sent, err
		}
		p = p[n:]
		sent += n
	}
	return sent, nil
}

// decideColorTerm resolves the COLORTERM value for a game the server
// is about to start: an explicit env request wins, then the TERM
// family (render.TrueColorTerm), then the DA2/DA3 probe. Empty means
// the 16-color palette.
func (ch *channel) decideColorTerm() string {
	if v := ch.envColorTerm(); v != "" {
		return v
	}
	if render.TrueColorTerm(ch.termValue()) {
		return "truecolor"
	}
	if p := ch.probeRef(); p != nil {
		if decided, ok := p.result(ch.probeWait()); decided && ok {
			return "truecolor"
		}
	}
	return ""
}

func (ch *channel) termValue() string {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	return ch.term
}

func (ch *channel) probeRef() *termProbe {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	return ch.probe
}

func (ch *channel) probeWait() time.Duration {
	if w := ch.wait; w > 0 {
		return w
	}
	return defaultProbeWait
}

// envColorTerm returns the COLORTERM the client sent as an env request
// (e.g. ssh -o SendEnv=COLORTERM), "" when none.
func (c *channel) envColorTerm() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.env["COLORTERM"]
}
