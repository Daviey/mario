package board

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/Daviey/mario/internal/pgwire"
)

// DBClient runs the verifier's three leaderboard operations directly
// against the Supabase Postgres endpoint, over internal/pgwire. It is the
// operator fallback for `mario -verify-pending` on machines that hold the
// database password (.env SUPABASE_DB_PASSWORD) but not the dashboard
// service key: the postgres role bypasses RLS, so the same pending/keep/
// delete semantics work without PostgREST. The connection is a single
// long-lived simple-query session — verifier runs are sequential.
type DBClient struct {
	conn *pgwire.Conn
}

// DBConfig is the resolved direct-connection parameters.
type DBConfig struct {
	Addr     string // host:port
	User     string
	Password string
	Database string
}

// DBConfigFromEnv resolves the direct-connection parameters:
// SUPABASE_DB_PASSWORD (required), SUPABASE_DB_HOST (optional override —
// the direct endpoint, NOT the pooler), and the project ref taken from
// SUPABASE_URL (or the embedded default) when the host is not overridden.
func DBConfigFromEnv() (DBConfig, error) {
	pw := os.Getenv("SUPABASE_DB_PASSWORD")
	if pw == "" {
		return DBConfig{}, errors.New("board: SUPABASE_DB_PASSWORD not set")
	}
	cfg := DBConfig{
		User:     "postgres",
		Password: pw,
		Database: "postgres",
	}
	if h := os.Getenv("SUPABASE_DB_HOST"); h != "" {
		cfg.Addr = net.JoinHostPort(h, dbPort())
		return cfg, nil
	}
	base := os.Getenv("SUPABASE_URL")
	if base == "" {
		base = DefaultURL
	}
	u, err := url.Parse(base)
	if err != nil || u.Host == "" {
		return DBConfig{}, fmt.Errorf("board: cannot derive db host from SUPABASE_URL %q", base)
	}
	cfg.Addr = net.JoinHostPort("db."+u.Host, dbPort())
	return cfg, nil
}

func dbPort() string {
	if p := os.Getenv("SUPABASE_DB_PORT"); p != "" {
		return p
	}
	return "5432"
}

// DBFromEnv connects to the database directly over TLS. Verification is
// require-mode (encrypt without identity check): Supabase's direct
// endpoint chains to Supabase's own 2021 CA, published only as a
// dashboard download, so system roots cannot verify it — libpq's
// sslmode=require behaves the same way and is Supabase's documented
// default for direct connections. SCRAM still protects the password;
// a MITM can at worst do what the verifier itself does (read pending
// rows, delete liars).
func DBFromEnv(ctx context.Context) (*DBClient, error) {
	cfg, err := DBConfigFromEnv()
	if err != nil {
		return nil, err
	}
	host, _, err := net.SplitHostPort(cfg.Addr)
	if err != nil {
		return nil, fmt.Errorf("board: bad db addr: %w", err)
	}
	conn, err := pgwire.Connect(ctx, pgwire.Config{
		Addr:     cfg.Addr,
		User:     cfg.User,
		Password: cfg.Password,
		Database: cfg.Database,
		TLS:      &tls.Config{ServerName: host, InsecureSkipVerify: true}, // require, not verify-full
	})
	if err != nil {
		return nil, fmt.Errorf("board: db connect: %w", err)
	}
	return &DBClient{conn: conn}, nil
}

// Close ends the session.
func (c *DBClient) Close() error { return c.conn.Close() }

// Pending mirrors Client.Pending over SQL.
func (c *DBClient) Pending(ctx context.Context, n int) ([]PendingRow, error) {
	return c.queryPending(ctx, "pending",
		" WHERE verified = false AND replay IS NOT NULL", "ASC", n)
}

// Latest returns the n most recent replay-backed rows, newest first,
// verified or not — the operator dump behind `mario -dump-replays`.
func (c *DBClient) Latest(ctx context.Context, n int) ([]PendingRow, error) {
	return c.queryPending(ctx, "latest",
		" WHERE replay IS NOT NULL", "DESC", n)
}

// queryPending is the body Pending and Latest share: the 14-column
// scores select with a WHERE fragment, a created_at ordering direction
// and a row limit. label names the caller in errors.
func (c *DBClient) queryPending(ctx context.Context, label, where, dir string, n int) ([]PendingRow, error) {
	rows, err := c.conn.Query(ctx, `SELECT id,name,score,level,mode,day,engine_version,replay,
		surface,user_agent,term,colorterm,input_regime,viewport
		FROM scores`+where+`
		ORDER BY created_at `+dir+` LIMIT `+strconv.Itoa(n))
	if err != nil {
		return nil, fmt.Errorf("db %s: %w", label, err)
	}
	out := make([]PendingRow, 0, len(rows))
	for _, r := range rows {
		if len(r) != 14 {
			return nil, fmt.Errorf("db %s: got %d columns, want 14", label, len(r))
		}
		p, err := pendingRowFromValues(r, label)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// pendingRowFromValues converts one 14-column query result into a
// PendingRow. NULL text columns become "" exactly like the PostgREST JSON
// path (its nulls decode to the zero value). label names the calling
// query in errors.
func pendingRowFromValues(r []pgwire.Value, label string) (PendingRow, error) {
	var p PendingRow
	p.ID = r[0].String()
	p.Name = r[1].String()
	n, err := r[2].Int()
	if err != nil {
		return p, fmt.Errorf("db %s: score: %w", label, err)
	}
	p.Score = n
	n, err = r[3].Int()
	if err != nil {
		return p, fmt.Errorf("db %s: level: %w", label, err)
	}
	p.Level = n
	p.Mode = r[4].String()
	p.Day = r[5].String()
	p.EngineVersion = r[6].String()
	p.Replay = r[7].String()
	p.Surface = r[8].String()
	p.UserAgent = r[9].String()
	p.Term = r[10].String()
	p.ColorTerm = r[11].String()
	p.InputRegime = r[12].String()
	p.Viewport = r[13].String()
	if p.ID == "" {
		return p, fmt.Errorf("db %s: NULL id", label)
	}
	return p, nil
}

// SetVerified mirrors Client.SetVerified over SQL.
func (c *DBClient) SetVerified(ctx context.Context, id string) error {
	_, _, err := c.conn.Exec(ctx,
		`UPDATE scores SET verified = true WHERE id = '`+sqlString(id)+`'`)
	if err != nil {
		return fmt.Errorf("db mark: %w", err)
	}
	return nil
}

// DeleteRow mirrors Client.DeleteRow over SQL.
func (c *DBClient) DeleteRow(ctx context.Context, id string) error {
	_, _, err := c.conn.Exec(ctx,
		`DELETE FROM scores WHERE id = '`+sqlString(id)+`'`)
	if err != nil {
		return fmt.Errorf("db delete: %w", err)
	}
	return nil
}

// sqlString quotes a value for single-quoted SQL literal context by
// doubling embedded single quotes. IDs arrive from the database itself,
// but the guard keeps the helper safe for any caller.
func sqlString(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}
