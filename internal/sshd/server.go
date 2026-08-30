package sshd

// Server: an unauthenticated SSH2 server whose only service is a game.
//
// Design: auth method "none" is accepted for any username, but the ONLY
// thing a client can do is open one session channel with a pty and play —
// exec, subsystems, X11/agent forwarding and every global request except
// keepalive are refused. There is no shell behind this server.
//
// The handler runs per connection with a Session that mirrors the pieces
// of a terminal the game needs: a byte stream down to the client, a feed
// for keystrokes, the pty size and TERM/ENV values.
import (
	"crypto/ed25519"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/Daviey/mario/render"
)

// Server serves the game over SSH.
type Server struct {
	// Addr is the listen address, e.g. ":2222".
	Addr string

	// HostKeyFile optionally persists the host key (generated on first
	// use). Empty = a fresh ephemeral key every launch: clients see a new
	// host key each time (acceptable for a game; bring a file when you
	// want TOFU stability).
	HostKeyFile string

	// MaxSessions caps concurrent playing sessions (default 16).
	MaxSessions int

	// MaxQueue caps sessions waiting in line when MaxSessions is full
	// (default 32). Waiting players see their position and an estimated
	// wait; they are admitted FIFO as slots free. Negative disables the
	// queue: excess connections are refused, in the clear, pre-handshake.
	MaxQueue int

	// QueueTimeout drops a waiting session after this long (default
	// 10m). Zero means the default.
	QueueTimeout time.Duration

	// PostAuthWait bounds how long a connection that finished the
	// key exchange may sit without opening its session channel
	// (default 60s). Such connections hold a goroutine and a
	// transport but are invisible to the session cap — admission
	// counts playing sessions only — so without this deadline an
	// idling fleet bypasses the cap entirely. Tests shrink it.
	PostAuthWait time.Duration

	// ProbeWait bounds how long a session waits for the client
	// terminal's DA2/DA3 color-depth reply before falling back to the
	// TERM rules (default 250ms). Tests shrink it.
	ProbeWait time.Duration

	// Handler runs one game per session. It must return when the session
	// is done (Session.Done closed, Write errors, or its own quit path).
	Handler func(*Session)

	// Log receives connection lifecycle lines; nil logs to stderr.
	Log *log.Logger

	// MoshBin enables the mosh handshake when non-empty: the path to a
	// real mosh-server binary that exec requests shaped like
	// "mosh-server new ..." are allowed to spawn (strictly validated,
	// always running this game binary, always on MoshPortRange). The
	// spawned server outlives the SSH connection by design.
	MoshBin string

	// MoshPortRange is the UDP range handed to mosh-server, "lo:hi"
	// colon form (default "60000:60100"). Open the same range in the
	// host firewall — mosh's data path is pure UDP.
	MoshPortRange string

	hk  *hostKey
	adm *admission
}

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
func (s *Session) Term() string {
	s.ch.mu.Lock()
	defer s.ch.mu.Unlock()
	return s.ch.term
}

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
	s.ch.feed = f
	p := s.ch.probe
	s.ch.mu.Unlock()
	if p != nil {
		if b := p.drain(); len(b) > 0 {
			f(b)
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

// DrainProbe stops the color-depth probe and returns any player
// keystrokes it buffered while waiting for the DA2/DA3 reply. Call
// right after OnFeed so nothing typed during the probe window is
// lost; later input flows to the feed directly.
func (s *Session) DrainProbe() []byte {
	s.ch.mu.Lock()
	p := s.ch.probe
	s.ch.mu.Unlock()
	if p == nil {
		return nil
	}
	return p.drain()
}

// OnResize installs the pty-size handler (fired on every window-change
// request, e.g. app.Resize).
func (s *Session) OnResize(f func(cols, rows int)) {
	s.ch.mu.Lock()
	s.ch.onResize = f
	s.ch.mu.Unlock()
}

// Done is closed when the client goes away (disconnect, channel close or
// transport error).
func (s *Session) Done() <-chan struct{} { return s.ch.done }

// Close ends the session: EOF + channel close, then the connection.
func (s *Session) Close() error { return s.ch.shutdown() }

// defaultPostAuthWait is the fallback for Server.PostAuthWait: how long
// a post-KEX connection may sit with no session channel (see there).
const defaultPostAuthWait = 60 * time.Second

// maxAuthTries caps failed userauth attempts per connection. Auth is a
// formality here — method "none" is the only success — so a client
// grinding out failures is pure abuse; past the cap it gets a
// DISCONNECT instead of an infinite reply loop.
const maxAuthTries = 20

var errChannelClosed = errors.New("sshd: channel closed")

type channel struct {
	t    *transport
	srv  *Server
	conn net.Conn

	mu          sync.Mutex
	cond        *sync.Cond
	peerID      uint32
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

	// Color-depth probe state (termprobe.go): created at pty-req.
	probe *termProbe
	wait  time.Duration // per-session ProbeWait, copied at channel open

	closeOnce sync.Once
	done      chan struct{}
}

// adjustFloor is the coalescing threshold for receive-window adjusts:
// owed bytes are replenished once they reach half the 2MB window we
// advertise at channel open (OpenSSH's own adjust-at-half policy). A
// human typing never crosses it — zero adjust packets for ordinary
// play — while a multi-megabyte paste still gets its window back long
// before the client could stall (it always has ≥1MB to send).
const adjustFloor = 1 << 20

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
		// Complete the wire teardown BEFORE closing done: serveConn
		// closes the connection once done is signaled, and closing with
		// writes in flight turns the FIN into an RST.
		// A well-behaved session ends with an exit status (ssh(1) exits
		// 255 without one), then EOF and the channel close.
		es := &buf{}
		es.u8(msgChannelRequest)
		es.u32(0)
		es.cstr("exit-status")
		es.boolean(false)
		es.u32(0)
		c.t.writePacket(es.b)
		c.t.writePacket([]byte{msgChannelEOF, 0, 0, 0, 0})
		w := &buf{}
		w.u8(msgChannelClose)
		w.u32(0)
		c.t.writePacket(w.b)
		close(c.done)
		// Give the client a moment to process the channel close before
		// the FIN — slamming TCP shut at the same instant makes ssh(1)
		// report "connection closed by remote host" instead of exiting
		// cleanly. If the client closes first, the reader notices.
		go func() {
			time.Sleep(300 * time.Millisecond)
			c.conn.Close()
		}()
	})
	return nil
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

