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
// client, e.g. app.Feed).
func (s *Session) OnFeed(f func([]byte)) {
	s.ch.mu.Lock()
	s.ch.feed = f
	s.ch.mu.Unlock()
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

var errChannelClosed = errors.New("sshd: channel closed")

type channel struct {
	t    *transport
	conn net.Conn

	mu         sync.Mutex
	cond       *sync.Cond
	peerID     uint32
	window     int // client's receive window for our CHANNEL_DATA
	maxPkt     int // client's max packet size
	feed       func([]byte)
	onResize   func(cols, rows int)
	term       string
	cols       int
	rows       int
	env        map[string]string
	shelled    bool // shell request seen
	remoteGone bool
	sendErr    bool

	closeOnce sync.Once
	done      chan struct{}
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
	c := &conn{t: newTransport(nc), srv: s}

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

	// Authenticated (by not authenticating). Clear the handshake deadline.
	nc.SetDeadline(time.Time{})

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
				if err := c.t.writePacket([]byte{code}); err != nil {
					return err
				}
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
			if c.ch != nil && r.ok() {
				// The bytes are consumed the moment they are fed, so
				// replenish the receive window we advertised at channel
				// open: without adjusts a client tracking the window
				// (ssh does) goes input-silent after ~2MB of cumulative
				// keystrokes — an hour of held keys, or a few big pastes.
				// One tiny adjust per keystroke packet keeps it exact.
				if len(data) > 0 {
					w := &buf{}
					w.u8(msgWindowAdjust)
					w.u32(0)
					w.u32(uint32(len(data)))
					if err := c.t.writePacket(w.b); err != nil {
						return err
					}
				}
				c.ch.mu.Lock()
				feed := c.ch.feed
				c.ch.mu.Unlock()
				if feed != nil {
					feed(data)
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
		conn:   c.t.conn,
		peerID: peerID,
		window: int(window),
		maxPkt: int(maxPkt),
		cols:   80,
		rows:   24,
		env:    map[string]string{},
		done:   make(chan struct{}),
	}
	c.ch.cond = sync.NewCond(&c.ch.mu)

	w.u8(msgChannelOpenConf)
	w.u32(peerID)
	w.u32(0)             // our channel id
	w.u32(1 << 21)       // our receive window (generous: keystrokes only)
	w.u32(maxPayloadLen) // our max packet size
	return c.t.writePacket(w.b)
}

func (c *conn) channelRequest(p []byte) error {
	r := &reader{b: p[1:]}
	_ = r.u32() // recipient channel
	kind := string(r.str())
	wantReply := r.boolean()
	if !r.ok() || c.ch == nil {
		return nil
	}
	reply := func(ok bool) error {
		if !wantReply {
			return nil
		}
		w := &buf{}
		if ok {
			w.u8(msgChannelSuccess)
		} else {
			w.u8(msgChannelFailure)
		}
		w.u32(0) // recipient channel (our id, as sent in the confirmation)
		return c.t.writePacket(w.b)
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
		return reply(true)
	case "env":
		name := string(r.str())
		val := string(r.str())
		c.ch.mu.Lock()
		if len(c.ch.env) < 64 && len(name) <= 256 && len(val) <= 1024 {
			c.ch.env[name] = val
		}
		c.ch.mu.Unlock()
		return reply(true)
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
			return reply(false)
		}
		if err := reply(true); err != nil {
			return err
		}
		sess := &Session{ch: c.ch}
		go func() {
			defer func() {
				if r := recover(); r != nil {
					c.srv.Log.Printf("handler panic: %v", r)
					c.ch.shutdown()
				}
			}()
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
		return reply(true)
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
				if err := reply(true); err != nil {
					return err
				}
				if err := c.srv.startMosh(c, req); err != nil {
					c.srv.Log.Printf("mosh: %v", err)
				}
				return nil
			}
		}
		return reply(false)
	default:
		// subsystem, x11, agent…: nothing here but the game.
		return reply(false)
	}
}
