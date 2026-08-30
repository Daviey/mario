package sshd

// Full-protocol tests against a real listener, driven by an in-package
// client that mirrors the handshake from the other side (its own curve25519
// key, its own exchange-hash computation, independent host-key signature
// verification). This cross-checks the server's framing, KDF and sequence
// handling against a second implementation of the same RFCs.

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const clientVersion = "SSH-2.0-GoMarioTest_1.0"

type testClient struct {
	t         *testing.T
	nc        net.Conn
	tr        *transport
	sessionID []byte
	hadPty    bool
}

// startServer boots a Server on its own listener; srv.Addr becomes the
// bound address. Optional pre functions configure the Server before it
// starts — mutating Server fields after Serve is running is a data race
// (Serve's init reads them).
func startServer(t *testing.T, handler func(*Session), pre ...func(*Server)) *Server {
	t.Helper()
	srv := &Server{
		Handler: handler,
		Log:     log.New(io.Discard, "", 0),
	}
	for _, f := range pre {
		f(srv)
	}
	if err := srv.init(); err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv.Addr = ln.Addr().String()
	t.Cleanup(func() { ln.Close() })
	go srv.Serve(ln)
	return srv
}

func dial(t *testing.T, addr string) *testClient {
	t.Helper()
	nc, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { nc.Close() })
	tc := &testClient{t: t, nc: nc, tr: newTransport(nc)}
	if err := tc.tr.exchangeVersion(clientVersion); err != nil {
		t.Fatalf("version exchange: %v", err)
	}
	tc.exchange(buildKexinit(), true)
	return tc
}

// exchange runs the client side of a key exchange (initial or rekey) on
// tc's transport, verifying the host key signature and swapping cipher
// state with the client's key roles.
func (tc *testClient) exchange(ic []byte, first bool) {
	t := tc.t
	if err := tc.tr.writePacket(ic); err != nil {
		t.Fatalf("send kexinit: %v", err)
	}

	p := tc.read()
	if len(p) == 0 || p[0] != msgKexinit {
		t.Fatalf("expected server KEXINIT, got %v", p)
	}
	if err := kexinitOffers(p); err != nil {
		t.Fatalf("server kexinit: %v", err)
	}
	is := p

	curve := ecdh.X25519()
	priv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	e := priv.PublicKey().Bytes()
	w := &buf{}
	w.u8(msgKexDHInit)
	w.str(e)
	if err := tc.tr.writePacket(w.b); err != nil {
		t.Fatal(err)
	}

	p = tc.read()
	if len(p) == 0 || p[0] != msgKexDHReply {
		t.Fatalf("expected KEX_REPLY, got %v", p)
	}
	r := &reader{b: p[1:]}
	ks := r.str()
	f := r.str()
	sigBlob := r.str()
	if !r.ok() {
		t.Fatal("malformed KEX_REPLY")
	}

	serverPub, err := curve.NewPublicKey(f)
	if err != nil {
		t.Fatalf("bad server public key: %v", err)
	}
	shared, err := priv.ECDH(serverPub)
	if err != nil {
		t.Fatalf("ecdh: %v", err)
	}

	// Exchange hash from the client's perspective (V_C is ours).
	h := sha256.New()
	hw := &buf{}
	hw.str([]byte(clientVersion))
	hw.str([]byte(serverVersion))
	hw.str(ic)
	hw.str(is)
	hw.str(ks)
	hw.str(e)
	hw.str(f)
	hw.mpint(shared)
	h.Write(hw.b)
	hash := h.Sum(nil)

	// Verify the host key signature over the hash.
	kr := &reader{b: ks}
	if string(kr.str()) != hostkeyAlgo {
		t.Fatal("unexpected host key algorithm")
	}
	pub := kr.str()
	sr := &reader{b: sigBlob}
	if string(sr.str()) != hostkeyAlgo {
		t.Fatal("unexpected signature algorithm")
	}
	sig := sr.str()
	if len(pub) != ed25519.PublicKeySize || len(sig) != ed25519.SignatureSize {
		t.Fatalf("bad key/sig sizes: %d/%d", len(pub), len(sig))
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), hash, sig) {
		t.Fatal("host key signature does not verify")
	}

	if first {
		tc.sessionID = hash
	}
	if err := tc.tr.writePacket([]byte{msgNewkeys}); err != nil {
		t.Fatal(err)
	}
	p = tc.read()
	if len(p) == 0 || p[0] != msgNewkeys {
		t.Fatalf("expected NEWKEYS, got %v", p)
	}
	keys := deriveKeys(shared, hash, tc.sessionID)
	enc, _ := aes.NewCipher(keys.encC)
	dec, _ := aes.NewCipher(keys.encS)
	// Sequence numbers continue across NEWKEYS (no strict-kex).
	tc.tr.out = dirState{stream: cipher.NewCTR(enc, keys.ivC), macKey: keys.macC, seq: tc.tr.out.seq}
	tc.tr.in = dirState{stream: cipher.NewCTR(dec, keys.ivS), macKey: keys.macS, seq: tc.tr.in.seq}
}

