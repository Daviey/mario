// Package pgwire is a minimal PostgreSQL v3 wire-protocol client that
// speaks the simple query protocol only (no extended protocol, no
// prepared statements, no COPY, no pipelining). It supports plaintext
// and TLS (SSLRequest upgrade) transports, cleartext, MD5 and
// SCRAM-SHA-256 authentication with server-signature verification.
// Stdlib only, by repo policy.
package pgwire

import (
	"bufio"
	"bytes"
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

const (
	protoVersion   = 196608 // 3.0
	sslRequestCode = 80877103
	defaultTimeout = 10 * time.Second
	appName        = "mario-verify"
	maxMessageLen  = 1 << 28 // 256 MiB sanity cap against corrupt streams
)

// Config for one connection.
type Config struct {
	Addr           string // "host:port"
	User           string
	Password       string
	Database       string
	TLS            *tls.Config   // nil = plaintext; non-nil = SSLRequest upgrade first
	ConnectTimeout time.Duration // 0 = 10s default
}

// Value is one column of one row. Null is distinct from empty string.
type Value struct {
	Null bool
	Data []byte
}

// String returns the raw bytes as a string; "" when Null.
func (v Value) String() string {
	if v.Null {
		return ""
	}
	return string(v.Data)
}

// Int parses the value as a decimal integer.
func (v Value) Int() (int, error) {
	s := v.String()
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("pgwire: %q is not an integer", s)
	}
	return n, nil
}

// Conn is a single threaded connection (no concurrent use).
type Conn struct {
	conn   net.Conn
	br     *bufio.Reader
	closed bool
}

// Connect dials cfg.Addr, optionally upgrades to TLS, performs the v3
// startup/authentication exchange and returns a ready connection.
func Connect(ctx context.Context, cfg Config) (*Conn, error) {
	timeout := cfg.ConnectTimeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	d := &net.Dialer{Timeout: timeout}
	raw, err := d.DialContext(ctx, "tcp", cfg.Addr)
	if err != nil {
		return nil, fmt.Errorf("pgwire: dial %s: %w", cfg.Addr, err)
	}
	conn := raw
	if cfg.TLS != nil {
		conn, err = upgradeTLS(raw, cfg)
		if err != nil {
			raw.Close()
			return nil, err
		}
	}
	c := &Conn{conn: conn, br: bufio.NewReader(conn)}
	if err := c.startup(ctx, cfg); err != nil {
		c.kill()
		return nil, err
	}
	return c, nil
}

// upgradeTLS performs the SSLRequest handshake and wraps raw in a TLS
// client connection.
func upgradeTLS(raw net.Conn, cfg Config) (net.Conn, error) {
	req := make([]byte, 8)
	binary.BigEndian.PutUint32(req[0:], 8)
	binary.BigEndian.PutUint32(req[4:], sslRequestCode)
	if _, err := raw.Write(req); err != nil {
		return nil, fmt.Errorf("pgwire: sslrequest: %w", err)
	}
	var reply [1]byte
	if _, err := io.ReadFull(raw, reply[:]); err != nil {
		return nil, fmt.Errorf("pgwire: sslrequest reply: %w", err)
	}
	switch reply[0] {
	case 'S':
		tlsCfg := cfg.TLS.Clone()
		if tlsCfg.ServerName == "" {
			if host, _, err := net.SplitHostPort(cfg.Addr); err == nil && host != "" {
				tlsCfg.ServerName = host
			}
		}
		return tls.Client(raw, tlsCfg), nil
	case 'N':
		return nil, errors.New("pgwire: server refused TLS")
	default:
		return nil, fmt.Errorf("pgwire: unexpected sslrequest reply %q", reply[0])
	}
}

