package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Daviey/mario/board"
	"github.com/Daviey/mario/engine"
	"github.com/Daviey/mario/replay"
)

// TestVerifyPendingKeepFixAndHide runs the real verifier loop against a
// fake PostgREST serving three pending rows backed by the same genuine
// replay of a scripted run: an honest row (kept + verified), a row
// overstating its score (corrected to the replayed score — the replay is
// authoritative, never delete), and a cross-version row (hidden, never
// deleted — the row and its recording stay for forensics).
func TestVerifyPendingKeepFixAndHide(t *testing.T) {
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
	liar := `{"id":"fix-1","name":"LIAR","score":999999,"level":` + itoa(g.LevelIndex()+1) + `,` +
		`"mode":"classic","engine_version":"` + board.EngineVersion + `","replay":` + jq(rec.JSON()) + `}`
	stale := `{"id":"hide-1","name":"STALE","score":` + itoa(g.Score) + `,"level":` + itoa(g.LevelIndex()+1) +
		`,"mode":"classic","engine_version":"0.0.0-old","replay":` + jq(rec.JSON()) + `}`

	var fetched atomic.Int32
	var patched, deleted atomic.Int32
	var fixedScore atomic.Int32
	var fixedBody atomic.Bool
	var hidBody atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			if fetched.Add(1) == 1 {
				w.Write([]byte("[" + honest + "," + liar + "," + stale + "]"))
			} else {
				w.Write([]byte("[]"))
			}
		case http.MethodPatch:
			patched.Add(1)
			body, _ := io.ReadAll(r.Body)
			if strings.Contains(string(body), `"score":`) {
				fixedScore.Add(1)
				fixedBody.Store(strings.Contains(string(body), `"score":`+itoa(g.Score)))
			}
			if strings.Contains(string(body), `"hidden":true`) {
				hidBody.Store(true)
			}
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
	if got := deleted.Load(); got != 0 {
		t.Errorf("deleted rows = %d, want 0 (mismatched rows are corrected or hidden, never deleted)", got)
	}
	if got := fixedScore.Load(); got != 1 {
		t.Errorf("corrected rows = %d, want 1", got)
	}
	if !fixedBody.Load() {
		t.Error("score correction PATCH did not carry the replayed score")
	}
	if !hidBody.Load() {
		t.Error("stale row was not hidden")
	}
	if got := patched.Load(); got != 4 {
		t.Errorf("total PATCHes = %d, want 4 (verify, score-fix, verify, hide)", got)
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

// jq embeds the replay string as a JSON string (PostgREST serves the text
// column quoted, exactly like this).
func jq(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
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

// A plain score disagreement is NOT a failure any more — the replay is
// authoritative, so those rows are corrected in place. What stays an
// alert is Hiding: ONE row whose replay cannot settle it can be a
// corrupted submission, but TWO unreplayable rows is a systematic
// recording bug hiding real scores (the 2026-08-30 recorder-wipe bug
// destroyed every death-containing run silently, for weeks) — the run
// must fail red. Version-stale rows (pending recordings from before an
// EngineVersion bump) are expected and must never trigger the failure.
func TestVerifyPendingSystematicHideFails(t *testing.T) {
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
	// Level lies cannot be corrected (level feeds the L<n> column), so
	// they hide — and two of them mean a systematic bug.
	liar1 := row("hide-1", "LIAR1", itoa(g.Score), "9", board.EngineVersion)
	liar2 := row("hide-2", "LIAR2", itoa(g.Score), "8", board.EngineVersion)
	stale := row("hide-3", "STALE", itoa(g.Score), itoa(g.LevelIndex()+1), "0.0.0-old")

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

	// Two unreplayable hides: the run must fail.
	if err := run("[" + honest + "," + liar1 + "," + liar2 + "]"); err == nil {
		t.Error("two hide-class failures must fail the verification run")
	}
	// One hide plus a version-stale row: expected noise, green run.
	if err := run("[" + honest + "," + liar1 + "," + stale + "]"); err != nil {
		t.Errorf("one determinism hide + one version hide must not fail: %v", err)
	}
	// Plain score disagreements are corrections, never failures — even
	// in bulk (this was the DROP-class red run before the policy change).
	liar3 := row("fix-1", "LIAR3", "999999", itoa(g.LevelIndex()+1), board.EngineVersion)
	liar4 := row("fix-2", "LIAR4", "999998", itoa(g.LevelIndex()+1), board.EngineVersion)
	if err := run("[" + honest + "," + liar3 + "," + liar4 + "]"); err != nil {
		t.Errorf("score corrections must stay green: %v", err)
	}
}