// conn is one client connection past the initial handshake.
type conn struct {
	t     *transport
	srv   *Server
	ch    *channel // the single session channel, nil until opened
	queue [][]byte // packets buffered across a rekey

	// authFails counts failed userauth attempts; reader-goroutine
	// only, capped at maxAuthTries.
	authFails int

	// Control lane: the reader goroutine never writes to the socket
	// once the session may be producing frames. A synchronous write
	// there — a window adjust, a keepalive reply — would queue behind
	// the wmu-held frame chunk stuck on a full kernel send buffer (up
	// to writeTimeoutSec), stalling every later inbound packet too:
	// keystrokes, the client's own window adjusts, disconnects.
	// Replies go through ctlQ, owed adjusts through ch.owe (both
	// lossless — a dropped adjust would wedge the client after 2MB, a
	// dropped ChannelSuccess hangs ssh(1) on a want-reply request),
	// both sent by pumpControl. Input handling stays pure: read → feed.
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

// ListenAndServe runs the server until the listener fails.
func (s *Server) ListenAndServe() error {
	if err := s.init(); err != nil {
		return err
	}
	ln, err := net.Listen("tcp", s.Addr)
	if err != nil {
		return err
	}
	defer ln.Close()
	s.Log.Printf("game server on %s (host key %s)", ln.Addr(), s.hk.fingerprint())
	return s.Serve(ln)
}

// init fills defaults and loads the host key.
func (s *Server) init() error {
	if s.Handler == nil {
		return errors.New("sshd: no Handler configured")
	}
	if s.MaxSessions <= 0 {
		s.MaxSessions = 16
	}
	if s.QueueTimeout <= 0 {
		s.QueueTimeout = defaultQueueTimeout
	}
	if s.PostAuthWait <= 0 {
		s.PostAuthWait = defaultPostAuthWait
	}
	if s.MaxQueue == 0 {
		s.MaxQueue = defaultMaxQueue
	}
	if s.Log == nil {
		s.Log = log.New(log.Writer(), "ssh: ", log.LstdFlags|log.Lmsgprefix)
	}
	if s.adm == nil {
		s.adm = newAdmission()
	}
	if s.hk == nil {
		hk, err := s.loadHostKey()
		if err != nil {
			return err
		}
		s.hk = hk
	}
	return nil
}

// Serve accepts connections on ln until it fails.
func (s *Server) Serve(ln net.Listener) error {
	if err := s.init(); err != nil {
		return err
	}
	for {
		nc, err := ln.Accept()
		if err != nil {
			return err
		}
		if tc := nc.(*net.TCPConn); tc != nil {
			tc.SetKeepAlive(true)
			tc.SetKeepAlivePeriod(15 * time.Second)
			// Control packets (window adjusts, keepalive and request
			// replies) interleave with 256KB frame chunks on the same
			// stream; Nagle would hold the small packet until the big
			// one is ACKed — exactly the wrong priority order.
			tc.SetNoDelay(true)
		}
		if !s.adm.room(s.MaxSessions, s.MaxQueue) {
			// No room to play or even to wait: refuse politely, in the
			// clear, before any crypto work.
			fmt.Fprintf(nc, "%s\r\n", serverVersion)
			writePlaintextDisconnect(nc, discTooManyConns, "too many connections")
			nc.Close()
			continue
		}
		go s.serveConn(nc)
	}
}

func writePlaintextDisconnect(w net.Conn, code uint32, msg string) {
	b := &buf{}
	b.u8(msgDisconnect)
	b.u32(code)
	b.cstr(msg)
	b.cstr("")
	pad := plainBlock - (5+len(b.b))%plainBlock
	if pad < 4 {
		pad += plainBlock
	}
	var pkt []byte
	pkt = binary.BigEndian.AppendUint32(pkt, uint32(1+len(b.b)+pad))
	pkt = append(pkt, byte(pad))
	pkt = append(pkt, b.b...)
	pkt = append(pkt, make([]byte, pad)...)
	w.Write(pkt)
}
func (s *Server) loadHostKey() (*hostKey, error) {
	if s.HostKeyFile == "" {
		return generateHostKey()
	}
	if data, err := os.ReadFile(s.HostKeyFile); err == nil {
		lines := splitLines(string(data))
		if len(lines) >= 2 {
			seed, err := hex.DecodeString(lines[1])
			if err == nil && len(seed) == ed25519.SeedSize {
				return &hostKey{priv: ed25519.NewKeyFromSeed(seed)}, nil
			}
		}
		return nil, fmt.Errorf("sshd: malformed host key file %s", s.HostKeyFile)
	}
	hk, err := generateHostKey()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(s.HostKeyFile), 0o700); err != nil {
		return nil, err
	}
	body := "mario-sshd host key v1\n" + hex.EncodeToString(hk.priv.Seed()) + "\n"
	if err := os.WriteFile(s.HostKeyFile, []byte(body), 0o600); err != nil {
		return nil, err
	}
	return hk, nil
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i <= len(s); i++ {
		if i == len(s) || s[i] == '\n' {
			if i > start {
				out = append(out, s[start:i])
			}
			start = i + 1
		}
	}
	return out
}

