package board

import (
	"bufio"
	"context"
	"encoding/binary"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Daviey/mario/internal/pgwire"
)

func TestSQLString(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", "plain"},
		{"it's", "it''s"},
		{"a''b", "a''''b"},
		{"", ""},
	}
	for _, c := range cases {
		if got := sqlString(c.in); got != c.want {
			t.Errorf("sqlString(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDBConfigFromEnv(t *testing.T) {
	t.Run("password required", func(t *testing.T) {
		t.Setenv("SUPABASE_DB_PASSWORD", "")
		if _, err := DBConfigFromEnv(); err == nil || !strings.Contains(err.Error(), "SUPABASE_DB_PASSWORD") {
			t.Fatalf("want missing-password error, got %v", err)
		}
	})
	t.Run("host derived from project url", func(t *testing.T) {
		t.Setenv("SUPABASE_DB_PASSWORD", "pw")
		t.Setenv("SUPABASE_URL", "https://zqxwkabc.supabase.co")
		t.Setenv("SUPABASE_DB_HOST", "")
		t.Setenv("SUPABASE_DB_PORT", "")
		cfg, err := DBConfigFromEnv()
		if err != nil {
			t.Fatal(err)
		}
		want := "db.zqxwkabc.supabase.co:5432"
		if cfg.Addr != want {
			t.Errorf("addr = %q, want %q", cfg.Addr, want)
		}
		if cfg.User != "postgres" || cfg.Database != "postgres" || cfg.Password != "pw" {
			t.Errorf("cfg = %+v", cfg)
		}
	})
	t.Run("explicit host and port win", func(t *testing.T) {
		t.Setenv("SUPABASE_DB_PASSWORD", "pw")
		t.Setenv("SUPABASE_DB_HOST", "db.lan")
		t.Setenv("SUPABASE_DB_PORT", "6543")
		cfg, err := DBConfigFromEnv()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.Addr != "db.lan:6543" {
			t.Errorf("addr = %q, want db.lan:6543", cfg.Addr)
		}
	})
}

func TestPendingRowFromValues(t *testing.T) {
	val := func(s string) pgwire.Value { return pgwire.Value{Data: []byte(s)} }
	nul := pgwire.Value{Null: true}
	cols := []pgwire.Value{
		val("id-1"), val("DAVE"), val("500"), val("2"),
		val("classic"), nul, val("2026.08.29a"), val(`{"v":1}`),
		nul, nul, val("xterm-256color"), nul, nul, val("80x24"),
	}
	p, err := pendingRowFromValues(cols, "pending")
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != "id-1" || p.Name != "DAVE" || p.Score != 500 || p.Level != 2 || p.Mode != "classic" {
		t.Errorf("row = %+v", p)
	}
	// Column-count guarding lives in Pending (it sees the query result);
	// here only value conversion is under test.
	bad := append([]pgwire.Value{}, cols...)
	bad[2] = val("not-a-number")
	if _, err := pendingRowFromValues(bad, "pending"); err == nil || !strings.Contains(err.Error(), "score") {
		t.Errorf("want score parse error, got %v", err)
	}
	bad = append([]pgwire.Value{}, cols...)
	bad[0] = nul
	if _, err := pendingRowFromValues(bad, "pending"); err == nil || !strings.Contains(err.Error(), "NULL id") {
		t.Errorf("want NULL id error, got %v", err)
	}
}

// TestLiveDBPending exercises the direct-Postgres backend against the real
// database (read-only). LIVE=1 go test -run TestLiveDBPending -v ./board
func TestLiveDBPending(t *testing.T) {
	if os.Getenv("LIVE") == "" {
		t.Skip("set LIVE=1 for live database tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, err := DBFromEnv(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	rows, err := c.Pending(ctx, 2)
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	for _, r := range rows {
		t.Logf("pending row %s %s %d", r.ID, r.Name, r.Score)
	}
}

// TestLiveDBLatest exercises the dump path behind `mario -dump-replays`
// against the real database (read-only). LIVE=1 go test -run TestLiveDBLatest -v ./board
func TestLiveDBLatest(t *testing.T) {
	if os.Getenv("LIVE") == "" {
		t.Skip("set LIVE=1 for live database tests")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	c, err := DBFromEnv(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	rows, err := c.Latest(ctx, 3)
	if err != nil {
		t.Fatalf("latest: %v", err)
	}
	for _, r := range rows {
		if r.Replay == "" {
			t.Errorf("row %s lacks replay data", r.ID)
		}
		t.Logf("latest row %s %s %d eng=%s surface=%s", r.ID, r.Name, r.Score, r.EngineVersion, r.Surface)
	}
}

// --- fake PostgreSQL backend for DBClient SQL assertions -----------------
//
// Unlike the LIVE tests above, these drive a loopback backend, so the
// exact SQL the direct-Postgres fallback sends is asserted on every run.

// fakeDB speaks just enough v3 wire for the DBClient tests: it accepts
// the startup + cleartext exchange, records every Query's SQL text and
// answers from a scripted responder.
type fakeDB struct {
	ln   net.Listener
	mu   sync.Mutex // sqls: server goroutine records, test goroutine reads
	sqls []string
	resp func(sql string) [][]byte
}

func newFakeDB(t *testing.T, resp func(sql string) [][]byte) *fakeDB {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	f := &fakeDB{ln: ln, resp: resp}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(conn)
		}
	}()
	return f
}

func (f *fakeDB) queries() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.sqls...)
}

func (f *fakeDB) serve(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	br := bufio.NewReader(conn)
	readFrame := func() ([]byte, error) {
		var hdr [5]byte
		if _, err := io.ReadFull(br, hdr[:]); err != nil {
			return nil, err
		}
		body := make([]byte, int(binary.BigEndian.Uint32(hdr[1:]))-4)
		if _, err := io.ReadFull(br, body); err != nil {
			return nil, err
		}
		return body, nil
	}
	// StartupMessage: int32 length (including itself), then the body.
	var lenB [4]byte
	if _, err := io.ReadFull(br, lenB[:]); err != nil {
		return
	}
	start := make([]byte, binary.BigEndian.Uint32(lenB[:])-4)
	if _, err := io.ReadFull(br, start); err != nil {
		return
	}
	// Cleartext exchange: request the password, accept any.
	if _, err := conn.Write(dbAuthFrame(3)); err != nil {
		return
	}
	if _, err := readFrame(); err != nil {
		return
	}
	if _, err := conn.Write(dbAuthFrame(0)); err != nil {
		return
	}
	if _, err := conn.Write(dbFrame('Z', []byte{'I'})); err != nil {
		return
	}
	for {
		body, err := readFrame()
		if err != nil {
			return
		}
		sql := strings.TrimRight(string(body), "\x00")
		f.mu.Lock()
		f.sqls = append(f.sqls, sql)
		f.mu.Unlock()
		for _, msg := range f.resp(sql) {
			if _, err := conn.Write(msg); err != nil {
				return
			}
		}
	}
}

// dbFrame wraps a payload in one backend message (type, length, body).
func dbFrame(typ byte, body []byte) []byte {
	out := make([]byte, 5+len(body))
	out[0] = typ
	binary.BigEndian.PutUint32(out[1:], uint32(len(body)+4))
	copy(out[5:], body)
	return out
}

// dbAuthFrame builds an Authentication message (code only, no extra).
func dbAuthFrame(code uint32) []byte {
	return dbFrame('R', binary.BigEndian.AppendUint32(nil, code))
}

// dbRowDesc announces n columns; the client ignores the field details.
func dbRowDesc(n int) []byte {
	return dbFrame('T', binary.BigEndian.AppendUint16(nil, uint16(n)))
}

// dbDataRow encodes one row; a nil value is NULL.
func dbDataRow(vals ...[]byte) []byte {
	body := binary.BigEndian.AppendUint16(nil, uint16(len(vals)))
	for _, v := range vals {
		if v == nil {
			body = binary.BigEndian.AppendUint32(body, 0xFFFFFFFF)
			continue
		}
		body = binary.BigEndian.AppendUint32(body, uint32(len(v)))
		body = append(body, v...)
	}
	return dbFrame('D', body)
}

func dbCmdTag(tag string) []byte { return dbFrame('C', append([]byte(tag), 0)) }

func dbReady() []byte { return dbFrame('Z', []byte{'I'}) }

// TestDBClientSQLOperations pins the SQL the direct-Postgres fallback
// sends: it must mirror the PostgREST semantics of Client.Pending /
// SetVerified / DeleteRow (same filters, same ordering, the same 14
// columns) or the operator fallback quietly verifies a different queue.
func TestDBClientSQLOperations(t *testing.T) {
	f := newFakeDB(t, func(sql string) [][]byte {
		if strings.HasPrefix(sql, "SELECT") {
			return [][]byte{
				dbRowDesc(14),
				dbDataRow([]byte("id-1"), []byte("DAVE"), []byte("500"), []byte("2"),
					[]byte("classic"), nil, []byte(EngineVersion), []byte(`{"v":1,"ticks":1,"runs":[[0,1]]}`),
					[]byte("ssh"), nil, []byte("xterm"), nil, []byte("legacy"), []byte("80x24")),
				dbCmdTag("SELECT 1"), dbReady(),
			}
		}
		return [][]byte{dbCmdTag("UPDATE 1"), dbReady()}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := pgwire.Connect(ctx, pgwire.Config{
		Addr: f.ln.Addr().String(), User: "mario", Password: "secret", Database: "mario",
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close()
	c := &DBClient{conn: conn}

	pend, err := c.Pending(ctx, 5)
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(pend) != 1 {
		t.Fatalf("pending = %+v", pend)
	}
	p := pend[0]
	if p.ID != "id-1" || p.Name != "DAVE" || p.Score != 500 || p.Level != 2 ||
		p.Mode != "classic" || p.EngineVersion != EngineVersion || p.Surface != "ssh" ||
		p.Term != "xterm" || p.Day != "" || p.Viewport != "80x24" {
		t.Fatalf("pending row = %+v", p)
	}

	if _, err := c.Latest(ctx, 3); err != nil {
		t.Fatalf("latest: %v", err)
	}
	// The quote in the id proves the SQL-literal quoting held (a naive
	// interpolation would terminate the literal early).
	if err := c.SetVerified(ctx, "o'brien"); err != nil {
		t.Fatalf("set verified: %v", err)
	}
	if err := c.DeleteRow(ctx, "id-1"); err != nil {
		t.Fatalf("delete: %v", err)
	}

	q := f.queries()
	if len(q) != 4 {
		t.Fatalf("sent %d queries, want 4: %q", len(q), q)
	}
	for _, want := range []string{
		"SELECT id,name,score,level,mode,day,engine_version,replay",
		"surface,user_agent,term,colorterm,input_regime,viewport",
		"FROM scores WHERE verified = false AND replay IS NOT NULL",
		"ORDER BY created_at ASC",
		"LIMIT 5",
	} {
		if !strings.Contains(q[0], want) {
			t.Errorf("Pending SQL %q missing %q", q[0], want)
		}
	}
	if strings.Contains(q[1], "verified") {
		t.Errorf("Latest SQL %q must not filter on verified", q[1])
	}
	for _, want := range []string{"replay IS NOT NULL", "ORDER BY created_at DESC", "LIMIT 3"} {
		if !strings.Contains(q[1], want) {
			t.Errorf("Latest SQL %q missing %q", q[1], want)
		}
	}
	if got, want := q[2], `UPDATE scores SET verified = true WHERE id = 'o''brien'`; got != want {
		t.Errorf("SetVerified SQL = %q, want %q", got, want)
	}
	if got, want := q[3], `DELETE FROM scores WHERE id = 'id-1'`; got != want {
		t.Errorf("DeleteRow SQL = %q, want %q", got, want)
	}
}

// TestDBClientColumnGuard: a query result that no longer has 14 columns
// (schema drift) must error out, not mis-parse into a PendingRow.
func TestDBClientColumnGuard(t *testing.T) {
	short := [][]byte{nil, []byte("DAVE"), []byte("500"), []byte("2"),
		[]byte("classic"), nil, []byte(EngineVersion), []byte("{}"),
		[]byte("ssh"), nil, []byte("xterm"), nil, []byte("legacy")}
	f := newFakeDB(t, func(string) [][]byte {
		return append([][]byte{dbRowDesc(len(short))}, dbDataRow(short...), dbCmdTag("SELECT 1"), dbReady())
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := pgwire.Connect(ctx, pgwire.Config{
		Addr: f.ln.Addr().String(), User: "mario", Password: "secret", Database: "mario",
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close()
	c := &DBClient{conn: conn}
	if _, err := c.Pending(ctx, 1); err == nil || !strings.Contains(err.Error(), "want 14") {
		t.Fatalf("err = %v, want a 14-column guard error", err)
	}
}
