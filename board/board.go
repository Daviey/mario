// Package board talks to the Supabase-hosted high score table over its
// PostgREST API. The publishable key (SUPABASE_URL/SUPABASE_KEY, typically
// in .env) is embedded in every client; RLS limits it to inserting and
// reading score rows. Every submission carries a replay recording that the
// verifier (GitHub Action, service key) replays to confirm the score.
package board

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Daviey/mario/internal/persist"
)

// EngineVersion marks the gameplay build a replay was recorded on. Bump it
// on ANY engine/level change — the verifier rejects rows it cannot trust.
const EngineVersion = "2026.08.30b"

// Client is a configured PostgREST endpoint.
type Client struct {
	BaseURL   string // e.g. https://xyz.supabase.co
	Key       string // publishable key
	UserAgent string // HTTP User-Agent; empty = mario/<EngineVersion>
	HTTP      *http.Client
}

// New returns a client for the given Supabase project URL and key.
func New(baseURL, key string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Key: key, HTTP: &http.Client{Timeout: 15 * time.Second}}
}

// DefaultURL/DefaultKey are the build-time embedded project URL and
// publishable key, injected via -ldflags -X (see the Makefile web target).
// The publishable key is safe to embed — RLS limits it to anon insert +
// public read. They exist so the WASM build, which has no environment
// variables, can still reach the leaderboard; real env variables always win.
var DefaultURL, DefaultKey string

// FromEnv builds a client from SUPABASE_URL and SUPABASE_KEY, falling
// back to the embedded build-time defaults when the variables are unset.
func FromEnv() (*Client, error) {
	u, k := os.Getenv("SUPABASE_URL"), os.Getenv("SUPABASE_KEY")
	if u == "" {
		u = DefaultURL
	}
	if k == "" {
		k = DefaultKey
	}
	if u == "" || k == "" {
		return nil, fmt.Errorf("SUPABASE_URL and SUPABASE_KEY must be set (see .env)")
	}
	return New(u, k), nil
}

// Entry is a score submission from a player. PowNonce is filled in by
// Submit (the server's proof-of-work gate rejects rows without a valid one);
// so is EngineVersion. Replay is the run's input log (replay package wire
// format) — the server requires it on every new row.
type Entry struct {
	Name          string `json:"name"`
	Score         int    `json:"score"`
	Level         int    `json:"level"` // 1-based level reached (0 reads as 1)
	DeviceID      string `json:"device_id"`
	PowNonce      string `json:"pow_nonce"`
	Mode          string `json:"mode,omitempty"` // "" / "classic" / "daily"
	Day           string `json:"day,omitempty"`  // daily rows: YYYY-MM-DD
	EngineVersion string `json:"engine_version"`
	Replay        string `json:"replay"`

	// Play context — operator-only diagnostics, hidden from anon by the
	// DB column grants (like device_id and ip): where and how the run
	// was played.
	Surface     string `json:"surface,omitempty"`      // local / ssh / web
	UserAgent   string `json:"user_agent,omitempty"`   // web: the browser's UA
	Term        string `json:"term,omitempty"`         // TERM from pty-req / env
	ColorTerm   string `json:"colorterm,omitempty"`    // COLORTERM when present
	InputRegime string `json:"input_regime,omitempty"` // kitty / legacy
	Viewport    string `json:"viewport,omitempty"`     // WxH tiles at submit
}

// viewportPat is the one shape the viewport diagnostic may take.
var viewportPat = regexp.MustCompile(`^[0-9]+x[0-9]+$`)

// ipPat accepts IPv4/IPv6 literals only — the plays table's ip column
// carries the player's SSH origin host, never a hostname.
var ipPat = regexp.MustCompile(`^[0-9a-fA-F.:]+$`)