// serveConn runs one connection to completion.
func (s *Server) serveConn(nc net.Conn) {
	defer nc.Close()
	c := &conn{
		t:         newTransport(nc),
		srv:       s,
		ctlWake:   make(chan struct{}, 1),
		adjustSig: make(chan *channel, 1),
		dead:      make(chan struct{}),
	}
	defer close(c.dead)

	// Bound the pre-auth phase against slowloris-style stalls.
	nc.SetDeadline(deadlineAfter(30))
	if err := c.t.exchangeVersion(serverVersion); err != nil {
		return
	}
	// The first packet must be the client's KEXINIT.
	p, err := c.t.readPacket()
	if err != nil || len(p) == 0 || p[0] != msgKexinit {
		return
	}
	if err := c.handshake(p); err != nil {
		s.Log.Printf("connection from %s: %v", nc.RemoteAddr(), err)
		return
	}

	// Authenticated (by not authenticating). Clear the handshake deadline
	// and start the control lane — from here on the reader goroutine
	// never writes to the socket (see conn's control-lane comment).
	// The write side stays unbounded, but the read side keeps one last
	// deadline: a connection that never opens its session channel holds
	// a goroutine and a transport while being invisible to the session
	// cap (admission counts playing sessions only), so it must not be
	// allowed to idle forever. openChannel disarms this once a session
	// exists.
	nc.SetDeadline(time.Time{})
	nc.SetReadDeadline(time.Now().Add(s.PostAuthWait))
	go c.pumpControl()

	if err := c.loop(); err != nil {
		s.Log.Printf("session %s: %v", nc.RemoteAddr(), err)
	}
	if c.ch != nil {
		// The reader is done: unblock the handler's writes, close the
		// channel (idempotent) and wait for the handler to drain out.
		c.ch.shutdown()
		<-c.ch.done
	}
}

