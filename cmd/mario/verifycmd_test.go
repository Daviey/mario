package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Daviey/mario/board"
	"github.com/Daviey/mario/engine"
	"github.com/Daviey/mario/replay"
)

// TestVerifyPendingKeepAndDrop runs the real verifier loop against a fake
// PostgREST serving two pending rows: one backed by a genuine replay of a
// scripted run (must be kept and marked verified), one lying about its
// score (must be dropped).
func TestVerifyPendingKeepAndDrop(t *testing.T) {
	// Record a genuine run.
	levels := engine.DefaultLevels()
	g := engine.NewGame(levels, 20, engine.LevelHeight)
	var rec replay.Recorder
	for i := 0; g.State != engine.StateGameOver && i < 6000; i++ {
		in := engine.Input{Right: true, Run: i%3 != 0, Up: i%97 < 22, AnyKey: i == 0}
		g.Update(in)
		if i == 1 {
			rec.Start()
		}
		if i >= 1 {
			rec.Record(in)
		}
	}
	rec.Finish()
	if res, err := replay.Run(levels, "classic", rec.JSON()); err != nil || res.Score != g.Score {
		t.Fatalf("fixture replay broken: %v %+v vs %d", err, res, g.Score)
	}

	honest := `{"id":"keep-1","name":"HONEST","score":` + itoa(g.Score) + `,"level":` + itoa(g.LevelIndex()+1) +
		`,"mode":"classic","engine_version":"` + board.EngineVersion + `","replay":` + jq(rec.JSON()) + `}`
	liar := `{"id":"drop-1","name":"LIAR","score":999999,"level":9,` +
		`"mode":"classic","engine_version":"` + board.EngineVersion + `","replay":` + jq(rec.JSON()) + `}`

	var patched, deleted, fetched atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if fetched.Add(1) == 1 {
				w.Write([]byte("[" + honest + "," + liar + "]"))
			} else {
				w.Write([]byte("[]"))
			}
		case http.MethodPatch:
			patched.Add(1)
			w.WriteHeader(204)
		case http.MethodDelete:
			deleted.Add(1)
			w.WriteHeader(204)
		}
	}))
	defer srv.Close()

	t.Setenv("SUPABASE_URL", srv.URL)
	t.Setenv("SUPABASE_SERVICE_KEY", "test-service-key")
	if err := runVerifyPending(); err != nil {
		t.Fatal(err)
	}
	if got := patched.Load(); got != 1 {
		t.Errorf("kept rows = %d, want 1", got)
	}
	if got := deleted.Load(); got != 1 {
		t.Errorf("dropped rows = %d, want 1", got)
	}
}

// Without a service key the verifier falls back to the direct database
// connection; with neither credential available it must refuse to run.
func TestVerifyPendingNeedsCredentials(t *testing.T) {
	t.Setenv("SUPABASE_URL", "https://example.invalid")
	t.Setenv("SUPABASE_SERVICE_KEY", "")
	t.Setenv("SUPABASE_DB_PASSWORD", "")
	err := runVerifyPending()
	if err == nil {
		t.Fatal("verifier must refuse to run without service key or db password")
	}
	if want := "SUPABASE_DB_PASSWORD"; !strings.Contains(err.Error(), want) {
		t.Errorf("error should mention the db fallback credential: %v", err)
	}
}

// With only the database password available the direct-Postgres backend
// is selected (and fails on an unreachable host — the selection, not the
// credential check, is what is under test).
func TestVerifyPendingFallsBackToDirectDB(t *testing.T) {
	t.Setenv("SUPABASE_URL", "https://example.invalid")
	t.Setenv("SUPABASE_SERVICE_KEY", "")
	t.Setenv("SUPABASE_DB_PASSWORD", "nope")
	t.Setenv("SUPABASE_DB_HOST", "127.0.0.1")
	t.Setenv("SUPABASE_DB_PORT", "1") // nothing listens here
	err := runVerifyPending()
	if err == nil {
		t.Fatal("direct db backend on a closed port must fail")
	}
	if want := "db connect"; !strings.Contains(err.Error(), want) {
		t.Errorf("error should come from the db dial, got: %v", err)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// jq embeds the replay string as a JSON string (PostgREST serves the text
// column quoted, exactly like this).
func jq(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// TestVerifyPendingSystematicDropFails verifies the alerting contract:
// ONE row whose replay disagrees with its claim can be a corrupted
// submission, but TWO is a systematic determinism or recording bug
// deleting real scores (the 2026-08-30 recorder-wipe bug dropped every
// death-containing run silently, for weeks) — the run must fail red.
// Version-stale rows (pending recordings from before an EngineVersion
// bump) are expected and must never trigger the failure.
func TestVerifyPendingSystematicDropFails(t *testing.T) {
	levels := engine.DefaultLevels()
	g := engine.NewGame(levels, 20, engine.LevelHeight)
	var rec replay.Recorder
	for i := 0; g.State != engine.StateGameOver && i < 6000; i++ {
		in := engine.Input{Right: true, Run: i%3 != 0, Up: i%97 < 22, AnyKey: i == 0}
		g.Update(in)
		if i == 1 {
			rec.Start()
		}
		if i >= 1 {
			rec.Record(in)
		}
	}
	rec.Finish()

	row := func(id, name, score, level, ver string) string {
		return `{"id":"` + id + `","name":"` + name + `","score":` + score +
			`,"level":` + level + `,"mode":"classic","engine_version":"` + ver +
			`","replay":` + jq(rec.JSON()) + `}`
	}
	honest := row("keep-1", "HONEST", itoa(g.Score), itoa(g.LevelIndex()+1), board.EngineVersion)
	liar1 := row("drop-1", "LIAR1", "999999", "9", board.EngineVersion)
	liar2 := row("drop-2", "LIAR2", "888888", "8", board.EngineVersion)
	stale := row("drop-3", "STALE", itoa(g.Score), itoa(g.LevelIndex()+1), "0.0.0-old")

	run := func(first string) error {
		var fetched atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				if fetched.Add(1) == 1 {
					w.Write([]byte(first))
				} else {
					w.Write([]byte("[]"))
				}
			case http.MethodPatch, http.MethodDelete:
				w.WriteHeader(204)
			}
		}))
		defer srv.Close()
		t.Setenv("SUPABASE_URL", srv.URL)
		t.Setenv("SUPABASE_SERVICE_KEY", "test-service-key")
		return runVerifyPending()
	}

	// Two determinism failures: the run must fail.
	if err := run("[" + honest + "," + liar1 + "," + liar2 + "]"); err == nil {
		t.Error("two replay-mismatch drops must fail the verification run")
	}
	// One determinism failure plus a version-stale row: expected noise,
	// the run stays green.
	if err := run("[" + honest + "," + liar1 + "," + stale + "]"); err != nil {
		t.Errorf("one determinism drop + one version drop must not fail: %v", err)
	}
}
