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
	"strconv"
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
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), Key: key, HTTP: http.DefaultClient}
}

// FromEnv builds a client from SUPABASE_URL and SUPABASE_KEY.
func FromEnv() (*Client, error) {
	u, k := os.Getenv("SUPABASE_URL"), os.Getenv("SUPABASE_KEY")
	if u == "" || k == "" {
		return nil, fmt.Errorf("SUPABASE_URL and SUPABASE_KEY must be set (see .env)")
	}
	return New(u, k), nil
}

// Entry is a score submission from a player.
type Entry struct {
	Name     string `json:"name"`
	Score    int    `json:"score"`
	DeviceID string `json:"device_id"`
}

// Row is a scores table row.
type Row struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Score     int       `json:"score"`
	CreatedAt time.Time `json:"created_at"`
}

// Submit inserts a score row.
func (c *Client) Submit(ctx context.Context, e Entry) error {
	body, err := json.Marshal(e)
	if err != nil {
		return err
	}
	_, err = c.do(ctx, http.MethodPost, "/rest/v1/scores", nil, body,
		"Prefer", "return=minimal")
	return err
}

// Top returns the n best scores, highest first.
func (c *Client) Top(ctx context.Context, n int) ([]Row, error) {
	q := url.Values{}
	q.Set("select", "id,name,score,created_at")
	q.Set("order", "score.desc")
	q.Set("limit", strconv.Itoa(n))
	body, err := c.do(ctx, http.MethodGet, "/rest/v1/scores", q, nil)
	if err != nil {
		return nil, err
	}
	var rows []Row
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil, fmt.Errorf("decode scores: %w", err)
	}
	return rows, nil
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