// handshake performs the initial key exchange.
func (c *conn) handshake(clientKexinit []byte) error {
	if err := kexinitOffers(clientKexinit); err != nil {
		return err
	}
	c.t.ic = clientKexinit
	c.t.is = buildKexinit()
	if err := c.t.writePacket(c.t.is); err != nil {
		return err
	}
	q, err := c.t.serverKex(c.srv.hk)
	c.queue = append(c.queue, q...)
	return err
}

// rekey responds to a mid-session KEXINIT.
func (c *conn) rekey(clientKexinit []byte) error {
	if err := kexinitOffers(clientKexinit); err != nil {
		return err
	}
	c.t.ic = clientKexinit
	c.t.is = buildKexinit()
	if err := c.t.writePacket(c.t.is); err != nil {
		return err
	}
	q, err := c.t.serverKex(c.srv.hk)
	c.queue = append(q, c.queue...)
	return err
}

// nextPacket pulls buffered rekey-overflow packets first.
func (c *conn) nextPacket() ([]byte, error) {
	if len(c.queue) > 0 {
		p := c.queue[0]
		c.queue = c.queue[1:]
		return p, nil
	}
	return c.t.readPacket()
}

func (c *conn) loop() error {
	authed := false
	for {
		p, err := c.nextPacket()
		if err != nil {
			if c.ch == nil && errors.Is(err, os.ErrDeadlineExceeded) {
				// The post-auth channel-open deadline (see serveConn).
				return c.dropIdle()
			}
			return err
		}
		if len(p) == 0 {
			continue
		}
		switch p[0] {
		case msgDisconnect:
			if c.ch != nil {
				c.ch.shutdown()
			}
			return nil
		case msgIgnore, msgDebug, msgUnimplemented, msgExtInfo:
		case msgKexinit:
			if err := c.rekey(p); err != nil {
				return fmt.Errorf("rekey: %w", err)
			}
		case msgServiceRequest:
			r := &reader{b: p[1:]}
			svc := string(r.str())
			if !r.ok() || svc != "ssh-userauth" {
				return fmt.Errorf("unknown service %q", svc)
			}
			w := &buf{}
			w.u8(msgServiceAccept)
			w.cstr(svc)
			if err := c.t.writePacket(w.b); err != nil {
				return err
			}
		case msgUserauthRequest:
			r := &reader{b: p[1:]}
			r.str() // username — anyone may play
			r.str() // service
			method := string(r.str())
			if !r.ok() {
				return errors.New("malformed userauth request")
			}
			switch method {
			case "none":
				if !authed {
					authed = true
					c.sendBanner()
				}
				if err := c.t.writePacket([]byte{msgUserauthSuccess}); err != nil {
					return err
				}
			default:
				c.authFails++
				if c.authFails > maxAuthTries {
					// Written here, on the reader goroutine: no session
					// exists while auth is failing, so the wire is quiet
					// and the control lane has nothing to protect.
					w := &buf{}
					w.u8(msgDisconnect)
					w.u32(discProtocolError)
					w.cstr("too many failed authentication attempts")
					w.cstr("")
					if err := c.t.writePacket(w.b); err != nil {
						return err
					}
					return errors.New("too many failed authentication attempts")
				}
				w := &buf{}
				w.u8(msgUserauthFailure)
				w.cstr("none")
				w.boolean(false)
				if err := c.t.writePacket(w.b); err != nil {
					return err
				}
			}
		case msgGlobalRequest:
			r := &reader{b: p[1:]}
			name := string(r.str())
			wantReply := r.boolean()
			// Only OpenSSH keepalives are honored; everything else —
			// port forwarding, streamlocal, hostkeys-prove — is refused.
			if wantReply {
				code := byte(msgRequestFailure)
				if name == "keepalive@openssh.com" {
					code = msgRequestSuccess
				}
				c.writeControlDrop([]byte{code})
			}
		case msgChannelOpen:
			if err := c.openChannel(p); err != nil {
				return err
			}
		case msgWindowAdjust:
			r := &reader{b: p[1:]}
			_ = r.u32() // recipient
			add := r.u32()
			if c.ch != nil && r.ok() {
				c.ch.mu.Lock()
				c.ch.window += int(add)
				c.ch.cond.Broadcast()
				c.ch.mu.Unlock()
			}
		case msgChannelData:
			r := &reader{b: p[1:]}
			_ = r.u32()
			data := r.str()
			rawLen := len(data)
			if c.ch != nil && r.ok() {
				// Input is the real-time path: bytes reach the game the
				// moment they are read, before any protocol bookkeeping.
				// Nothing here may wait on a socket write — see the
				// control-lane notes on conn.
				c.ch.mu.Lock()
				feed := c.ch.feed
				probe := c.ch.probe
				c.ch.mu.Unlock()
				if probe != nil {
					// The color-depth probe (termprobe.go) buffers bytes
					// until the session installs its feed, then strips
					// any late DA2/DA3 replies from passing input.
					rest, buffered := probe.offer(data)
					if buffered {
						data = nil
					} else {
						data = rest
					}
				}
				if feed != nil && len(data) > 0 {
					feed(data)
				}
				// Then replenish the receive window we advertised at
				// channel open, off this goroutine: every consumed byte
				// (probe-buffered and stripped ones included — the client
				// spent window on them all) is owed back. Adjusts
				// coalesce and ride the control pump once they reach
				// half the window; without them a window-tracking client
				// (ssh is) goes input-silent after 2MB of cumulative
				// input — an hour of held keys, or a few big pastes.
				// The signal carries the channel pointer so the pump
				// never reads c.ch itself.
				if rawLen > 0 && c.ch.addOwed(rawLen) {
					select {
					case c.adjustSig <- c.ch:
					default:
					}
				}
			}
		case msgChannelRequest:
			if err := c.channelRequest(p); err != nil {
				return err
			}
		case msgChannelEOF:
			// The client's stdin is done (e.g. piped input under ssh -tt):
			// no more keystrokes, but the session lives on until the
			// channel actually closes or the player quits.
		case msgChannelClose:
			if c.ch != nil {
				c.ch.shutdown()
				return nil
			}
		default:
			// Unknown messages are ignored per RFC 4253 §11.
		}
	}
}

