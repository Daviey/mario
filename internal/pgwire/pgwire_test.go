package pgwire

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"io"
	"math/big"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// --- fake backend message builders -------------------------------------

func frame(typ byte, body []byte) []byte {
	out := make([]byte, 5+len(body))
	out[0] = typ
	binary.BigEndian.PutUint32(out[1:], uint32(len(body)+4))
	copy(out[5:], body)
	return out
}

func authMsg(code int32, extra ...[]byte) []byte {
	body := make([]byte, 4)
	binary.BigEndian.PutUint32(body, uint32(code))
	for _, e := range extra {
		body = append(body, e...)
	}
	return frame('R', body)
}

func paramStatus(k, v string) []byte {
	return frame('S', []byte(k+"\x00"+v+"\x00"))
}

func readyQuery() []byte { return frame('Z', []byte{'I'}) }

func rowDesc(names ...string) []byte {
	body := make([]byte, 2)
	binary.BigEndian.PutUint16(body, uint16(len(names)))
	for _, n := range names {
		body = append(body, n...)
		body = append(body, 0) // name terminator
		// table oid, attnum, type oid, typlen, typmod, format code —
		// exactly rowDescFieldTail bytes, all zeros: the client keeps
		// only the names.
		body = append(body, make([]byte, rowDescFieldTail)...)
	}
	return frame('T', body)
}

// dataRow encodes one row; nil means NULL.
func dataRow(vals ...[]byte) []byte {
	body := make([]byte, 2)
	binary.BigEndian.PutUint16(body, uint16(len(vals)))
	for _, v := range vals {
		if v == nil {
			body = binary.BigEndian.AppendUint32(body, 0xFFFFFFFF)
			continue
		}
		body = binary.BigEndian.AppendUint32(body, uint32(len(v)))
		body = append(body, v...)
	}
	return frame('D', body)
}

func cmdTag(tag string) []byte { return frame('C', []byte(tag+"\x00")) }

func errResp(code, msg string) []byte {
	body := []byte("SERROR\x00")
	body = append(body, 'C')
	body = append(body, code...)
	body = append(body, 0)
	body = append(body, 'M')
	body = append(body, msg...)
	body = append(body, 0, 0)
	return frame('E', body)
}

// --- fake server ---------------------------------------------------------

// fakeServer is a loopback PostgreSQL backend speaking just enough of the
// v3 protocol for these tests. It never calls t.Fatal (it runs in
// goroutines); failures surface as ErrorResponse messages to the client.
type fakeServer struct {
	ln       net.Listener
	auth     string // "cleartext", "md5" or "scram"
	user     string
	password string
	reject   bool // reject any presented password
	tlsMode  string
	tlsConf  *tls.Config
	onQuery  func(sql string) [][]byte
	// wantApp, when set, is asserted against the startup message's
	// application_name inside the serve goroutine — a mismatch fails the
	// connection with an ErrorResponse, which keeps the assertion
	// race-detector-clean (no cross-goroutine state).
	wantApp string

	t         *testing.T
	startOnce sync.Once
}

func newFake(t *testing.T, auth string) *fakeServer {
	t.Helper()
	fs := &fakeServer{auth: auth, user: "mario", password: "secret", t: t}
	t.Cleanup(func() {
		if fs.ln != nil {
			fs.ln.Close()
		}
	})
	return fs
}

// start lazily opens the listener and the accept loop: tests configure
// fields (tlsMode, onQuery, ...) after newFake but before cfg(), and
// Dial/Accept does not give the race detector a happens-before edge for
// those writes — configuring must strictly precede serving.
func (fs *fakeServer) start() {
	fs.startOnce.Do(func() {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			fs.t.Fatalf("listen: %v", err)
		}
		fs.ln = ln
		go func() {
			for {
				conn, err := ln.Accept()
				if err != nil {
					return
				}
				go fs.serveConn(conn)
			}
		}()
	})
}