// startup sends the StartupMessage and runs the authentication exchange,
// then consumes post-auth messages until ReadyForQuery.
func (c *Conn) startup(ctx context.Context, cfg Config) error {
	release := c.guard(ctx)
	defer release()

	body := make([]byte, 0, 64)
	body = binary.BigEndian.AppendUint32(body, protoVersion)
	for _, kv := range [][2]string{
		{"user", cfg.User},
		{"database", cfg.Database},
		{"application_name", appName},
	} {
		body = append(body, kv[0]...)
		body = append(body, 0)
		body = append(body, kv[1]...)
		body = append(body, 0)
	}
	body = append(body, 0)
	msg := binary.BigEndian.AppendUint32(make([]byte, 0, len(body)+4), uint32(len(body)+4))
	msg = append(msg, body...)
	if _, err := c.conn.Write(msg); err != nil {
		return c.fail(ctx, "startup", err)
	}
	if err := c.authenticate(ctx, cfg); err != nil {
		return err
	}
	return c.skipToReady(ctx)
}

// authenticate runs the AuthenticationSASL/cleartext/MD5 loop until the
// server reports AuthenticationOk.
func (c *Conn) authenticate(ctx context.Context, cfg Config) error {
	var sc *scramClient
	for {
		typ, payload, err := c.readMessage()
		if err != nil {
			return c.fail(ctx, "auth", err)
		}
		switch typ {
		case 'N': // NoticeResponse — ignored
		case 'E':
			return pgError(payload)
		case 'R':
			if len(payload) < 4 {
				return errors.New("pgwire: short authentication message")
			}
			code := binary.BigEndian.Uint32(payload)
			switch code {
			case 0: // AuthenticationOk
				return nil
			case 3: // cleartext
				if err := c.writeMsg(ctx, 'p', append([]byte(cfg.Password), 0)); err != nil {
					return err
				}
			case 5: // MD5
				if len(payload) < 8 {
					return errors.New("pgwire: short md5 authentication message")
				}
				if err := c.writeMsg(ctx, 'p', append(md5Response(cfg.User, cfg.Password, payload[4:8]), 0)); err != nil {
					return err
				}
			case 10: // SASL — SCRAM-SHA-256
				var err error
				sc, err = c.scramStart(ctx, cfg, payload)
				if err != nil {
					return err
				}
			case 11: // SASLContinue — server-first message
				if sc == nil {
					return errors.New("pgwire: unexpected sasl continue")
				}
				if err := sc.setServerFirst(string(payload[4:])); err != nil {
					return err
				}
				if err := c.writeMsg(ctx, 'p', []byte(sc.clientFinal())); err != nil {
					return err
				}
			case 12: // SASLFinal — server signature
				if sc == nil {
					return errors.New("pgwire: unexpected sasl final")
				}
				if !sc.verifyServerFinal(string(payload[4:])) {
					return errors.New("pgwire: scram server signature mismatch")
				}
			default:
				return fmt.Errorf("pgwire: unsupported authentication type %d", code)
			}
		default:
			return fmt.Errorf("pgwire: unexpected message %q during authentication", typ)
		}
	}
}

// scramStart picks SCRAM-SHA-256 from the offered mechanisms and sends
// the SASLInitialResponse carrying the client-first message.
func (c *Conn) scramStart(ctx context.Context, cfg Config, payload []byte) (*scramClient, error) {
	if len(payload) < 4 {
		return nil, errors.New("pgwire: short sasl mechanism list")
	}
	supported := false
	for _, m := range strings.Split(string(payload[4:]), "\x00") {
		if m == scramMechanism {
			supported = true
		}
	}
	if !supported {
		return nil, fmt.Errorf("pgwire: server does not support %s", scramMechanism)
	}
	nonce := make([]byte, 18)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("pgwire: scram nonce: %w", err)
	}
	sc := newScramClient("", cfg.Password, base64.StdEncoding.EncodeToString(nonce))
	first := sc.clientFirst()
	body := append([]byte(scramMechanism), 0)
	body = binary.BigEndian.AppendUint32(body, uint32(len(first)))
	body = append(body, first...)
	if err := c.writeMsg(ctx, 'p', body); err != nil {
		return nil, err
	}
	return sc, nil
}

// md5Response computes the PostgreSQL MD5 password verifier:
// "md5" + hex(md5(hex(md5(password+user)) + salt)).
func md5Response(user, password string, salt []byte) []byte {
	inner := md5.Sum([]byte(password + user))
	mid := append([]byte(hex.EncodeToString(inner[:])), salt...)
	outer := md5.Sum(mid)
	return []byte("md5" + hex.EncodeToString(outer[:]))
}