// ClampPlayContext bounds the client-supplied diagnostic fields so a hostile
// client cannot stuff the operator's table (the DB CHECKs mirror this).
func ClampPlayContext(e *Entry) {
	e.Surface = clampMeta(e.Surface, 16)
	if e.Surface != "local" && e.Surface != "ssh" && e.Surface != "web" {
		e.Surface = ""
	}
	e.UserAgent = clampMeta(e.UserAgent, 256)
	e.Term = clampMeta(e.Term, 64)
	e.ColorTerm = clampMeta(e.ColorTerm, 32)
	if e.InputRegime != "kitty" && e.InputRegime != "legacy" {
		e.InputRegime = ""
	}
	if !viewportPat.MatchString(e.Viewport) {
		e.Viewport = ""
	}
}

// clampMeta drops control characters and caps the length.
func clampMeta(s string, n int) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	if len(s) > n {
		s = s[:n]
	}
	return s
}

// Row is a leaderboard entry as served by the board_rows RPC. The raw
// device_id never leaves the database; mine-ness arrives precomputed;
// verified rows carry a replay-confirmed mark.
type Row struct {
	Name      string    `json:"name"`
	Score     int       `json:"score"`
	Level     int       `json:"level"`
	Mine      bool      `json:"mine"`
	Verified  bool      `json:"verified"`
	CreatedAt time.Time `json:"created_at"`
}

// Submit inserts a score row, solving the server's proof-of-work gate
// first (~0.1s of hashing; invisible next to the network round trip).
func (c *Client) Submit(ctx context.Context, e Entry) error {
	if e.Mode == "" {
		e.Mode = "classic"
	}
	if e.EngineVersion == "" {
		e.EngineVersion = EngineVersion
	}
	e.PowNonce = solvePow(e.DeviceID, e.Score)
	e.Level = clampLevel(e.Level)
	ClampPlayContext(&e)
	body, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = c.do(ctx, http.MethodPost, "/rest/v1/scores", nil, body,
		"Prefer", "return=minimal")
	return err
}

// Top returns the n best classic-mode scores, highest first. Rows
// belonging to deviceID arrive flagged Mine; "" gives the anonymous view.
func (c *Client) Top(ctx context.Context, n int, deviceID string) ([]Row, error) {
	return c.TopMode(ctx, n, deviceID, "", "")
}

// TopMode returns the n best scores for a leaderboard mode. mode "daily"
// additionally filters to one challenge day (YYYY-MM-DD); the classic
// board ignores day.
func (c *Client) TopMode(ctx context.Context, n int, deviceID, mode, day string) ([]Row, error) {
	args := map[string]any{"p_limit": n}
	if deviceID != "" {
		args["p_device_id"] = deviceID
	}
	if mode != "" {
		args["p_mode"] = mode
	}
	if mode == "daily" && day != "" {
		args["p_day"] = day
	}
	body, err := json.Marshal(args)
	if err != nil {
		return nil, err
	}
	out, err := c.do(ctx, http.MethodPost, "/rest/v1/rpc/board_rows", nil, body)
	if err != nil {
		return nil, err
	}
	var rows []Row
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, fmt.Errorf("decode scores: %w", err)
	}
	for i, r := range rows {
		rows[i].Name = SanitizeDisplayName(r.Name)
		rows[i].Level = clampLevel(r.Level)
	}
	return rows, nil
}

// PendingRow is an unverified submission awaiting replay verification.
// Only the service-role verifier may see these. The play-context columns
// ride along for the operator log; anon never sees them.
type PendingRow struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Score         int    `json:"score"`
	Level         int    `json:"level"`
	Mode          string `json:"mode"`
	Day           string `json:"day"`
	EngineVersion string `json:"engine_version"`
	Replay        string `json:"replay"`
	Surface       string `json:"surface"`
	UserAgent     string `json:"user_agent"`
	Term          string `json:"term"`
	ColorTerm     string `json:"colorterm"`
	InputRegime   string `json:"input_regime"`
	Viewport      string `json:"viewport"`
}