func (fs *fakeServer) addr() string {
	fs.start()
	return fs.ln.Addr().String()
}

func (fs *fakeServer) cfg() Config {
	fs.start()
	return Config{Addr: fs.addr(), User: fs.user, Password: fs.password, Database: "mario"}
}

func (fs *fakeServer) serveConn(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	br := bufio.NewReader(conn)

	if fs.tlsMode == "refuse" {
		var hdr [8]byte
		if _, err := io.ReadFull(br, hdr[:]); err != nil {
			return
		}
		_, _ = conn.Write([]byte{'N'})
		return
	}
	if fs.tlsMode == "accept" {
		var hdr [8]byte
		if _, err := io.ReadFull(br, hdr[:]); err != nil {
			return
		}
		if binary.BigEndian.Uint32(hdr[0:]) != 8 || binary.BigEndian.Uint32(hdr[4:]) != sslRequestCode {
			_, _ = conn.Write([]byte{'N'})
			return
		}
		if _, err := conn.Write([]byte{'S'}); err != nil {
			return
		}
		tconn := tls.Server(conn, fs.tlsConf)
		if err := tconn.Handshake(); err != nil {
			return
		}
		conn = tconn
		br = bufio.NewReader(conn)
	}

	user := fs.readStartup(conn, br)
	if user == "" {
		return
	}
	if !fs.authenticate(conn, br, user) {
		return
	}
	fs.writes(conn, authMsg(0),
		paramStatus("server_version", "15.4"),
		paramStatus("client_encoding", "UTF8"),
		frame('K', []byte{0, 0, 0, 1, 0, 0, 0, 2}),
		readyQuery())
	for {
		typ, body, err := readFrame(br)
		if err != nil {
			return
		}
		if typ == 'X' {
			return
		}
		if typ != 'Q' {
			fs.errf(conn, "08P01", fmt.Sprintf("unexpected message type %q", typ))
			return
		}
		resp := fs.onQuery(strings.TrimRight(string(body), "\x00"))
		if resp == nil {
			fs.errf(conn, "XX000", "no script for query")
			return
		}
		fs.writes(conn, resp...)
	}
}

// readStartup consumes the StartupMessage and returns the user name, or
// "" (conn closed) on protocol garbage.
func (fs *fakeServer) readStartup(conn net.Conn, br *bufio.Reader) string {
	var lenB [4]byte
	if _, err := io.ReadFull(br, lenB[:]); err != nil {
		return ""
	}
	n := int(binary.BigEndian.Uint32(lenB[:]))
	if n < 8 || n > 4096 {
		return ""
	}
	body := make([]byte, n-4)
	if _, err := io.ReadFull(br, body); err != nil {
		return ""
	}
	if binary.BigEndian.Uint32(body) != protoVersion {
		fs.errf(conn, "08P01", "unsupported protocol version")
		return ""
	}
	user, app := "", ""
	rest := body[4:]
	for len(rest) > 1 {
		i := bytes.IndexByte(rest, 0)
		if i < 0 {
			return ""
		}
		key := string(rest[:i])
		rest = rest[i+1:]
		i = bytes.IndexByte(rest, 0)
		if i < 0 {
			return ""
		}
		if key == "user" {
			user = string(rest[:i])
		}
		if key == "application_name" {
			app = string(rest[:i])
		}
		rest = rest[i+1:]
	}
	if fs.wantApp != "" && app != fs.wantApp {
		fs.errf(conn, "08P01", fmt.Sprintf("application_name %q, want %q", app, fs.wantApp))
		return ""
	}
	if user != fs.user {
		fs.errf(conn, "28000", fmt.Sprintf("unknown user %q", user))
		return ""
	}
	return user
}