// read pulls one packet, failing the test on transport errors.
func (tc *testClient) read() []byte {
	tc.t.Helper()
	tc.nc.SetReadDeadline(time.Now().Add(5 * time.Second))
	defer tc.nc.SetReadDeadline(time.Time{})
	p, err := tc.tr.readPacket()
	if err != nil {
		tc.t.Fatalf("readPacket: %v", err)
	}
	return p
}

// expect waits for a specific message, skipping banners/noise.
func (tc *testClient) expect(code byte) []byte {
	tc.t.Helper()
	for {
		p := tc.read()
		if len(p) == 0 {
			continue
		}
		if p[0] == code {
			return p
		}
		switch p[0] {
		case msgUserauthBanner, msgIgnore, msgDebug, msgExtInfo:
		default:
			tc.t.Fatalf("expected msg %d, got %d (%v)", code, p[0], p)
		}
	}
}

func (tc *testClient) send(payload []byte) {
	tc.t.Helper()
	if err := tc.tr.writePacket(payload); err != nil {
		tc.t.Fatalf("writePacket: %v", err)
	}
}

func (tc *testClient) authNone() {
	w := &buf{}
	w.u8(msgServiceRequest)
	w.cstr("ssh-userauth")
	tc.send(w.b)
	tc.expect(msgServiceAccept)

	w = &buf{}
	w.u8(msgUserauthRequest)
	w.cstr("player") // any username
	w.cstr("ssh-connection")
	w.cstr("none")
	tc.send(w.b)
	tc.expect(msgUserauthSuccess)
}

// openSession opens a session channel with the given client-side window
// and max packet size.
func (tc *testClient) openSession(window, maxPkt uint32) {
	w := &buf{}
	w.u8(msgChannelOpen)
	w.cstr("session")
	w.u32(0) // sender (client) channel id
	w.u32(window)
	w.u32(maxPkt)
	tc.send(w.b)
	tc.expect(msgChannelOpenConf)
}

func (tc *testClient) ptyReq(cols, rows uint32) {
	tc.hadPty = true
	w := &buf{}
	w.u8(msgChannelRequest)
	w.u32(0)
	w.cstr("pty-req")
	w.boolean(true)
	w.cstr("xterm-256color")
	w.u32(cols)
	w.u32(rows)
	w.u32(0)
	w.u32(0)
	w.cstr("")
	tc.send(w.b)
	tc.expect(msgChannelSuccess)
}

func (tc *testClient) envReq(name, val string) {
	w := &buf{}
	w.u8(msgChannelRequest)
	w.u32(0)
	w.cstr("env")
	w.boolean(true)
	w.cstr(name)
	w.cstr(val)
	tc.send(w.b)
	tc.expect(msgChannelSuccess)
}

func (tc *testClient) shell() {
	w := &buf{}
	w.u8(msgChannelRequest)
	w.u32(0)
	w.cstr("shell")
	w.boolean(true)
	tc.send(w.b)
	tc.expect(msgChannelSuccess)
	// The color-depth probe follows the success with its own
	// CHANNEL_DATA (termprobe.go): consume it here so later reads see
	// only game output.
	if tc.hadPty {
		if q := tc.readData(); string(q) != termQuery {
			tc.t.Fatalf("expected term probe query after shell, got %q", q)
		}
	}
}

