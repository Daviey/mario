package board

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	e := Entry{
		Name: "DAVE", Score: 12500, Seed: 0,
		Replay:        json.RawMessage(`{"v":1,"i":[1]}`),
		EngineVersion: "v1", DeviceID: "d",
	}
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
	if sent["engine_version"] != "v1" {
		t.Errorf("engine_version = %v", sent["engine_version"])
	}
	// verified must never be client-set.
	if _, ok := sent["verified"]; ok {
		t.Error("client must not send verified")
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

func TestTopQueryAndDecode(t *testing.T) {
	client, f := testClient(t, func(*http.Request, string) (int, string) {
		return 200, `[{"id":"a","name":"DAVE","score":12500,"created_at":"2026-08-25T12:00:00Z"}]`
	})
	rows, err := client.Top(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	req := f.last(t)
	q := req.URL.Query()
	if q.Get("verified") != "eq.true" || q.Get("order") != "score.desc" || q.Get("limit") != "10" {
		t.Errorf("query = %v", q)
	}
	if len(rows) != 1 || rows[0].Name != "DAVE" || rows[0].Score != 12500 {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestPendingQuery(t *testing.T) {
	client, f := testClient(t, func(*http.Request, string) (int, string) {
		return 200, `[{"id":"a","name":"T","score":1,"verified":false}]`
	})
	if _, err := client.Pending(context.Background(), 50); err != nil {
		t.Fatal(err)
	}
	q := f.last(t).URL.Query()
	if q.Get("verified") != "eq.false" || q.Get("order") != "created_at.asc" {
		t.Errorf("query = %v", q)
	}
}

func TestSetVerifiedPatches(t *testing.T) {
	client, f := testClient(t, func(*http.Request, string) (int, string) { return 204, "" })
	if err := client.SetVerified(context.Background(), "abc", true); err != nil {
		t.Fatal(err)
	}
	req := f.last(t)
	if req.Method != http.MethodPatch || req.URL.Query().Get("id") != "eq.abc" {
		t.Fatalf("%s %v", req.Method, req.URL.Query())
	}
	if body := f.bodies[len(f.bodies)-1]; body != `{"verified":true}` {
		t.Errorf("patch body = %s", body)
	}
}

func TestDelete(t *testing.T) {
	client, f := testClient(t, func(*http.Request, string) (int, string) { return 204, "" })
	if err := client.Delete(context.Background(), "abc"); err != nil {
		t.Fatal(err)
	}
	req := f.last(t)
	if req.Method != http.MethodDelete || req.URL.Query().Get("id") != "eq.abc" {
		t.Fatalf("%s %v", req.Method, req.URL.Query())
	}
}

func TestFromEnv(t *testing.T) {
	t.Setenv("SUPABASE_URL", "")
	t.Setenv("SUPABASE_KEY", "")
	if _, err := FromEnv(); err == nil {
		t.Fatal("missing env must error")
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