// authenticate runs the server side of the auth exchange; true = ok.
func (fs *fakeServer) authenticate(conn net.Conn, br *bufio.Reader, user string) bool {
	switch fs.auth {
	case "cleartext":
		fs.writes(conn, authMsg(3))
		typ, body, err := readFrame(br)
		if err != nil || typ != 'p' {
			return false
		}
		if fs.reject || strings.TrimRight(string(body), "\x00") != fs.password {
			fs.errf(conn, "28P01", fmt.Sprintf("password authentication failed for user %q", user))
			return false
		}
	case "md5":
		salt := []byte{0xAB, 0xCD, 0xEF, 0x12}
		fs.writes(conn, authMsg(5, salt))
		typ, body, err := readFrame(br)
		if err != nil || typ != 'p' {
			return false
		}
		// Independent server-side math: "md5" + hex(md5(hex(md5(pass+user))+salt)).
		inner := md5.Sum([]byte(fs.password + user))
		mid := append([]byte(hex.EncodeToString(inner[:])), salt...)
		outer := md5.Sum(mid)
		want := "md5" + hex.EncodeToString(outer[:])
		if fs.reject || strings.TrimRight(string(body), "\x00") != want {
			fs.errf(conn, "28P01", "md5 password authentication failed")
			return false
		}
	case "scram":
		return fs.scramAuth(conn, br)
	case "sasl-no-scram":
		// Offers only mechanisms the client does not speak; the client
		// errors out without writing, so just wait for its close.
		fs.writes(conn, authMsg(10, []byte("SCRAM-SHA-512\x00\x00")))
		readFrame(br)
		return false
	case "sasl-continue-first":
		// SASLContinue with no preceding SASL (client sc is nil).
		fs.writes(conn, authMsg(11, []byte("r=x,s=pgw,i=4096")))
		readFrame(br)
		return false
	case "oversized":
		// Announces a message length past the 256 MiB sanity cap; the
		// client must reject the frame without trying to buffer it.
		var hdr [5]byte
		hdr[0] = 'S'
		binary.BigEndian.PutUint32(hdr[1:], 1<<30)
		_, _ = conn.Write(hdr[:])
		readFrame(br)
		return false
	default:
		fs.errf(conn, "XX000", "unknown auth mode "+fs.auth)
		return false
	}
	return true
}

// scramAuth implements the SCRAM-SHA-256 verifier server side: it checks
// the client proof against StoredKey-derived math and emits a valid v=
// signature, proving the whole loop beyond the RFC vector.
func (fs *fakeServer) scramAuth(conn net.Conn, br *bufio.Reader) bool {
	fail := func(msg string) bool {
		fs.errf(conn, "28P01", msg)
		return false
	}
	fs.writes(conn, authMsg(10, []byte("SCRAM-SHA-256\x00\x00")))
	typ, body, err := readFrame(br)
	if err != nil || typ != 'p' {
		return false
	}
	if string(body[:14]) != "SCRAM-SHA-256\x00" {
		return fail("bad mechanism")
	}
	firstLen := int(int32(binary.BigEndian.Uint32(body[14:])))
	if firstLen < 0 || 18+firstLen > len(body) {
		return fail("bad initial response length")
	}
	first := string(body[18 : 18+firstLen])
	bare := strings.TrimPrefix(first, "n,,")
	i := strings.Index(bare, ",r=")
	if i < 0 {
		return fail("bad client-first message")
	}
	cNonce := bare[i+3:]

	combined := cNonce + "SrvN0nceExtd"
	salt := []byte("pgwire-test-salt")
	iters := 4096
	serverFirst := fmt.Sprintf("r=%s,s=%s,i=%d", combined, base64.StdEncoding.EncodeToString(salt), iters)
	fs.writes(conn, authMsg(11, []byte(serverFirst)))

	typ, body, err = readFrame(br)
	if err != nil || typ != 'p' {
		return false
	}
	final := string(body)
	noProof := "c=biws,r=" + combined
	if !strings.HasPrefix(final, noProof+",p=") {
		return fail("bad client-final message")
	}
	proofB64 := strings.TrimPrefix(final, noProof+",p=")
	authStr := bare + "," + serverFirst + "," + noProof

	mac := func(key []byte, s string) []byte {
		h := hmac.New(sha256.New, key)
		h.Write([]byte(s))
		return h.Sum(nil)
	}
	salted := pbkdf2SHA256([]byte(fs.password), salt, iters, sha256.Size)
	clientKey := mac(salted, "Client Key")
	storedKey := sha256.Sum256(clientKey)
	clientSig := mac(storedKey[:], authStr)
	wantProof := make([]byte, len(clientKey))
	for j := range wantProof {
		wantProof[j] = clientKey[j] ^ clientSig[j]
	}
	gotProof, err := base64.StdEncoding.DecodeString(proofB64)
	if fs.reject || err != nil || !hmac.Equal(gotProof, wantProof) {
		return fail("scram proof mismatch")
	}
	v := base64.StdEncoding.EncodeToString(mac(mac(salted, "Server Key"), authStr))
	fs.writes(conn, authMsg(12, []byte("v="+v)))
	return true
}

