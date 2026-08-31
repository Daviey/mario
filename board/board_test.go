package board

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Daviey/mario/internal/persist"
)

// fakePostgREST records requests and answers with canned JSON bodies.
// Tests here drive it strictly sequentially, so no locking is needed.
type fakePostgREST struct {
	requests []http.Request
	bodies   []string
	resp     func(r *http.Request, body string) (int, string)
}

func newFake(resp func(*http.Request, string) (int, string)) *fakePostgREST {
	return &fakePostgREST{resp: resp}
}

func (f *fakePostgREST) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.requests = append(f.requests, *r)
	f.bodies = append(f.bodies, string(body))
	code, out := f.resp(r, string(body))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	io.WriteString(w, out)
}

func (f *fakePostgREST) last(t *testing.T) *http.Request {
	t.Helper()
	if len(f.requests) == 0 {
		t.Fatal("no requests recorded")
	}
	return &f.requests[len(f.requests)-1]
}

func testClient(t *testing.T, resp func(*http.Request, string) (int, string)) (*Client, *fakePostgREST) {
	t.Helper()
	f := newFake(resp)
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return New(srv.URL, "testkey"), f
}

func TestSubmitWire(t *testing.T) {
	client, f := testClient(t, func(*http.Request, string) (int, string) { return 201, "" })
	e := Entry{Name: "DAVE", Score: 12500, Level: 3, DeviceID: "d", Replay: `{"v":1,"ticks":2,"runs":[[0,2]]}`}
	if err := client.Submit(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	req := f.last(t)
	if req.Method != http.MethodPost || req.URL.Path != "/rest/v1/scores" {
		t.Fatalf("got %s %s", req.Method, req.URL.Path)
	}
	if got := req.Header.Get("apikey"); got != "testkey" {
		t.Errorf("apikey = %q", got)
	}
	if got := req.Header.Get("Authorization"); got != "Bearer testkey" {
		t.Errorf("Authorization = %q", got)
	}
	if got := req.Header.Get("Prefer"); got != "return=minimal" {
		t.Errorf("Prefer = %q", got)
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(f.bodies[len(f.bodies)-1]), &sent); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"name": "DAVE", "score": float64(12500), "level": float64(3), "device_id": "d", "pow_nonce": sent["pow_nonce"], "mode": "classic",
		"engine_version": EngineVersion, "replay": e.Replay}
	if len(sent) != len(want) {
		t.Errorf("body = %v, want exactly %v", sent, want)
	}
	for k, v := range want {
		if sent[k] != v {
			t.Errorf("body[%s] = %v, want %v", k, sent[k], v)
		}
	}
}

func TestSubmitError(t *testing.T) {
	client, _ := testClient(t, func(*http.Request, string) (int, string) {
		return 401, `{"code":"42501","message":"rls"}` // rejection must surface
	})
	err := client.Submit(context.Background(), Entry{Name: "X", Score: 1})
	if err == nil || !strings.Contains(err.Error(), "42501") {
		t.Fatalf("want status/body in error, got %v", err)
	}
}

func TestSubmitPlayContext(t *testing.T) {
	client, f := testClient(t, func(*http.Request, string) (int, string) { return 201, "" })
	e := Entry{Name: "DAVE", Score: 10, DeviceID: "d", Replay: `{"v":1}`,
		Surface: "ssh", UserAgent: "Mozilla/5.0 (test) " + strings.Repeat("x", 300),
		Term: "xterm-256color", ColorTerm: "truecolor", InputRegime: "kitty", Viewport: "40x15"}
	if err := client.Submit(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(f.bodies[len(f.bodies)-1]), &sent); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]any{
		"surface": "ssh",
		"term":    "xterm-256color", "colorterm": "truecolor",
		"input_regime": "kitty", "viewport": "40x15",
	} {
		if sent[k] != want {
			t.Errorf("body[%s] = %v, want %v", k, sent[k], want)
		}
	}
	if ua, ok := sent["user_agent"].(string); !ok || len(ua) != 256 || !strings.HasPrefix(ua, "Mozilla/5.0 (test) ") {
		t.Errorf("user_agent not capped/passed: %v", sent["user_agent"])
	}
	if got := f.last(t).Header.Get("User-Agent"); got != "mario/"+EngineVersion {
		t.Errorf("User-Agent header = %q", got)
	}
}