func (tc *testClient) winch(cols, rows uint32) {
	w := &buf{}
	w.u8(msgChannelRequest)
	w.u32(0)
	w.cstr("window-change")
	w.boolean(false)
	w.u32(cols)
	w.u32(rows)
	tc.send(w.b)
}

func (tc *testClient) sendData(b []byte) {
	w := &buf{}
	w.u8(msgChannelData)
	w.u32(0)
	w.str(b)
	tc.send(w.b)
}

// readData waits for the next CHANNEL_DATA, skipping window adjusts.
func (tc *testClient) readData() []byte {
	tc.t.Helper()
	for {
		p := tc.read()
		if len(p) == 0 {
			continue
		}
		if p[0] == msgChannelData {
			r := &reader{b: p[1:]}
			r.u32()
			data := r.str()
			if !r.ok() {
				tc.t.Fatal("malformed CHANNEL_DATA")
			}
			return data
		}
		if p[0] == msgWindowAdjust {
			continue
		}
		tc.t.Fatalf("expected CHANNEL_DATA, got msg %d", p[0])
	}
}

// echoHandler mirrors the game host shape: reports its view of the pty,
// echoes fed bytes back, lives until the session ends.
func echoHandler(s *Session) {
	var once sync.Once
	s.OnFeed(func(b []byte) {
		once.Do(func() { go s.Write(b) })
	})
	cols, rows := s.Size()
	fmt.Fprintf(s, "TERM=%s SIZE=%dx%d COLORTERM=%s", s.Term(), cols, rows, s.Env("COLORTERM"))
	<-s.Done()
}

func TestServeSessionFlow(t *testing.T) {
	srv := startServer(t, echoHandler)
	tc := dial(t, srv.Addr)
	tc.authNone()
	tc.openSession(1<<20, 32768)
	tc.ptyReq(120, 40)
	tc.envReq("COLORTERM", "truecolor")
	tc.shell()

	if got := string(tc.readData()); got != "TERM=xterm-256color SIZE=120x40 COLORTERM=truecolor" {
		t.Fatalf("handler pty report = %q", got)
	}

	tc.sendData([]byte("hi"))
	if got := string(tc.readData()); got != "hi" {
		t.Fatalf("echo = %q", got)
	}

	// Graceful end: client closes the channel, the server follows.
	w := &buf{}
	w.u8(msgChannelClose)
	w.u32(0)
	tc.send(w.b)
	// Teardown order: exit-status request, EOF, then channel close.
	p := tc.read()
	r := &reader{b: p[1:]}
	if len(p) == 0 || p[0] != msgChannelRequest || r.u32() != 0 || string(r.str()) != "exit-status" {
		t.Fatalf("expected exit-status request, got %v", p)
	}
	tc.expect(msgChannelEOF)
	tc.expect(msgChannelClose)
}

// resizeHandler reports the launch size, then every window-change the
// client sends mid-session.
func resizeHandler(s *Session) {
	cols, rows := s.Size()
	fmt.Fprintf(s, "SIZE=%dx%d", cols, rows)
	s.OnResize(func(cols, rows int) {
		fmt.Fprintf(s, "RESIZE=%dx%d", cols, rows)
	})
	<-s.Done()
}