// Pending fetches up to n unverified rows that carry a replay. Requires a
// client built with the service-role key.
func (c *Client) Pending(ctx context.Context, n int) ([]PendingRow, error) {
	q := url.Values{
		"select":   {`id,name,score,level,mode,day,engine_version,replay,surface,user_agent,term,colorterm,input_regime,viewport`},
		"verified": {"eq.false"},
		"replay":   {"not.is.null"},
		"order":    {"created_at.asc"},
		"limit":    {strconv.Itoa(n)},
	}
	out, err := c.doCap(ctx, http.MethodGet, "/rest/v1/scores", q, nil, 8<<20)
	if err != nil {
		return nil, err
	}
	var rows []PendingRow
	if err := json.Unmarshal(out, &rows); err != nil {
		return nil, fmt.Errorf("decode pending: %w", err)
	}
	return rows, nil
}

// SetVerified marks a row's replay as confirmed. Service-role only.
func (c *Client) SetVerified(ctx context.Context, id string) error {
	body, err := json.Marshal(map[string]bool{"verified": true})
	if err != nil {
		return err
	}
	_, err = c.do(ctx, http.MethodPatch, "/rest/v1/scores",
		url.Values{"id": {"eq." + id}}, body)
	return err
}

// DeleteRow removes a row (failed verification). Service-role only.
func (c *Client) DeleteRow(ctx context.Context, id string) error {
	_, err := c.do(ctx, http.MethodDelete, "/rest/v1/scores",
		url.Values{"id": {"eq." + id}}, nil)
	return err
}

// PlaySession is one server-side play session's telemetry (the plays
// table): written by the SSH game host at disconnect. The game only ever
// inserts — anon has no read path on plays at all — and the rows
// originate from our servers only (player machines never phone home).
type PlaySession struct {
	IP            string    `json:"ip"` // player's SSH origin host
	StartedAt     time.Time `json:"started_at"`
	EndedAt       time.Time `json:"ended_at"`
	Level         int       `json:"level"`     // deepest level reached
	Score         int       `json:"score"`     // best score in the session
	Submitted     bool      `json:"submitted"` // a score row landed from this session
	Runs          int       `json:"runs"`      // games started (recording-arming rule)
	Term          string    `json:"term,omitempty"`
	ColorTerm     string    `json:"colorterm,omitempty"` // the DECIDED depth signal, not the raw env
	Colors        int       `json:"colors"`              // 24 (truecolor), 256 (fixed xterm cube) or 16 (ANSI fallback)
	Client        string    `json:"client,omitempty"`    // SSH identification string (ssh-surface UA)
	InputRegime   string    `json:"input_regime,omitempty"`
	Viewport      string    `json:"viewport,omitempty"`
	EngineVersion string    `json:"engine_version"`
}

// ClampPlaySession bounds the telemetry fields the same way
// ClampPlayContext bounds submissions (the plays CHECKs mirror this).
func ClampPlaySession(p *PlaySession) {
	p.IP = clampMeta(p.IP, 45)
	if !ipPat.MatchString(p.IP) {
		p.IP = ""
	}
	p.Term = clampMeta(p.Term, 64)
	p.ColorTerm = clampMeta(p.ColorTerm, 32)
	p.Client = clampMeta(p.Client, 128)
	if p.Colors != 24 && p.Colors != 256 {
		p.Colors = 16
	}
	if p.InputRegime != "kitty" && p.InputRegime != "legacy" {
		p.InputRegime = ""
	}
	if !viewportPat.MatchString(p.Viewport) {
		p.Viewport = ""
	}
	p.EngineVersion = clampMeta(p.EngineVersion, 32)
	p.Level = clampLevel(p.Level)
	if p.Score < 0 {
		p.Score = 0
	}
	if p.Score > 9999999 {
		p.Score = 9999999
	}
	if p.Runs < 0 {
		p.Runs = 0
	}
	if p.EndedAt.Before(p.StartedAt) {
		p.EndedAt = p.StartedAt
	}
}

