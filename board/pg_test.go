package board

import (
	"context"
	"os"
	"strings"
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
	p, err := pendingRowFromValues(cols)
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
	if _, err := pendingRowFromValues(bad); err == nil || !strings.Contains(err.Error(), "score") {
		t.Errorf("want score parse error, got %v", err)
	}
	bad = append([]pgwire.Value{}, cols...)
	bad[0] = nul
	if _, err := pendingRowFromValues(bad); err == nil || !strings.Contains(err.Error(), "NULL id") {
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
