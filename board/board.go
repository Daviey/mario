// Package board talks to the Supabase-hosted high score table over its
// PostgREST API. The publishable key (SUPABASE_URL/SUPABASE_KEY, typically
// in .env) is embedded in every client; RLS limits it to inserting and
// reading score rows. Scores are client-attested — no verification layer.
package board

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// Client is a configured PostgREST endpoint.
type Client struct {
	BaseURL string // e.g. https://xyz.supabase.co
	Key     string // publishable key
	HTTP    *http.Client
}

// New returns a client for the given Supabase project URL and key.
func New(baseURL, key string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Key: key, HTTP: &http.Client{Timeout: 15 * time.Second}}
}

// DefaultURL/DefaultKey are the build-time embedded project URL and
// publishable key, injected via -ldflags -X (see the Makefile web target).
// The publishable key is safe to embed — RLS limits it to anon insert +
// public read. They exist so the WASM build, which has no environment
// variables, can still reach the leaderboard; real env vars always win.
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
// Submit (the server's proof-of-work gate rejects rows without a valid one).
type Entry struct {
	Name     string `json:"name"`
	Score    int    `json:"score"`
	DeviceID string `json:"device_id"`
	PowNonce string `json:"pow_nonce"`
	Mode     string `json:"mode,omitempty"` // "" / "classic" / "daily"
	Day      string `json:"day,omitempty"`  // daily rows: YYYY-MM-DD
}

// Row is a leaderboard entry as served by the board_rows RPC. The raw
// device_id never leaves the database; mine-ness arrives precomputed.
type Row struct {
	Name      string    `json:"name"`
	Score     int       `json:"score"`
	Mine      bool      `json:"mine"`
	CreatedAt time.Time `json:"created_at"`
}

// Submit inserts a score row, solving the server's proof-of-work gate
// first (~0.1s of hashing; invisible next to the network round trip).
func (c *Client) Submit(ctx context.Context, e Entry) error {
	if e.Mode == "" {
		e.Mode = "classic"
	}
	e.PowNonce = solvePow(e.DeviceID, e.Score)
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
		rows[i].Name = sanitizeDisplayName(r.Name)
	}
	return rows, nil
}

// sanitizeDisplayName clamps a peer-supplied name to the documented safe charset
// and length before it reaches the terminal or DOM.
func sanitizeDisplayName(s string) string {
	var b []rune
	for _, r := range s {
		if r >= 'a' && r <= 'z' {
			r -= 'a' - 'A'
		}
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b = append(b, r)
		case r == ' ' || r == '-' || r == '.':
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

func (c *Client) do(ctx context.Context, method, path string, q url.Values, body []byte, hdr ...string) ([]byte, error) {
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
	out, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(out))
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return nil, fmt.Errorf("%s %s: %s: %s", method, path, resp.Status, msg)
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
