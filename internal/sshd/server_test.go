package sshd

// Full-protocol tests against a real listener, driven by an in-package
// client that mirrors the handshake from the other side (its own curve25519
// key, its own exchange-hash computation, independent host-key signature
// verification). This cross-checks the server's framing, KDF and sequence
// handling against a second implementation of the same RFCs.

import (
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
	"testing"
	"time"
)

const clientVersion = "SSH-2.0-GoMarioTest_1.0"

type testClient struct {
	t         *testing.T
	nc        net.Conn
	tr        *transport
	sessionID []byte
}

// startServer boots a Server on its own listener; srv.Addr becomes the
// bound address.
func startServer(t *testing.T, handler func(*Session)) *Server {
	t.Helper()
	srv := &Server{
		Handler: handler,
		Log:     log.New(io.Discard, "", 0),
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
	srv := startServer(t, echoHandler)
	srv.MaxSessions = 1
	srv.sem = make(chan struct{}, 1)

	tc := dial(t, srv.Addr)
	tc.authNone() // occupies the single slot

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