func TestServeWindowChangeMidSession(t *testing.T) {
	srv := startServer(t, resizeHandler)
	tc := dial(t, srv.Addr)
	tc.authNone()
	tc.openSession(1<<20, 32768)
	tc.ptyReq(120, 40)
	tc.shell()
	if got := string(tc.readData()); got != "SIZE=120x40" {
		t.Fatalf("initial size report = %q", got)
	}

	tc.winch(100, 30)
	if got := string(tc.readData()); got != "RESIZE=100x30" {
		t.Fatalf("resize report = %q", got)
	}
	// Zero fields are ignored per RFC 4254 — if the server wrongly fired
	// for this one, the next read sees the stray report and fails.
	tc.winch(0, 0)
	tc.winch(80, 24)
	if got := string(tc.readData()); got != "RESIZE=80x24" {
		t.Fatalf("second resize report = %q", got)
	}

	// Graceful end, mirroring TestServeSessionFlow.
	w := &buf{}
	w.u8(msgChannelClose)
	w.u32(0)
	tc.send(w.b)
	p := tc.read()
	r := &reader{b: p[1:]}
	if len(p) == 0 || p[0] != msgChannelRequest || r.u32() != 0 || string(r.str()) != "exit-status" {
		t.Fatalf("expected exit-status request, got %v", p)
	}
	tc.expect(msgChannelEOF)
	tc.expect(msgChannelClose)
}

func TestExecAndForwardingRejected(t *testing.T) {
	srv := startServer(t, echoHandler)
	tc := dial(t, srv.Addr)
	tc.authNone()

	// Global port forwarding must be refused.
	w := &buf{}
	w.u8(msgGlobalRequest)
	w.cstr("tcpip-forward")
	w.boolean(true)
	w.cstr("127.0.0.1")
	w.u32(8080)
	tc.send(w.b)
	tc.expect(msgRequestFailure)

	// keepalive is the one honored global request.
	w = &buf{}
	w.u8(msgGlobalRequest)
	w.cstr("keepalive@openssh.com")
	w.boolean(true)
	tc.send(w.b)
	tc.expect(msgRequestSuccess)

	tc.openSession(1<<20, 32768)
	tc.ptyReq(80, 24)

	// exec is refused: no shell exists behind this server.
	w = &buf{}
	w.u8(msgChannelRequest)
	w.u32(0)
	w.cstr("exec")
	w.boolean(true)
	w.cstr("rm -rf /")
	tc.send(w.b)
	tc.expect(msgChannelFailure)

	// A second session channel on the same connection is refused too.
	w = &buf{}
	w.u8(msgChannelOpen)
	w.cstr("session")
	w.u32(1)
	w.u32(1 << 20)
	w.u32(32768)
	tc.send(w.b)
	tc.expect(msgChannelOpenFail)
}

func TestWindowFlowControl(t *testing.T) {
	const payload = 200
	srv := startServer(t, func(s *Session) {
		s.Write(make([]byte, payload)) // one big write; server must chunk
		<-s.Done()
	})
	tc := dial(t, srv.Addr)
	tc.authNone()
	tc.openSession(64, 32) // tiny window and packet cap
	tc.shell()

	got, adjusts := 0, 0
	for got < payload {
		// Read exactly what the current window allows.
		target := min(payload, 64*(adjusts+1))
		for got < target {
			d := tc.readData()
			if len(d) > 32 {
				t.Fatalf("server sent %d bytes, exceeding our 32-byte packet cap", len(d))
			}
			got += len(d)
		}

		// The window is exhausted: the server must be blocked until we
		// grant more. (A partial read here would desync the stream, but
		// that only happens on the failure path under test.)
		tc.nc.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
		if _, err := tc.tr.readPacket(); err == nil {
			t.Fatal("server sent data beyond the advertised window")
		}

		w := &buf{}
		w.u8(msgWindowAdjust)
		w.u32(0)
		w.u32(64)
		tc.send(w.b)
		adjusts++
	}
	if adjusts < 3 {
		t.Fatalf("expected several window adjusts, got %d", adjusts)
	}
}