// skipToReady consumes ParameterStatus, BackendKeyData and NoticeResponse
// messages until ReadyForQuery arrives.
func (c *Conn) skipToReady(ctx context.Context) error {
	for {
		typ, payload, err := c.readMessage()
		if err != nil {
			return c.fail(ctx, "auth", err)
		}
		switch typ {
		case 'Z':
			return nil
		case 'E':
			return pgError(payload)
		case 'S', 'K', 'N': // parameter status, backend key data, notice
		default:
			return fmt.Errorf("pgwire: unexpected message %q after authentication", typ)
		}
	}
}

// Query runs one simple-query SQL statement, returning all rows.
func (c *Conn) Query(ctx context.Context, sql string) ([][]Value, error) {
	rows, _, err := c.simpleQuery(ctx, sql)
	return rows, err
}

// Exec runs one statement, returning the CommandComplete tag and its
// affected-rows count ("UPDATE 3" -> "UPDATE", 3; "SELECT 12" ->
// "SELECT", 12; tags without a count -> count 0).
func (c *Conn) Exec(ctx context.Context, sql string) (tag string, count int64, err error) {
	_, tag, err = c.simpleQuery(ctx, sql)
	if err != nil {
		return "", 0, err
	}
	name, n := parseTag(tag)
	return name, n, nil
}

// Close sends Terminate and closes the connection. Safe to call twice.
func (c *Conn) Close() error {
	if c.closed {
		return nil
	}
	c.closed = true
	_, _ = c.conn.Write([]byte{'X', 0, 0, 0, 4}) // best-effort Terminate
	return c.conn.Close()
}

// simpleQuery sends one Query message and collects DataRows until
// ReadyForQuery.
func (c *Conn) simpleQuery(ctx context.Context, sql string) (rows [][]Value, tag string, err error) {
	release := c.guard(ctx)
	defer release()

	if err := c.writeMsg(ctx, 'Q', append([]byte(sql), 0)); err != nil {
		return nil, "", err
	}
	for {
		typ, payload, err := c.readMessage()
		if err != nil {
			return nil, "", c.fail(ctx, "query", err)
		}
		switch typ {
		case 'T': // RowDescription — payload fully read, fields ignored
		case 'D':
			row, err := parseDataRow(payload)
			if err != nil {
				return nil, "", err
			}
			rows = append(rows, row)
		case 'C': // CommandComplete
			tag = strings.TrimRight(string(payload), "\x00")
		case 'I': // EmptyQueryResponse
			tag = ""
		case 'N': // NoticeResponse — ignored
		case 'Z': // ReadyForQuery (1-byte tx status)
			return rows, tag, nil
		case 'E':
			return nil, "", pgError(payload)
		default: // async/unknown messages — ignored
		}
	}
}

// parseDataRow decodes one DataRow payload. Values reference the message
// buffer; there are no fixed-size reads, so ~256KB values are fine.
func parseDataRow(p []byte) ([]Value, error) {
	if len(p) < 2 {
		return nil, errors.New("pgwire: short data row")
	}
	n := int(binary.BigEndian.Uint16(p))
	row := make([]Value, n)
	off := 2
	for i := range n {
		if off+4 > len(p) {
			return nil, errors.New("pgwire: truncated data row")
		}
		l := int(int32(binary.BigEndian.Uint32(p[off:])))
		off += 4
		if l < 0 { // -1 = NULL
			row[i] = Value{Null: true}
			continue
		}
		if off+l > len(p) {
			return nil, errors.New("pgwire: truncated data row")
		}
		row[i] = Value{Data: p[off : off+l]}
		off += l
	}
	return row, nil
}

// parseTag splits a CommandComplete tag into its command name and
// affected-rows count. The count is the trailing token when numeric
// ("UPDATE 3" -> "UPDATE", 3; "INSERT 0 2" -> "INSERT 0", 2); tags
// without a numeric tail report 0 ("CREATE TABLE" -> "CREATE TABLE", 0).
func parseTag(tag string) (string, int64) {
	i := strings.LastIndexByte(tag, ' ')
	if i < 0 {
		return tag, 0
	}
	n, err := strconv.ParseInt(tag[i+1:], 10, 64)
	if err != nil {
		return tag, 0
	}
	return tag[:i], n
}