func (c *conn) sendBanner() {
	w := &buf{}
	w.u8(msgUserauthBanner)
	w.cstr("\r\n  SUPER CLI MARIO — the unauthenticated game server.\r\n" +
		"  Any username works. Arrows/WASD move, space jumps, q quits.\r\n" +
		"  There is no shell here: the game is the whole host.\r\n")
	w.cstr("")
	c.t.writePacket(w.b)
}

func (c *conn) openChannel(p []byte) error {
	r := &reader{b: p[1:]}
	kind := string(r.str())
	peerID := r.u32()
	window := r.u32()
	maxPkt := r.u32()
	if !r.ok() {
		return errors.New("malformed channel open")
	}
	w := &buf{}
	if kind != "session" || c.ch != nil {
		w.u8(msgChannelOpenFail)
		w.u32(peerID)
		w.u32(4) // resource shortage / one session per connection
		w.cstr("this server hosts one game session per connection")
		w.cstr("")
		return c.t.writePacket(w.b)
	}
	if window > 1<<30 {
		window = 1 << 30
	}
	if maxPkt == 0 || maxPkt > maxPayloadLen {
		maxPkt = maxPayloadLen
	}
	c.ch = &channel{
		t:      c.t,
		srv:    c.srv,
		conn:   c.t.conn,
		peerID: peerID,
		window: int(window),
		maxPkt: int(maxPkt),
		cols:   80,
		rows:   24,
		env:    map[string]string{},
		wait:   c.srv.ProbeWait,
		done:   make(chan struct{}),
	}
	c.ch.cond = sync.NewCond(&c.ch.mu)

	// A session channel exists: the post-auth read deadline (see
	// serveConn) has done its job — from here the connection's life is
	// the session's (queue timeout, client close, transport error).
	c.t.conn.SetReadDeadline(time.Time{})

	w.u8(msgChannelOpenConf)
	w.u32(peerID)
	w.u32(0)             // our channel id
	w.u32(1 << 21)       // our receive window (generous: keystrokes only)
	w.u32(maxPayloadLen) // our max packet size
	return c.t.writePacket(w.b)
}

