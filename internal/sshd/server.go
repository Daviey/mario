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

	// MoshMax caps concurrently running mosh servers (default 16).
	// Separate from MaxSessions on purpose: the admission queue caps
	// shell sessions, but a mosh child outlives its SSH connection, so
	// it holds no session slot — without its own cap a reconnect loop
	// could stack unbounded children. Tests shrink it to probe the
	// boundary.
	MoshMax int

	hk  *hostKey
	adm *admission
}

// defaultPostAuthWait is the fallback for Server.PostAuthWait: how long
// a post-KEX connection may sit with no session channel (see there).
const defaultPostAuthWait = 60 * time.Second

// maxAuthTries caps failed userauth attempts per connection. Auth is a
// formality here — method "none" is the only success — so a client
// grinding out failures is pure abuse; past the cap it gets a
// DISCONNECT instead of an infinite reply loop.
const maxAuthTries = 20

// conn is one client connection past the initial handshake.
type conn struct {
	t     *transport
	srv   *Server
	ch    *channel // the single session channel, nil until opened
	queue [][]byte // packets buffered across a rekey

	// authFails counts failed userauth attempts; reader-goroutine
	// only, capped at maxAuthTries.
	authFails int

	// controlLane carries every post-session control write (see
	// control.go for the model).
	controlLane
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
	if s.MoshMax <= 0 {
		s.MoshMax = defaultMoshMax
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
		if tc, ok := nc.(*net.TCPConn); ok {
			// Best-effort socket tuning: the errors are ignored — a
			// failed keepalive or nodelay only degrades, never breaks,
			// the session.
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
		t:           newTransport(nc),
		srv:         s,
		controlLane: newControlLane(),
	}
	defer close(c.dead)

	// Bound the pre-auth phase against slowloris-style stalls.
	nc.SetDeadline(time.Now().Add(30 * time.Second))
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
	// never writes to the socket (see control.go).
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
		// The teardown burst rides the control lane; give the pump a
		// bounded beat to put it on the wire before the deferred conn
		// close takes the socket away (the handler-initiated path has
		// the FIN grace for this; the reader-initiated one ends here).
		c.drainControl(finGrace)
	}
}

// serverKexStep validates the client's KEXINIT, answers with our own,
// and runs one key exchange, returning the packets the client sent
// mid-exchange (they were consumed off the wire under the old-key
// rules and must be reprocessed by loop once the new keys are in).
func (c *conn) serverKexStep(clientKexinit []byte) ([][]byte, error) {
	if err := kexinitOffers(clientKexinit); err != nil {
		return nil, err
	}
	c.t.ic = clientKexinit
	c.t.is = buildKexinit()
	if err := c.t.writePacket(c.t.is); err != nil {
		return nil, err
	}
	return c.t.serverKex(c.srv.hk)
}

// handshake performs the initial key exchange. The overflow (if any)
// lands after anything already buffered — at the initial exchange the
// queue is empty, so this is simply append.
func (c *conn) handshake(clientKexinit []byte) error {
	q, err := c.serverKexStep(clientKexinit)
	c.queue = append(c.queue, q...)
	return err
}

// rekey responds to a mid-session KEXINIT. The fresh overflow goes
// ahead of any packets still buffered from an earlier exchange (the
// order differs from handshake's append; both coincide in practice,
// because loop drains the queue before it can read a fresh KEXINIT —
// the order is only observable when a client pipelines a KEXINIT
// into a previous exchange's tail).
func (c *conn) rekey(clientKexinit []byte) error {
	q, err := c.serverKexStep(clientKexinit)
	c.queue = append(q, c.queue...)
	return err
}