// pgError decodes an ErrorResponse payload into an error, formatted like
// pgwire: ERROR 42P01: relation "x" does not exist.
func pgError(p []byte) error {
	sev, code, msg := "", "", ""
	for len(p) > 0 {
		ft := p[0]
		p = p[1:]
		if ft == 0 { // terminator
			break
		}
		end := bytes.IndexByte(p, 0)
		if end < 0 {
			break
		}
		val := string(p[:end])
		p = p[end+1:]
		switch ft {
		case 'S':
			sev = val
		case 'C':
			code = val
		case 'M':
			msg = val
		}
	}
	if sev == "" {
		sev = "ERROR"
	}
	switch {
	case code != "" && msg != "":
		return fmt.Errorf("pgwire: %s %s: %s", sev, code, msg)
	case msg != "":
		return fmt.Errorf("pgwire: %s: %s", sev, msg)
	case code != "":
		return fmt.Errorf("pgwire: %s %s", sev, code)
	}
	return errors.New("pgwire: error response")
}

// readMessage reads one backend message: type byte, int32 length
// (including itself, excluding the type byte), then exactly length-4
// payload bytes.
func (c *Conn) readMessage() (byte, []byte, error) {
	var hdr [5]byte
	if _, err := io.ReadFull(c.br, hdr[:]); err != nil {
		return 0, nil, err
	}
	n := int(binary.BigEndian.Uint32(hdr[1:]))
	if n < 4 || n > maxMessageLen {
		return 0, nil, fmt.Errorf("pgwire: bad message length %d", n)
	}
	payload := make([]byte, n-4)
	if _, err := io.ReadFull(c.br, payload); err != nil {
		return 0, nil, err
	}
	return hdr[0], payload, nil
}

// writeMsg sends one frontend message: type byte + int32 length +
// payload.
func (c *Conn) writeMsg(ctx context.Context, typ byte, body []byte) error {
	buf := make([]byte, 5+len(body))
	buf[0] = typ
	binary.BigEndian.PutUint32(buf[1:], uint32(len(body)+4))
	copy(buf[5:], body)
	if _, err := c.conn.Write(buf); err != nil {
		return c.fail(ctx, "write", err)
	}
	return nil
}

// guard derives connection deadlines from ctx: an explicit ctx deadline
// becomes the conn deadline, and ctx cancellation interrupts in-flight
// reads. The returned release func must be called when the operation
// ends.
func (c *Conn) guard(ctx context.Context) (release func()) {
	if dl, ok := ctx.Deadline(); ok {
		_ = c.conn.SetDeadline(dl)
	} else {
		_ = c.conn.SetDeadline(time.Time{})
	}
	done := ctx.Done()
	if done == nil {
		return func() { _ = c.conn.SetDeadline(time.Time{}) }
	}
	stop := make(chan struct{})
	go func() {
		select {
		case <-done:
			// Interrupt any in-flight read/write immediately.
			_ = c.conn.SetDeadline(time.Now().Add(-time.Second))
		case <-stop:
		}
	}()
	return func() {
		close(stop)
		_ = c.conn.SetDeadline(time.Time{})
	}
}

// fail converts an I/O error into a package error. If ctx is done (or
// its deadline clearly caused an i/o timeout, which can surface a tick
// before ctx.Err() is set) the connection is killed and the ctx error is
// wrapped instead.
func (c *Conn) fail(ctx context.Context, op string, err error) error {
	ctxErr := ctx.Err()
	if ctxErr == nil {
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			if dl, ok := ctx.Deadline(); ok && !time.Now().Before(dl) {
				ctxErr = context.DeadlineExceeded
			}
		}
	}
	if ctxErr != nil {
		c.kill()
		return fmt.Errorf("pgwire: %s: %w", op, ctxErr)
	}
	return fmt.Errorf("pgwire: %s: %w", op, err)
}

// kill closes the underlying connection once.
func (c *Conn) kill() {
	if !c.closed {
		c.closed = true
		_ = c.conn.Close()
	}
}