func (fs *fakeServer) writes(conn net.Conn, msgs ...[]byte) {
	for _, m := range msgs {
		if _, err := conn.Write(m); err != nil {
			return
		}
	}
}

func (fs *fakeServer) errf(conn net.Conn, code, msg string) {
	fs.writes(conn, errResp(code, msg), readyQuery())
}

func readFrame(br *bufio.Reader) (byte, []byte, error) {
	var hdr [5]byte
	if _, err := io.ReadFull(br, hdr[:]); err != nil {
		return 0, nil, err
	}
	n := int(binary.BigEndian.Uint32(hdr[1:]))
	if n < 4 || n > 1<<28 {
		return 0, nil, fmt.Errorf("bad frame length %d", n)
	}
	body := make([]byte, n-4)
	if _, err := io.ReadFull(br, body); err != nil {
		return 0, nil, err
	}
	return hdr[0], body, nil
}

// --- 1. RFC 7677 section 3 test vector ----------------------------------

func TestScramVector(t *testing.T) {
	// Username "user", password "pencil", pinned client nonce.
	sc := newScramClient("user", "pencil", "rOprNGfwEbeRWgbNEkqO")
	if got, want := sc.clientFirst(), "n,,n=user,r=rOprNGfwEbeRWgbNEkqO"; got != want {
		t.Fatalf("client-first = %q, want %q", got, want)
	}
	serverFirst := "r=rOprNGfwEbeRWgbNEkqO%hvYDpWUa2RaTCAfuxFIlj)hNlF$k0,s=W22ZaJ0SNY7soEsUEjb6gQ==,i=4096"
	if err := sc.setServerFirst(serverFirst); err != nil {
		t.Fatalf("setServerFirst: %v", err)
	}
	wantNoProof := "c=biws,r=rOprNGfwEbeRWgbNEkqO%hvYDpWUa2RaTCAfuxFIlj)hNlF$k0"
	got := sc.clientFinal()
	if want := wantNoProof + ",p=dHzbZapWIk4jUhN+Ute9ytag9zjfMHgsqmmiz7AndVQ="; got != want {
		t.Fatalf("client-final:\n got  %q\n want %q", got, want)
	}
	if !sc.verifyServerFinal("v=6rriTRBi23WpRR/wtup+mMhUZUn/dB5nLTJRsjl95G4=") {
		t.Fatal("server signature verification failed")
	}
	if sc.verifyServerFinal("v=7rriTRBi23WpRR/wtup+mMhUZUn/dB5nLTJRsjl95G4=") {
		t.Fatal("tampered server signature accepted")
	}
}

// --- 2. cleartext auth + byte-exact row decoding -------------------------