func TestClampPlayContextRejectsJunk(t *testing.T) {
	e := &Entry{Surface: "browser", InputRegime: "touch", Viewport: "40x15; drop table"}
	ClampPlayContext(e)
	if e.Surface != "" || e.InputRegime != "" || e.Viewport != "" {
		t.Errorf("junk context survived: %+v", e)
	}
}

func TestTopRPCAndDecode(t *testing.T) {
	client, f := testClient(t, func(*http.Request, string) (int, string) {
		return 200, `[{"name":"DAVE","score":12500,"level":3,"mine":true,"created_at":"2026-08-25T12:00:00Z"}]`
	})
	rows, err := client.Top(context.Background(), 10, "d")
	if err != nil {
		t.Fatal(err)
	}
	req := f.last(t)
	if req.Method != http.MethodPost || req.URL.Path != "/rest/v1/rpc/board_rows" {
		t.Fatalf("got %s %s", req.Method, req.URL.Path)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(f.bodies[len(f.bodies)-1]), &args); err != nil {
		t.Fatal(err)
	}
	if args["p_device_id"] != "d" || args["p_limit"] != float64(10) {
		t.Errorf("rpc args = %v", args)
	}
	if len(rows) != 1 || rows[0].Name != "DAVE" || rows[0].Score != 12500 || rows[0].Level != 3 || !rows[0].Mine {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestTopAnonymous(t *testing.T) {
	var body string
	client, _ := testClient(t, func(_ *http.Request, b string) (int, string) {
		body = b
		return 200, `[]`
	})
	rows, err := client.Top(context.Background(), 5, "")
	if err != nil || len(rows) != 0 {
		t.Fatalf("rows = %+v, err = %v", rows, err)
	}
	if strings.Contains(body, "p_device_id") {
		t.Errorf("anonymous view must not send p_device_id: %s", body)
	}
}

func TestTopEmpty(t *testing.T) {
	client, _ := testClient(t, func(*http.Request, string) (int, string) { return 200, `[]` })
	rows, err := client.Top(context.Background(), 5, "")
	if err != nil || len(rows) != 0 {
		t.Fatalf("rows = %+v, err = %v", rows, err)
	}
}

// TestTopModeArgShaping pins how TopMode shapes the rpc body: the daily
// board carries p_mode and p_day (plus p_device_id when known), while
// the classic board (Top, mode "") sends neither — board_rows defaults
// p_mode to 'classic' itself, so the wire stays minimal on the hot path.
func TestTopModeArgShaping(t *testing.T) {
	var body string
	client, _ := testClient(t, func(_ *http.Request, b string) (int, string) {
		body = b
		return 200, `[]`
	})
	ctx := context.Background()

	if _, err := client.TopMode(ctx, 10, "dev-1", "daily", "2026-08-31"); err != nil {
		t.Fatal(err)
	}
	var args map[string]any
	if err := json.Unmarshal([]byte(body), &args); err != nil {
		t.Fatal(err)
	}
	for k, want := range map[string]any{
		"p_limit": float64(10), "p_device_id": "dev-1",
		"p_mode": "daily", "p_day": "2026-08-31",
	} {
		if args[k] != want {
			t.Errorf("daily rpc args[%s] = %v, want %v", k, args[k], want)
		}
	}

	if _, err := client.Top(ctx, 10, "dev-1"); err != nil {
		t.Fatal(err)
	}
	args = nil
	if err := json.Unmarshal([]byte(body), &args); err != nil {
		t.Fatal(err)
	}
	if args["p_limit"] != float64(10) || args["p_device_id"] != "dev-1" {
		t.Errorf("classic rpc args = %v, want only p_limit and p_device_id", args)
	}
	for _, k := range []string{"p_mode", "p_day"} {
		if _, ok := args[k]; ok {
			t.Errorf("classic rpc args must omit %s: %v", k, args)
		}
	}
}

func TestFromEnv(t *testing.T) {
	t.Setenv("SUPABASE_URL", "")
	t.Setenv("SUPABASE_KEY", "")
	if _, err := FromEnv(); err == nil {
		t.Fatal("missing env must error")
	}
	// The error must name exactly the missing variable(s).
	t.Setenv("SUPABASE_URL", "")
	t.Setenv("SUPABASE_KEY", "k")
	_, err := FromEnv()
	if err == nil || !strings.Contains(err.Error(), "SUPABASE_URL") || strings.Contains(err.Error(), "SUPABASE_KEY") {
		t.Fatalf("err = %v, want it to name SUPABASE_URL only", err)
	}
	t.Setenv("SUPABASE_URL", "https://x.supabase.co/")
	t.Setenv("SUPABASE_KEY", "k")
	c, err := FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if c.BaseURL != "https://x.supabase.co" {
		t.Errorf("trailing slash not trimmed: %q", c.BaseURL)
	}
}

func TestLoadDotEnv(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "a.env")
	b := filepath.Join(dir, "b.env")
	missing := filepath.Join(dir, "missing.env")
	os.WriteFile(a, []byte("# comment\n\nURL=https://a\nQUOTED=\"quoted value\"\nBROKEN\nKEYONLY=\n"), 0o644)
	os.WriteFile(b, []byte("URL=https://b\n"), 0o644)

	t.Setenv("URL", "")       // set by file a
	t.Setenv("QUOTED", "")    // set by file a, quoted
	t.Setenv("KEYONLY", "")   // empty value stays empty
	t.Setenv("PRIOR", "real") // real env wins over files

	LoadDotEnv(a, b, missing)

	if got := os.Getenv("URL"); got != "https://a" {
		t.Errorf("URL = %q, want file-a value (first file wins)", got)
	}
	if got := os.Getenv("QUOTED"); got != "quoted value" {
		t.Errorf("QUOTED = %q", got)
	}
	if got := os.Getenv("PRIOR"); got != "real" {
		t.Errorf("PRIOR = %q, real env must win", got)
	}
	if got := os.Getenv("KEYONLY"); got != "" {
		t.Errorf("KEYONLY = %q, want empty", got)
	}
}

func TestFromEnvFallsBackToDefaults(t *testing.T) {
	t.Setenv("SUPABASE_URL", "")
	t.Setenv("SUPABASE_KEY", "")
	origURL, origKey := DefaultURL, DefaultKey
	defer func() { DefaultURL, DefaultKey = origURL, origKey }()
	DefaultURL, DefaultKey = "https://example.supabase.co", "anon-key"
	c, err := FromEnv()
	if err != nil {
		t.Fatalf("FromEnv with defaults: %v", err)
	}
	if c.BaseURL != "https://example.supabase.co" || c.Key != "anon-key" {
		t.Fatalf("client = %+v, want embedded defaults", c)
	}
	// Real environment variables always win over the embedded defaults.
	t.Setenv("SUPABASE_URL", "https://env.supabase.co")
	c, err = FromEnv()
	if err != nil || c.BaseURL != "https://env.supabase.co" {
		t.Fatalf("env should win: %+v err=%v", c, err)
	}
}

func TestSanitizeDisplayName(t *testing.T) {
	cases := []struct{ in, want string }{
		{"DAVE", "DAVE"},
		{"dave", "DAVE"},             // lowercased to the documented charset
		{"A\x1b[31mX", "A31MX"},      // ESC sequence stripped
		{"<b>x</b>", "BXB"},          // markup stripped (angle brackets dropped)
		{"a\u202Eb", "AB"},           // bidi override stripped
		{"123456789012", "12345678"}, // clamped to 8 runes
		{"\x01\x02\x03", "-"},        // nothing left -> placeholder
	}
	for _, c := range cases {
		if got := SanitizeDisplayName(c.in); got != c.want {
			t.Errorf("SanitizeDisplayName(%q) = %q want %q", c.in, got, c.want)
		}
	}
}

// persist.NameCharSet is the single Go-side source of truth for player
// names: both Go sanitizers must accept exactly it and reject the same
// outsiders ('_' has no pixel-font glyph anywhere).
func TestNameCharSetSingleSource(t *testing.T) {
	for _, r := range persist.NameCharSet {
		// Wrapped: SanitizeName's length floor is 1 char, and a lone
		// space also trips its trim — the rune set is what must agree.
		s := "X" + string(r) + "Y"
		if got := SanitizeDisplayName(s); got != s {
			t.Errorf("SanitizeDisplayName(%q) = %q: charset rune not accepted", s, got)
		}
		if got, ok := persist.SanitizeName(s); !ok || got != s {
			t.Errorf("SanitizeName(%q) = %q,%v: charset rune not accepted", s, got, ok)
		}
	}
	if _, ok := persist.SanitizeName("x_y"); ok {
		t.Error(`SanitizeName("x_y") accepted '_'`)
	}
	if got := SanitizeDisplayName("x_y"); got != "XY" {
		t.Errorf(`SanitizeDisplayName("x_y") = %q, want XY ('_' dropped)`, got)
	}
}

func TestTransientClassification(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{&HTTPError{Status: 500, StatusText: "500 Internal Server Error"}, true},
		{&HTTPError{Status: 502, StatusText: "502 Bad Gateway"}, true},
		{&HTTPError{Status: 503, StatusText: "503 Service Unavailable"}, true},
		{&HTTPError{Status: 400, StatusText: "400 Bad Request"}, false},  // bad request
		{&HTTPError{Status: 401, StatusText: "401 Unauthorized"}, false}, // RLS
		{&HTTPError{Status: 429, StatusText: "429 Too Many Requests"}, false},
		{errors.New("connection reset by peer"), true},          // transport-level
		{fmt.Errorf("top: %w", &HTTPError{Status: 429}), false}, // wrapped
		{nil, false},
	}
	for _, c := range cases {
		if got := Transient(c.err); got != c.want {
			t.Errorf("Transient(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

// Top must sanitize every fetched row: peer-stored legacy rows may violate
// the charset the display paths assume (regression for terminal injection).
func TestTopSanitizesNames(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("apikey")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `[{"id":"11111111-1111-1111-1111-111111111111","name":"A\u001b[31mX","score":1,"device_id":"22222222-2222-2222-2222-222222222222","created_at":"2026-08-26T00:00:00Z"}]`)
	}))
	defer srv.Close()
	c := New(srv.URL, "testkey")
	rows, err := c.Top(context.Background(), 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Name != "A31MX" {
		t.Fatalf("rows = %+v want sanitized name A31MX", rows)
	}
	if got != "testkey" {
		t.Fatalf("apikey header = %q", got)
	}
}

// Level rides along on the wire and is clamped to the DB CHECK bounds on
// both paths: zero maps to 1 (legacy rows), overflow to 99.
func TestLevelClamped(t *testing.T) {
	client, f := testClient(t, func(*http.Request, string) (int, string) { return 201, "" })
	for _, lvl := range []int{0, 1, 99, 500} {
		if err := client.Submit(context.Background(), Entry{Name: "X", Score: 1, Level: lvl, DeviceID: "d"}); err != nil {
			t.Fatal(err)
		}
		var sent map[string]any
		if err := json.Unmarshal([]byte(f.bodies[len(f.bodies)-1]), &sent); err != nil {
			t.Fatal(err)
		}
		want := float64(min(max(lvl, 1), 99))
		if sent["level"] != want {
			t.Errorf("level %d sent as %v, want %v", lvl, sent["level"], want)
		}
	}

	client, _ = testClient(t, func(*http.Request, string) (int, string) {
		return 200, `[{"name":"A","score":5,"level":0,"created_at":"2026-08-26T00:00:00Z"},{"name":"B","score":4,"level":1234,"created_at":"2026-08-26T00:00:00Z"}]`
	})
	rows, err := client.Top(context.Background(), 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].Level != 1 || rows[1].Level != 99 {
		t.Fatalf("read-back levels = %d, %d; want 1, 99", rows[0].Level, rows[1].Level)
	}
}

// RecordPlay inserts into the plays table with the write-only shape:
// POST /rest/v1/plays, Prefer return=minimal, clamped fields, and the
// engine version defaulted.
func TestRecordPlay(t *testing.T) {
	client, f := testClient(t, func(*http.Request, string) (int, string) { return 201, "" })
	start := time.Date(2026, 8, 30, 1, 0, 0, 0, time.UTC)
	err := client.RecordPlay(context.Background(), PlaySession{
		IP:          "203.0.113.9\x1b[evil",
		StartedAt:   start,
		EndedAt:     start.Add(time.Minute),
		Level:       3,
		Score:       4200,
		Submitted:   true,
		Runs:        2,
		Term:        "xterm-ghostty",
		Viewport:    "40x14",
		InputRegime: "kitty",
		Colors:      31,
		Client:      "SSH-2.0-OpenSSH_10.4",
	})
	if err != nil {
		t.Fatal(err)
	}
	r := f.last(t)
	if r.Method != "POST" || r.URL.Path != "/rest/v1/plays" {
		t.Fatalf("%s %s, want POST /rest/v1/plays", r.Method, r.URL.Path)
	}
	if got := r.Header.Get("Prefer"); got != "return=minimal" {
		t.Fatalf("Prefer = %q", got)
	}
	var sent map[string]any
	if err := json.Unmarshal([]byte(f.bodies[0]), &sent); err != nil {
		t.Fatal(err)
	}
	// The control character in ip fails the literal-only pattern: the
	// field clamps to empty rather than shipping operator-hostile bytes.
	if sent["ip"] != "" {
		t.Errorf("ip = %v, want empty (clamped)", sent["ip"])
	}
	for k, want := range map[string]any{
		"level": float64(3), "score": float64(4200), "submitted": true,
		"runs": float64(2), "term": "xterm-ghostty", "viewport": "40x14",
		"input_regime": "kitty", "engine_version": EngineVersion,
		"colors": float64(16), // 31 is not a real palette: clamped to 16
		"client": "SSH-2.0-OpenSSH_10.4",
	} {
		if sent[k] != want {
			t.Errorf("%s = %v, want %v", k, sent[k], want)
		}
	}

	// A server error must surface as an error (the caller logs it).
	client, _ = testClient(t, func(*http.Request, string) (int, string) { return 500, "boom" })
	if err := client.RecordPlay(context.Background(), PlaySession{StartedAt: start, EndedAt: start}); err == nil {
		t.Fatal("500 must error")
	}
}

// TestClampPlaySessionColors pins the three-tier colors clamp: 24 and
// 256 pass through, junk folds to 16.
func TestClampPlaySessionColors(t *testing.T) {
	for _, tc := range []struct {
		in, want int
	}{
		{24, 24},
		{256, 256},
		{16, 16},
		{31, 16}, // junk folds to 16
		{0, 16},
	} {
		p := PlaySession{Colors: tc.in}
		ClampPlaySession(&p)
		if p.Colors != tc.want {
			t.Errorf("ClampPlaySession colors %d = %d, want %d", tc.in, p.Colors, tc.want)
		}
	}
}

// TestClampMetaRuneBoundary pins that truncation lands on rune
// boundaries: cutting mid-rune (either after the lead byte or between
// continuation bytes) would ship invalid UTF-8, which Postgres rejects
// outright.
func TestClampMetaRuneBoundary(t *testing.T) {
	for _, tc := range []struct {
		in, want string
		n        int
	}{
		{"abcdef", "abc", 3}, // ASCII: plain cut
		{"aécd", "aé", 3},    // cut between a rune's continuation bytes
		{"aé", "a", 2},       // cut right after the lead byte
		{"é", "", 1},         // nothing of the rune fits
		{"éé", "é", 2},       // cut exactly between runes
		{"日本語", "日本", 6},     // 3-byte runes
	} {
		if got := clampMeta(tc.in, tc.n); got != tc.want {
			t.Errorf("clampMeta(%q, %d) = %q, want %q", tc.in, tc.n, got, tc.want)
		} else if !utf8.ValidString(got) {
			t.Errorf("clampMeta(%q, %d) = %q is not valid UTF-8", tc.in, tc.n, got)
		}
	}
}

// TestClampPlaySessionBounds pins the non-color clamps: the engine
// version's 32-char cap, the score caps (the plays CHECK mirrors them),
// clock ordering, and the ip literal-only rule (hostnames never reach
// the ip column).
func TestClampPlaySessionBounds(t *testing.T) {
	start := time.Unix(1000, 0)
	p := PlaySession{
		IP:            "game.example.com", // hostname: rejected outright
		StartedAt:     start,
		EndedAt:       start.Add(-time.Second),
		EngineVersion: strings.Repeat("x", 40),
		Score:         -5,
	}
	ClampPlaySession(&p)
	if p.IP != "" {
		t.Errorf("hostname ip %q survived the clamp", p.IP)
	}
	if !p.EndedAt.Equal(start) {
		t.Errorf("EndedAt %v before StartedAt %v was not reset", p.EndedAt, start)
	}
	if want := strings.Repeat("x", 32); p.EngineVersion != want {
		t.Errorf("EngineVersion = %d chars, want %d", len(p.EngineVersion), len(want))
	}
	if p.Score != 0 {
		t.Errorf("negative score %d not clamped to 0", p.Score)
	}

	p.Score = 99999999
	ClampPlaySession(&p)
	if p.Score != 9999999 {
		t.Errorf("score %d not clamped to 9999999", p.Score)
	}

	// The runs CHECK is two-sided: negative folds to 0, past the
	// million cap folds to the cap, in-range passes through.
	for _, tc := range []struct{ in, want int }{
		{-3, 0},
		{1_000_001, 1_000_000},
		{42, 42},
	} {
		p.Runs = tc.in
		ClampPlaySession(&p)
		if p.Runs != tc.want {
			t.Errorf("ClampPlaySession runs %d = %d, want %d", tc.in, p.Runs, tc.want)
		}
	}

	// Real literals in both families pass through untouched.
	p.IP, p.Viewport = "192.168.1.5", "80x24"
	ClampPlaySession(&p)
	if p.IP != "192.168.1.5" || p.Viewport != "80x24" {
		t.Errorf("legitimate literal fields clamped: ip=%q viewport=%q", p.IP, p.Viewport)
	}
	p.IP = "::1"
	ClampPlaySession(&p)
	if p.IP != "::1" {
		t.Errorf("ipv6 literal %q rejected", p.IP)
	}
}

// The scores engine_version CHECK (scores_engine_version_len) is mirrored
// on submissions too: a long engine string clamps to 32 chars, the same
// bound ClampPlaySession enforces on telemetry.
func TestClampPlayContextEngineVersion(t *testing.T) {
	e := &Entry{EngineVersion: strings.Repeat("e", 33), Surface: "ssh"}
	ClampPlayContext(e)
	if want := strings.Repeat("e", 32); e.EngineVersion != want {
		t.Errorf("Entry.EngineVersion = %d chars, want %d", len(e.EngineVersion), len(want))
	}
	if e.Surface != "ssh" {
		t.Errorf("unrelated fields must survive: %+v", e)
	}
}