// Keystroke data consumed by the handler must be replenished with window
// adjusts: a client that tracks our advertised window (OpenSSH does)
// stops sending channel data once it believes the window is spent, so a
// session that never adjusts goes input-dead after 2MB of cumulative
// input — an hour of held keys, or a few big pastes. Adjusts coalesce
// server-side at half the window (adjustFloor) and ride the control
// pump rather than the reader goroutine, so the contract that matters
// is accounting: every consumed byte is eventually returned, never
// invented, in amounts the client can reconcile.
func TestInputWindowReplenished(t *testing.T) {
	const chunk = 32768
	const total = 3 << 20 // 1.5× the window the server advertises
	var fed atomic.Int64
	srv := startServer(t, func(s *Session) {
		s.OnFeed(func(b []byte) { fed.Add(int64(len(b))) })
		<-s.Done()
	})
	tc := dial(t, srv.Addr)
	tc.authNone()
	tc.openSession(1<<20, chunk)
	tc.shell()

	// A faithful window-tracking client: unacknowledged in-flight input
	// never exceeds the 2MB window. If adjusts stop, this deadlocks
	// into read()'s 5s deadline instead of passing vacuously.
	readAdjust := func() int64 {
		p := tc.expect(msgWindowAdjust)
		r := &reader{b: p[1:]}
		r.u32() // recipient channel
		add := r.u32()
		if !r.ok() || add == 0 || add%chunk != 0 || add < adjustFloor {
			t.Fatalf("window adjust = %d (want ≥%d, a multiple of %d)", add, adjustFloor, chunk)
		}
		return int64(add)
	}
	remaining, adjusted := int64(1<<21), int64(0)
	payload := make([]byte, chunk)
	for sent := 0; sent < total; sent += chunk {
		if remaining < chunk {
			add := readAdjust()
			remaining += add
			adjusted += add
		}
		tc.sendData(payload)
		remaining -= chunk
	}
	// Trailing adjusts may still be in flight; drain what arrives
	// promptly. A sub-floor residue can legitimately stay owed
	// (takeOwed drains the whole counter, so a late collapsed take can
	// eat the tail that was building toward the next crossing) — it
	// carries toward the next crossing and the client always keeps
	// ≥1MB credit, which is exactly the no-stall contract the
	// faithful send loop above just proved by construction.
	tc.nc.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	for {
		p, err := tc.tr.readPacket()
		if err != nil {
			break
		}
		if len(p) > 0 && p[0] == msgWindowAdjust {
			r := &reader{b: p[1:]}
			r.u32() // recipient channel
			add := r.u32()
			if r.ok() && add > 0 {
				adjusted += int64(add)
			}
		}
	}
	if adjusted < total-adjustFloor {
		t.Fatalf("window adjusts returned %d bytes, want ≥%d (client never starves)", adjusted, total-adjustFloor)
	}
	if got := fed.Load(); got != total {
		t.Fatalf("handler consumed %d bytes, want %d", got, total)
	}
}

