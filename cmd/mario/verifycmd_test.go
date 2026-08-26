package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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

func TestVerifyPendingNeedsServiceKey(t *testing.T) {
	t.Setenv("SUPABASE_URL", "https://example.invalid")
	t.Setenv("SUPABASE_SERVICE_KEY", "")
	if err := runVerifyPending(); err == nil {
		t.Fatal("verifier must refuse to run without the service key")
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