// RecordPlay inserts one session-telemetry row. Fire-and-forget by
// design: callers log errors, never surface them to gameplay.
func (c *Client) RecordPlay(ctx context.Context, p PlaySession) error {
	if p.EngineVersion == "" {
		p.EngineVersion = EngineVersion
	}
	ClampPlaySession(&p)
	body, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = c.do(ctx, http.MethodPost, "/rest/v1/plays", nil, body,
		"Prefer", "return=minimal")
	return err
}

// clampLevel bounds a level to the DB CHECK (1..99). Zero maps to 1 —
// legacy rows and callers that don't track levels read the same way.
func clampLevel(n int) int {
	if n < 1 {
		return 1
	}
	if n > 99 {
		return 99
	}
	return n
}

// HTTPError is a non-2xx HTTP response, kept typed so callers can
// branch on the status (retry policy) instead of parsing the message.
// It renders exactly as the old fmt.Errorf did.
type HTTPError struct {
	Method     string
	Path       string
	Status     int
	StatusText string // e.g. "400 Bad Request"
	Body       string // first 300 bytes, for the operator log
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("%s %s: %s: %s", e.Method, e.Path, e.StatusText, e.Body)
}

// Transient reports whether err is worth one retry: transport-level
// failures (timeouts, resets, DNS — anything without an HTTP status)
// and 5xx server errors. A status the server chose deliberately (4xx:
// PoW/RLS rejections, 429 rate limits, bad requests) fails identically
// again; retrying it only doubles the latency.
func Transient(err error) bool {
	if err == nil {
		return false
	}
	var he *HTTPError
	if errors.As(err, &he) {
		return he.Status >= 500
	}
	return true
}

// SanitizeDisplayName clamps a peer-supplied name to the documented safe
// charset and length before it reaches the terminal, DOM or operator log.
// The accepted runes are persist.NameCharSet — the one Go-side charset.
func SanitizeDisplayName(s string) string {
	var b []rune
	for _, r := range s {
		if r >= 'a' && r <= 'z' {
			r -= 'a' - 'A'
		}
		if r < utf8.RuneSelf && strings.ContainsRune(persist.NameCharSet, r) {
			b = append(b, r)
		}
		if len(b) == 8 {
			break
		}
	}
	if len(b) == 0 {
		return "-"
	}
	return string(b)
}

// do performs a request with the default 1 MiB response cap.
func (c *Client) do(ctx context.Context, method, path string, q url.Values, body []byte, hdr ...string) ([]byte, error) {
	return c.doCap(ctx, method, path, q, body, 1<<20, hdr...)
}

// doCap is do with an explicit response-size cap. Endpoints that legitimately
// return more (Pending carries 256 KB replay strings per row) must pass a cap
// strictly larger than the worst case, or a big row silently truncates the
// body and the JSON decode fails with a confusing error.
func (c *Client) doCap(ctx context.Context, method, path string, q url.Values, body []byte, cap int64, hdr ...string) ([]byte, error) {
	u := c.BaseURL + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rdr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("apikey", c.Key)
	req.Header.Set("Authorization", "Bearer "+c.Key)
	ua := c.UserAgent
	if ua == "" {
		ua = "mario/" + EngineVersion
	}
	req.Header.Set("User-Agent", ua)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	out, err := io.ReadAll(io.LimitReader(resp.Body, cap))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(out))
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return nil, &HTTPError{Method: method, Path: path, Status: resp.StatusCode, StatusText: resp.Status, Body: msg}
	}
	return out, nil
}

// LoadDotEnv reads KEY=VALUE lines from each existing path and exports any
// variable that is not already set in the environment. Real environment
// variables always win; files are tried in order, first hit wins.
func LoadDotEnv(paths ...string) {
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			k, v, ok := strings.Cut(line, "=")
			if !ok {
				continue
			}
			k = strings.TrimSpace(k)
			v = strings.TrimSpace(v)
			if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
				v = v[1 : len(v)-1]
			}
			if k != "" && os.Getenv(k) == "" {
				os.Setenv(k, v)
			}
		}
	}
}