// dropIdle ends a connection whose post-auth channel-open deadline
// (Server.PostAuthWait, armed in serveConn) expired: KEX is done but no
// session channel was ever opened, so the connection holds a goroutine
// and a transport while the session cap cannot see it. The client gets
// a proper DISCONNECT rather than a bare TCP teardown.
func (c *conn) dropIdle() error {
	w := &buf{}
	w.u8(msgDisconnect)
	w.u32(discByApplication)
	w.cstr("no session channel opened in time")
	w.cstr("")
	// Direct write on the reader goroutine: no session ever started,
	// the wire is quiet and the control lane has nothing to protect.
	c.t.writePacket(w.b)
	return errors.New("idle connection dropped: no session channel before PostAuthWait")
}

func (c *conn) channelRequest(p []byte) error {
	r := &reader{b: p[1:]}
	_ = r.u32() // recipient channel
	kind := string(r.str())
	wantReply := r.boolean()
	if !r.ok() || c.ch == nil {
		return nil
	}
	reply := func(ok bool) {
		if !wantReply {
			return
		}
		w := &buf{}
		if ok {
			w.u8(msgChannelSuccess)
		} else {
			w.u8(msgChannelFailure)
		}
		w.u32(0) // recipient channel (our id, as sent in the confirmation)
		// Replies ride the control pump: writing here would put the
		// reader behind a frame chunk stuck on a full kernel buffer.
		c.writeControl(w.b)
	}
	switch kind {
	case "pty-req":
		term := string(r.str())
		cols := r.u32()
		rows := r.u32()
		r.u32() // pixel width
		r.u32() // pixel height
		r.str() // terminfo blob
		if !r.ok() {
			return errors.New("malformed pty-req")
		}
		c.ch.mu.Lock()
		if c.ch.term == "" {
			c.ch.term = term
		}
		if cols > 0 {
			c.ch.cols = int(min(cols, 10000))
		}
		if rows > 0 {
			c.ch.rows = int(min(rows, 10000))
		}
		c.ch.mu.Unlock()
		reply(true)
		return nil
	case "env":
		name := string(r.str())
		val := string(r.str())
		c.ch.mu.Lock()
		if len(c.ch.env) < 64 && len(name) <= 256 && len(val) <= 1024 {
			c.ch.env[name] = val
		}
		c.ch.mu.Unlock()
		reply(true)
		return nil
	case "window-change":
		cols := r.u32()
		rows := r.u32()
		if r.ok() {
			c.ch.mu.Lock()
			// Zero fields mean "unknown" (RFC 4254) and are ignored;
			// the host is notified only when the size really changed.
			changed := false
			if cols > 0 && int(min(cols, 10000)) != c.ch.cols {
				c.ch.cols = int(min(cols, 10000))
				changed = true
			}
			if rows > 0 && int(min(rows, 10000)) != c.ch.rows {
				c.ch.rows = int(min(rows, 10000))
				changed = true
			}
			var cb func(cols, rows int)
			var cc, rr int
			if changed {
				cb, cc, rr = c.ch.onResize, c.ch.cols, c.ch.rows
			}
			c.ch.mu.Unlock()
			if cb != nil {
				cb(cc, rr)
			}
		}
		return nil
	case "shell":
		c.ch.mu.Lock()
		start := !c.ch.shelled
		c.ch.shelled = true
		c.ch.mu.Unlock()
		if !start {
			reply(false)
			return nil
		}
		reply(true)
		// Pin the queued success ahead of the handler's first output
		// (the probe query below): the sync waits on the pump, never on
		// the socket, and the wire is quiet this early.
		c.syncControl()
		// Ask the client's terminal to identify itself (DA2+DA3); the
		// replies set the session's color depth (termprobe.go). Shell
		// sessions only: the mosh wrapper runs its ssh with -n (stdin
		// from /dev/null), so a reply can never come back through a
		// handshake — don't send the query or pay the wait there.
		// Queries draw nothing on screen. The field write is locked to
		// match every other access (the data loop on this same goroutine
		// is sequential with it, and the handler goroutines only spawn
		// after — but don't leave an unlocked accessor behind).
		c.ch.mu.Lock()
		needProbe := c.ch.probe == nil && c.ch.term != ""
		if needProbe {
			c.ch.probe = newTermProbe(c.ch.wait)
		}
		c.ch.mu.Unlock()
		sess := &Session{ch: c.ch}
		go func() {
			defer func() {
				if r := recover(); r != nil {
					c.srv.Log.Printf("handler panic: %v", r)
					c.ch.shutdown()
				}
			}()
			// The query write rides the session channel from this
			// goroutine, off the conn reader (which must never wait on
			// a socket write once frames may be flowing); ch.write can
			// block on the channel window and takes ch.mu itself.
			if needProbe {
				if _, err := c.ch.write([]byte(termQuery)); err != nil {
					c.ch.shutdown()
					return
				}
			}
			admitted, err := c.srv.adm.enter(sess, c.srv.MaxSessions, c.srv.MaxQueue, c.srv.QueueTimeout, nil)
			if err != nil {
				// Refused, gave up or disconnected while waiting.
				c.ch.shutdown()
				return
			}
			c.srv.Handler(sess)
			c.srv.adm.exit(time.Since(admitted))
			c.ch.shutdown()
		}()
		return nil
	case "signal":
		reply(true)
		return nil
	case "exec":
		// The one exec the server ever runs: the mosh handshake.
		// "mosh-server new ..." is strictly validated and rebuilt with
		// our own command and port range; anything else is refused
		// exactly like before. Every attempt is logged — this is the
		// one place a client can ask this server to run something.
		cmdline := string(r.str())
		c.srv.Log.Printf("exec request: %q", cmdline)
		if c.srv.MoshBin != "" {
			if req, ok := parseMoshArgv(cmdline); ok {
				c.ch.mu.Lock()
				started := c.ch.execStarted
				c.ch.execStarted = true
				c.ch.mu.Unlock()
				if started {
					reply(false)
					return nil
				}
				reply(true)
				// Same ordering pin as shell: the CONNECT line
				// startMosh writes must follow the exec success.
				c.syncControl()
				// Off the conn goroutine: startMosh waits for the
				// color probe's reply, and this loop is what delivers
				// inbound CHANNEL_DATA to the probe.
				go func() {
					if err := c.srv.startMosh(c, req); err != nil {
						c.srv.Log.Printf("mosh: %v", err)
					}
				}()
				return nil
			}
		}
		reply(false)
		return nil
	default:
		// subsystem, x11, agent…: nothing here but the game.
		reply(false)
	}
	return nil
}