// nextPacket pulls buffered rekey-overflow packets first: the queued
// overflow replays before any fresh read once the new keys are in.
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
			// Direct write, reader goroutine: the wire is quiet
			// pre-session and the control pump is idle — nothing to
			// protect yet.
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
				// Direct write, reader goroutine: pre-session, wire
				// quiet, pump idle (as above).
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
				// Direct write, reader goroutine: pre-session, wire
				// quiet, pump idle (as above).
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
			if c.ch != nil && r.ok() {
				c.deliverInput(data)
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

// deliverInput is the real-time path: bytes reach the game the moment
// they are read, before any protocol bookkeeping. Nothing here may
// wait on a socket write — see the control-lane notes in control.go.
func (c *conn) deliverInput(data []byte) {
	rawLen := len(data)
	c.ch.mu.Lock()
	feed := c.ch.feed
	probe := c.ch.probe
	c.ch.mu.Unlock()
	if probe != nil {
		// The color-depth probe (termprobe.go) buffers bytes until the
		// session installs its feed, then strips any late DA2/DA3
		// replies from passing input.
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
	// Then replenish the receive window we advertised at channel open,
	// off this goroutine: every consumed byte (probe-buffered and
	// stripped ones included — the client spent window on them all) is
	// owed back. Adjusts coalesce and ride the control pump once they
	// reach half the window; without them a window-tracking client
	// (ssh is) goes input-silent after 2MB of cumulative input — an
	// hour of held keys, or a few big pastes. The signal carries the
	// channel pointer so the pump never reads c.ch itself.
	if rawLen > 0 && c.ch.addOwed(rawLen) {
		select {
		case c.adjustSig <- c.ch:
		default:
		}
	}
}

// sendBanner writes the flavor-text banner after auth. The write error
// is swallowed on purpose: losing the banner is purely cosmetic, and
// the auth SUCCESS that follows carries the real result. Direct write,
// reader goroutine: pre-session, wire quiet, pump idle.
func (c *conn) sendBanner() {
	w := &buf{}
	w.u8(msgUserauthBanner)
	w.cstr("\r\n  SUPER CLI MARIO — the unauthenticated game server.\r\n" +
		"  Any username works. Arrows/WASD move, space jumps, hold X to run, q quits.\r\n" +
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
		lane:   c,
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

	// Direct write, reader goroutine: this is the last packet written
	// before the session may start producing frames — the wire is
	// still quiet and the pump idle.
	w.u8(msgChannelOpenConf)
	w.u32(peerID)
	w.u32(0)             // our channel id
	w.u32(rxWindow)      // our receive window (generous: keystrokes only)
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

// env request bounds: the 65th variable (or an oversized name/value)
// is refused outright rather than silently accepted-and-dropped.
const (
	maxEnvVars = 64
	maxEnvName = 256
	maxEnvVal  = 1024
)

func (c *conn) channelRequest(p []byte) error {
	r := &reader{b: p[1:]}
	_ = r.u32() // recipient channel
	kind := string(r.str())
	wantReply := r.boolean()
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
	if !r.ok() || c.ch == nil {
		// Malformed payload, or a request for a channel that does not
		// exist: fail it, never silence it — ssh(1) waits forever on a
		// want-reply request that gets no answer.
		reply(false)
		return nil
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
		if !r.ok() {
			// Truncated payload: refuse rather than store a
			// half-parsed variable and report success.
			reply(false)
			return nil
		}
		c.ch.mu.Lock()
		ok := len(c.ch.env) < maxEnvVars && len(name) <= maxEnvName && len(val) <= maxEnvVal
		if ok {
			c.ch.env[name] = val
		}
		c.ch.mu.Unlock()
		if !ok {
			// The cap is a real refusal, not a silent drop: the client
			// learns its variable did not land.
			c.srv.Log.Printf("session %s: env request %q refused (cap %d vars, %d/%d byte limits)",
				c.t.conn.RemoteAddr(), name, maxEnvVars, maxEnvName, maxEnvVal)
			reply(false)
			return nil
		}
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
		return c.startShellSession(reply)
	case "signal":
		reply(true)
		return nil
	case "exec":
		return c.startMoshSession(reply, string(r.str()))
	default:
		// subsystem, x11, agent…: nothing here but the game.
		reply(false)
	}
	return nil
}

// startShellSession serves the "shell" request: one game per channel.
// The success reply is pinned ahead of the handler's first output —
// syncControl waits on the pump, never on the socket, and the wire is
// quiet this early (its twin call sits in startMoshSession).
func (c *conn) startShellSession(reply func(bool)) error {
	c.ch.mu.Lock()
	start := !c.ch.shelled
	c.ch.shelled = true
	c.ch.mu.Unlock()
	if !start {
		reply(false)
		return nil
	}
	reply(true)
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
}

// startMoshSession serves the "exec" request — the one exec the server
// ever runs: the mosh handshake. "mosh-server new ..." is strictly
// validated and rebuilt with our own command and port range; anything
// else is refused. Every attempt is logged — this is the one place a
// client can ask this server to run something. The MoshMax child slot
// is claimed BEFORE the request is answered, so a refused client sees
// a CHANNEL_FAILURE instead of a silent yes.
func (c *conn) startMoshSession(reply func(bool), cmdline string) error {
	c.srv.Log.Printf("exec request: %q", cmdline)
	if c.srv.MoshBin == "" {
		reply(false)
		return nil
	}
	req, ok := parseMoshArgv(cmdline)
	if !ok {
		reply(false)
		return nil
	}
	c.ch.mu.Lock()
	started := c.ch.execStarted
	c.ch.execStarted = true
	c.ch.mu.Unlock()
	if started {
		reply(false)
		return nil
	}
	if !c.srv.acquireMoshSlot() {
		c.srv.Log.Printf("mosh: refusing handshake, %d children running (cap %d)",
			RunningMosh(), c.srv.MoshMax)
		reply(false)
		return nil
	}
	reply(true)
	// Same ordering pin as shell: the CONNECT line startMosh writes
	// must follow the exec success.
	c.syncControl()
	// Off the conn goroutine: startMosh waits for the color probe's
	// reply, and this loop is what delivers inbound CHANNEL_DATA to
	// the probe.
	go func() {
		if err := c.srv.startMosh(c, req); err != nil {
			c.srv.Log.Printf("mosh: %v", err)
		}
	}()
	return nil
}