// A keystroke must reach the handler while the server's own output is
// wedged. The reader goroutine used to write each input packet's window
// adjust synchronously — with the kernel send buffer full of undrained
// frame data, that write stalled for up to writeTimeoutSec and fed the
// keystroke only after it (measured ~30s locally before the control
// pump). Input is the real-time path; output is the droppable one.
func TestKeystrokeFedWhileOutputCongested(t *testing.T) {
	fedAt := make(chan time.Time, 8)
	writing := make(chan struct{})
	srv := startServer(t, func(s *Session) {
		s.OnFeed(func(b []byte) { fedAt <- time.Now() })
		go func() {
			close(writing)
			s.Write(bytes.Repeat([]byte("x"), 32<<20)) // unread: wedges mid-write
		}()
		<-s.Done()
	})
	tc := dial(t, srv.Addr)
	tc.authNone()
	// A huge channel window, so the SSH flow-control window is never
	// the brake (a window-waiting writer holds no wmu): with a small
	// window the handler stalls in cond.Wait and the pre-fix reader
	// write below never contended — the test passed vacuously. Here
	// the kernel send buffer is the only brake, and the wedged frame
	// write holds wmu through its blocked conn.Write.
	tc.openSession(1<<29, 32768)
	tc.shell()

	<-writing
	time.Sleep(300 * time.Millisecond) // fill the kernel buffers, wedge the writer

	start := time.Now()
	tc.sendData([]byte("j"))
	select {
	case ts := <-fedAt:
		if d := ts.Sub(start); d > 2*time.Second {
			t.Fatalf("keystroke delivered after %v behind congested output", d)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("keystroke delivery stalled behind congested output")
	}

	// Reader-side replies must survive congestion too: an env request
	// mid-wedge queues its success on the control lane, and draining
	// the backlog must deliver it.
	w := &buf{}
	w.u8(msgChannelRequest)
	w.u32(0)
	w.cstr("env")
	w.boolean(true)
	w.cstr("PRIORITY")
	w.cstr("check")
	tc.send(w.b)
	for {
		p := tc.read()
		if len(p) == 0 {
			continue
		}
		switch p[0] {
		case msgChannelData, msgWindowAdjust, msgUserauthBanner, msgIgnore, msgDebug:
			continue // backlog drain
		case msgChannelSuccess:
			return // reply delivered through the congestion
		default:
			t.Fatalf("expected CHANNEL_SUCCESS after drain, got msg %d", p[0])
		}
	}
}

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
		t:         newTransport(c1),
		srv:       &Server{Log: log.New(io.Discard, "", 0)},
		ctlWake:   make(chan struct{}, 1),
		adjustSig: make(chan *channel, 1),
		dead:      make(chan struct{}),
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

func TestRekeyMidSession(t *testing.T) {
	srv := startServer(t, echoHandler)
	tc := dial(t, srv.Addr)
	tc.authNone()
	tc.openSession(1<<20, 32768)
	tc.ptyReq(80, 24)
	tc.shell()
	tc.readData() // greeting

	// Client-initiated rekey, then traffic must flow on the new keys.
	tc.exchange(buildKexinit(), false)
	tc.sendData([]byte("after-rekey"))
	if got := string(tc.readData()); got != "after-rekey" {
		t.Fatalf("echo after rekey = %q", got)
	}
}

func TestSessionCapRefusesExcess(t *testing.T) {
	// Queue disabled: the old pre-handshake refusal (with the queue on,
	// excess sessions wait in line instead — see admission_test.go).
	srv := startServer(t, echoHandler, func(s *Server) {
		s.MaxSessions = 1
		s.MaxQueue = -1
	})

	tc := dial(t, srv.Addr)
	tc.authNone()
	tc.openSession(1<<20, 32768)
	tc.shell() // holds the slot (admission is per-shell, not per-conn)

	nc2, err := net.Dial("tcp", srv.Addr)
	if err != nil {
		t.Fatal(err)
	}
	defer nc2.Close()
	nc2.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, 256)
	n, err := nc2.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("read refusal: %v", err)
	}
	got := string(buf[:n])
	// The disconnect packet may land in a separate segment: keep reading
	// until we have a newline plus the start of a binary packet.
	for {
		if i := strings.IndexByte(got, '\n'); i >= 0 && len(got) > i+6 {
			break
		}
		nc2.SetReadDeadline(time.Now().Add(5 * time.Second))
		n2, err2 := nc2.Read(buf[n:])
		got += string(buf[n : n+n2])
		n += n2
		if err2 != nil {
			break
		}
	}
	if !strings.Contains(got, serverVersion) {
		t.Fatalf("refusal missing version line: %q", got)
	}
	i := strings.IndexByte(got, '\n')
	if i < 0 || len(got) <= i+6 {
		t.Fatalf("expected a disconnect packet after the version line: %q", got)
	}
	pkt := []byte(got[i+1:])
	// binary packet: length(4) padlen(1) payload...
	if pkt[5] != msgDisconnect {
		t.Fatalf("expected msgDisconnect, got %d", pkt[5])
	}
	r := &reader{b: pkt[6:]}
	if code := r.u32(); code != discTooManyConns {
		t.Fatalf("disconnect reason = %d", code)
	}
}