func TestQueryValues(t *testing.T) {
	big := make([]byte, 300*1024)
	for i := range big {
		big[i] = byte(i * 7 % 251)
	}
	fs := newFake(t, "cleartext")
	fs.onQuery = func(sql string) [][]byte {
		return [][]byte{
			rowDesc("name", "num", "note", "blob"),
			dataRow([]byte("alice"), []byte("7"), []byte("hi"), nil),
			dataRow([]byte("it's 'quoted'"), []byte("ünïcode ☺"), []byte{}, big),
			cmdTag("SELECT 2"),
			readyQuery(),
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := Connect(ctx, fs.cfg())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()
	rows, cols, err := c.Query(ctx, "SELECT * FROM t")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	wantCols := []string{"name", "num", "note", "blob"}
	if len(cols) != len(wantCols) {
		t.Fatalf("got %d column names %v, want %d", len(cols), cols, len(wantCols))
	}
	for i, want := range wantCols {
		if cols[i] != want {
			t.Errorf("column %d name = %q, want %q", i, cols[i], want)
		}
	}
	if len(rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(rows))
	}
	check := func(v Value, want []byte) {
		t.Helper()
		if v.Null {
			t.Fatalf("value unexpectedly null (want %q)", want)
		}
		if !bytes.Equal(v.Data, want) {
			t.Fatalf("value = %q, want %q (len %d vs %d)", v.Data, want, len(v.Data), len(want))
		}
	}
	check(rows[0][0], []byte("alice"))
	if n, err := rows[0][1].Int(); err != nil || n != 7 {
		t.Fatalf("Int() = %d, %v; want 7, nil", n, err)
	}
	check(rows[0][2], []byte("hi"))
	if !rows[0][3].Null {
		t.Fatal("column 4 of row 1 must be NULL")
	}
	if got := rows[0][3].String(); got != "" {
		t.Fatalf("NULL String() = %q, want \"\"", got)
	}
	check(rows[1][0], []byte("it's 'quoted'"))
	check(rows[1][1], []byte("ünïcode ☺"))
	check(rows[1][2], []byte{})
	if rows[1][2].Null {
		t.Fatal("empty string must not decode as NULL")
	}
	check(rows[1][3], big)
	if _, err := rows[0][0].Int(); err == nil || !strings.Contains(err.Error(), "not an integer") {
		t.Fatalf("Int() on %q: got %v, want error mentioning \"not an integer\"", rows[0][0], err)
	}
}

// --- 3. MD5 auth variant ---------------------------------------------------

func TestMD5Auth(t *testing.T) {
	fs := newFake(t, "md5")
	fs.onQuery = func(sql string) [][]byte {
		return [][]byte{rowDesc("v"), dataRow([]byte("ok")), cmdTag("SELECT 1"), readyQuery()}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := Connect(ctx, fs.cfg())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()
	rows, _, err := c.Query(ctx, "SELECT 'ok' AS v")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(rows) != 1 || rows[0][0].String() != "ok" {
		t.Fatalf("rows = %v, want one row with \"ok\"", rows)
	}
}

// --- 4. SCRAM auth variant (full loop against a live verifier) ------------

func TestScramAuth(t *testing.T) {
	fs := newFake(t, "scram")
	fs.onQuery = func(sql string) [][]byte {
		return [][]byte{rowDesc("v"), dataRow([]byte("ok")), cmdTag("SELECT 1"), readyQuery()}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := Connect(ctx, fs.cfg())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()
	rows, _, err := c.Query(ctx, "SELECT 'ok' AS v")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(rows) != 1 || rows[0][0].String() != "ok" {
		t.Fatalf("rows = %v, want one row with \"ok\"", rows)
	}
}

// --- 5. ErrorResponse parsing ----------------------------------------------

func TestErrorResponse(t *testing.T) {
	fs := newFake(t, "cleartext")
	fs.onQuery = func(sql string) [][]byte {
		return [][]byte{errResp("42P01", `relation "scores" does not exist`), readyQuery()}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := Connect(ctx, fs.cfg())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()
	_, _, err = c.Query(ctx, "SELECT * FROM scores")
	if err == nil {
		t.Fatal("query must fail")
	}
	for _, sub := range []string{"42P01", `relation "scores" does not exist`} {
		if !strings.Contains(err.Error(), sub) {
			t.Fatalf("error %q missing %q", err, sub)
		}
	}
}

// --- 6. Exec tag parsing ----------------------------------------------------

func TestExecTags(t *testing.T) {
	fs := newFake(t, "cleartext")
	fs.onQuery = func(sql string) [][]byte {
		return [][]byte{cmdTag(sql), readyQuery()} // echo the SQL back as the tag
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := Connect(ctx, fs.cfg())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()
	cases := []struct {
		sql   string
		tag   string
		count int64
	}{
		{"UPDATE 3", "UPDATE", 3},
		{"DELETE 1", "DELETE", 1},
		{"SELECT", "SELECT", 0},
		{"CREATE TABLE", "CREATE TABLE", 0},
		{"INSERT 0 2", "INSERT 0", 2},
	}
	for _, tc := range cases {
		tag, count, err := c.Exec(ctx, tc.sql)
		if err != nil {
			t.Fatalf("Exec(%q): %v", tc.sql, err)
		}
		if tag != tc.tag || count != tc.count {
			t.Fatalf("Exec(%q) = (%q, %d); want (%q, %d)", tc.sql, tag, count, tc.tag, tc.count)
		}
	}
}

// --- 7. wrong password -------------------------------------------------------

func TestWrongPassword(t *testing.T) {
	fs := newFake(t, "cleartext")
	fs.reject = true
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := Connect(ctx, fs.cfg())
	if err == nil {
		t.Fatal("connect must fail on a rejected password")
	}
	if !strings.Contains(err.Error(), "28P01") {
		t.Fatalf("error = %v, want 28P01 auth failure", err)
	}
}

// --- 8. NULL column ------------------------------------------------------------

func TestNullColumn(t *testing.T) {
	fs := newFake(t, "cleartext")
	fs.onQuery = func(sql string) [][]byte {
		return [][]byte{rowDesc("a", "b"), dataRow(nil, []byte("x")), cmdTag("SELECT 1"), readyQuery()}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, err := Connect(ctx, fs.cfg())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()
	rows, _, err := c.Query(ctx, "SELECT NULL, 'x'")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if !rows[0][0].Null || rows[0][0].Data != nil {
		t.Fatalf("col 1 = %+v, want null", rows[0][0])
	}
	if rows[0][1].Null || string(rows[0][1].Data) != "x" {
		t.Fatalf("col 2 = %+v, want \"x\"", rows[0][1])
	}
}

// --- TLS transport -------------------------------------------------------------

func selfSignedCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "127.0.0.1"},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("cert: %v", err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func TestTLSRefused(t *testing.T) {
	fs := newFake(t, "cleartext")
	fs.tlsMode = "refuse"
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cfg := fs.cfg()
	cfg.TLS = &tls.Config{ServerName: "localhost"}
	_, err := Connect(ctx, cfg)
	if err == nil || !strings.Contains(err.Error(), "pgwire: server refused TLS") {
		t.Fatalf("err = %v, want \"pgwire: server refused TLS\"", err)
	}
}

func TestTLSConnect(t *testing.T) {
	fs := newFake(t, "cleartext")
	fs.tlsMode = "accept"
	fs.tlsConf = &tls.Config{Certificates: []tls.Certificate{selfSignedCert(t)}}
	fs.onQuery = func(sql string) [][]byte {
		return [][]byte{rowDesc("v"), dataRow([]byte("tls ok")), cmdTag("SELECT 1"), readyQuery()}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cfg := fs.cfg()
	cfg.TLS = &tls.Config{InsecureSkipVerify: true} // self-signed test cert
	c, err := Connect(ctx, cfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()
	rows, _, err := c.Query(ctx, "SELECT 1")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(rows) != 1 || rows[0][0].String() != "tls ok" {
		t.Fatalf("rows = %v", rows)
	}
}

// TestTLSScramProductionPath is the one test that stacks every layer the
// board.DBFromEnv production path uses — SSLRequest upgrade + SCRAM-SHA-256
// exchange + full Connect→Query with column names — instead of exercising
// them in isolation (TestTLSConnect and TestScramAuth). It also pins the
// application_name default and its -dump-replays override, so a regression
// in any single layer shows up on the exact shape production runs.
func TestTLSScramProductionPath(t *testing.T) {
	fs := newFake(t, "scram")
	fs.tlsMode = "accept"
	fs.tlsConf = &tls.Config{Certificates: []tls.Certificate{selfSignedCert(t)}}
	fs.wantApp = defaultAppName
	fs.onQuery = func(sql string) [][]byte {
		return [][]byte{rowDesc("v"), dataRow([]byte("prod ok")), cmdTag("SELECT 1"), readyQuery()}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cfg := fs.cfg()
	cfg.TLS = &tls.Config{InsecureSkipVerify: true} // require-mode, like DBFromEnv
	c, err := Connect(ctx, cfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	rows, cols, err := c.Query(ctx, "SELECT 'prod ok' AS v")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if len(rows) != 1 || rows[0][0].String() != "prod ok" {
		t.Fatalf("rows = %v, want one row with \"prod ok\"", rows)
	}
	if len(cols) != 1 || cols[0] != "v" {
		t.Fatalf("cols = %v, want [v]", cols)
	}
	// The override half: a fresh server asserting the custom name against
	// its startup message. A new server (not a field flip on fs) because
	// the fake's contract is configure-before-start — see start().
	fs2 := newFake(t, "scram")
	fs2.tlsMode = "accept"
	fs2.tlsConf = &tls.Config{Certificates: []tls.Certificate{selfSignedCert(t)}}
	fs2.wantApp = "mario-dump-replays"
	fs2.onQuery = fs.onQuery
	cfg2 := fs2.cfg()
	cfg2.TLS = &tls.Config{InsecureSkipVerify: true} // require-mode, like DBFromEnv
	cfg2.AppName = fs2.wantApp
	c2, err := Connect(ctx, cfg2)
	if err != nil {
		t.Fatalf("connect with AppName override: %v", err)
	}
	defer c2.Close()
	if _, _, err := c2.Query(ctx, "SELECT 1"); err != nil {
		t.Fatalf("query after override: %v", err)
	}
}

// --- context cancellation mid-query --------------------------------------------

func TestQueryContextCancel(t *testing.T) {
	fs := newFake(t, "cleartext")
	stall := make(chan struct{})
	fs.onQuery = func(sql string) [][]byte {
		<-stall // never answers
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	defer close(stall)
	start := time.Now()
	c, err := Connect(ctx, fs.cfg())
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()
	if _, _, err := c.Query(ctx, "SELECT 1"); err == nil {
		t.Fatal("query must fail when the context deadline passes")
	} else if !strings.Contains(err.Error(), context.DeadlineExceeded.Error()) {
		t.Fatalf("err = %v, want wrapped context deadline error", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("query took %v; ctx deadline not honored", elapsed)
	}
	if _, _, err := c.Query(context.Background(), "SELECT 1"); err == nil {
		t.Fatal("conn must be dead after ctx cancellation")
	}
}

// --- 9. negative auth paths -----------------------------------------------------

// TestAuthNegativePaths: hostile or broken server auth behavior must
// fail closed — an unusable SASL mechanism list, SASLContinue before
// SASL, and an oversized message length all error instead of hanging or
// allocating.
func TestAuthNegativePaths(t *testing.T) {
	for _, tc := range []struct{ auth, want string }{
		{"sasl-no-scram", "does not support SCRAM-SHA-256"},
		{"sasl-continue-first", "unexpected sasl continue"},
		{"oversized", "bad message length"},
	} {
		fs := newFake(t, tc.auth)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := Connect(ctx, fs.cfg())
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", tc.auth, err, tc.want)
		}
	}
}