func TestSessionRemoteAddr(t *testing.T) {
	addrs := make(chan string, 1)
	srv := startServer(t, func(s *Session) {
		addrs <- s.RemoteAddr()
		<-s.Done()
	})
	tc := dial(t, srv.Addr)
	tc.authNone()
	tc.openSession(1<<20, 32768)
	tc.shell()
	select {
	case a := <-addrs:
		host, _, err := net.SplitHostPort(a)
		if err != nil || host != "127.0.0.1" {
			t.Fatalf("RemoteAddr = %q, want the dialed loopback host", a)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("handler never ran")
	}
}

// A connection that completes the key exchange but never opens a
// session channel holds a goroutine and a transport while being
// invisible to the session cap (admission counts playing sessions
// only) — an idling fleet of them bypasses the cap entirely.
// PostAuthWait bounds the squat: the client sees a DISCONNECT and the
// server side is torn down.
func TestPostAuthNoChannelDeadline(t *testing.T) {
	srv := startServer(t, echoHandler, func(s *Server) {
		s.PostAuthWait = 250 * time.Millisecond
	})
	tc := dial(t, srv.Addr)
	tc.authNone() // KEX done, authenticated, no channel — now we squat
	start := time.Now()
	p := tc.expect(msgDisconnect)
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("idle drop took %v, PostAuthWait not honored", d)
	}
	r := &reader{b: p[1:]}
	if code := r.u32(); code != discByApplication {
		t.Fatalf("idle disconnect reason = %d, want %d (by application)", code, discByApplication)
	}
	// And the transport really ends, releasing the goroutine.
	tc.nc.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := tc.nc.Read(make([]byte, 1)); err == nil {
		t.Fatal("connection still open after the idle disconnect")
	}
}

// Failed userauth attempts are capped: auth here is a formality
// ("none" for everyone), so grinding failures is pure abuse and gets
// a DISCONNECT after maxAuthTries instead of an infinite reply loop.
func TestAuthRetryCapDisconnects(t *testing.T) {
	srv := startServer(t, echoHandler)
	tc := dial(t, srv.Addr)
	w := &buf{}
	w.u8(msgServiceRequest)
	w.cstr("ssh-userauth")
	tc.send(w.b)
	tc.expect(msgServiceAccept)

	authFail := func() {
		w := &buf{}
		w.u8(msgUserauthRequest)
		w.cstr("player")
		w.cstr("ssh-connection")
		w.cstr("password")
		w.boolean(false)
		w.cstr("x")
		tc.send(w.b)
	}
	for range maxAuthTries {
		authFail()
		tc.expect(msgUserauthFailure)
	}
	// One failure too many: the reply is a DISCONNECT, not another
	// failure.
	authFail()
	p := tc.expect(msgDisconnect)
	r := &reader{b: p[1:]}
	if code := r.u32(); code != discProtocolError {
		t.Fatalf("auth-cap disconnect reason = %d, want %d (protocol error)", code, discProtocolError)
	}
}

// End-to-end for the truncated-reply hold: a client that emits a DA2
// escape with no terminator must not wedge input — once the probe's
// wait window expires, probing is abandoned and later keystrokes
// reach the handler.
func TestTruncatedProbeRecoversE2E(t *testing.T) {
	fed := make(chan string, 8)
	gate := make(chan struct{})
	srv := startServer(t, func(s *Session) {
		<-gate // hold the feed back: the probe stays in buffering mode
		s.OnFeed(func(b []byte) { fed <- string(b) })
		<-s.Done()
	}, func(s *Server) { s.ProbeWait = 50 * time.Millisecond })
	tc := dial(t, srv.Addr)
	tc.authNone()
	tc.openSession(1<<20, 32768)
	tc.ptyReq(80, 24)
	tc.shell()
	// An escape that never terminates would historically keep the
	// probe buffering every later byte forever.
	tc.sendData([]byte("\x1b[>"))
	time.Sleep(150 * time.Millisecond) // past the probe's wait window
	close(gate)
	tc.sendData([]byte("AFTER"))

	gotAfter := ""
	deadline := time.After(5 * time.Second)
	for !strings.Contains(gotAfter, "AFTER") {
		select {
		case got := <-fed:
			gotAfter += got
		case <-deadline:
			t.Fatalf("input stayed swallowed after the probe gave up (got %q)", gotAfter)
		}
	}
	// Fully recovered: subsequent input passes through untouched.
	tc.sendData([]byte("NEXT"))
	select {
	case got := <-fed:
		if got != "NEXT" {
			t.Fatalf("post-recovery passthrough = %q, want NEXT", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("post-recovery input never reached the feed")
	}
}
