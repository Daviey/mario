package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mario/board"
	"mario/engine"
)

// runResultAt builds a plausible finished run with the given claimed score.
func runResultAt(score int) *runResult {
	r := newRecorder()
	for t := range 120 {
		r.record(scriptInput(t))
	}
	g := replayGame(r.rec)
	return &runResult{rec: r, score: score, coins: g.CoinCount, state: g.State}
}

func replayGame(rec recording) *engine.Game {
	g := engine.NewGame(engine.DefaultLevels(), 20, engine.LevelHeight)
	for _, v := range rec.I {
		g.Update(decodeInput(v))
	}
	return g
}

// submitServer captures the entry a client submits.
type submitServer struct {
	got chan board.Entry
	srv *httptest.Server
}

func newSubmitServer(t *testing.T) *submitServer {
	t.Helper()
	s := &submitServer{got: make(chan board.Entry, 1)}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var e board.Entry
		json.NewDecoder(r.Body).Decode(&e)
		s.got <- e
		w.WriteHeader(201)
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func TestMaybeSubmitYes(t *testing.T) {
	s := newSubmitServer(t)
	t.Setenv("SUPABASE_URL", s.srv.URL)
	t.Setenv("SUPABASE_KEY", "k")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir()) // fresh player identity

	var out bytes.Buffer
	in := strings.NewReader("y\nDAVE\n")
	if err := maybeSubmit(&out, in, runResultAt(12500), true); err != nil {
		t.Fatal(err)
	}
	e := <-s.got
	if e.Name != "DAVE" || e.Score != 12500 || e.EngineVersion != scoreEngineVersion {
		t.Fatalf("entry = %+v", e)
	}
	var rec recording
	if err := json.Unmarshal(e.Replay, &rec); err != nil || rec.V != 1 || len(rec.I) != 120 {
		t.Fatalf("replay payload = %s (%v)", e.Replay, err)
	}
	if !strings.Contains(out.String(), "pending verification") {
		t.Errorf("out = %q", out.String())
	}

	// Name must have been persisted for the next run.
	pc, err := loadPlayer()
	if err != nil || pc.Name != "DAVE" {
		t.Fatalf("saved player = %+v, %v", pc, err)
	}
}

func TestMaybeSubmitKeepsStoredNameOnEnter(t *testing.T) {
	s := newSubmitServer(t)
	t.Setenv("SUPABASE_URL", s.srv.URL)
	t.Setenv("SUPABASE_KEY", "k")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	pc, _ := loadPlayer()
	pc.saveName("BIFF")

	var out bytes.Buffer
	in := strings.NewReader("\n\n") // accept default, blank name keeps stored
	if err := maybeSubmit(&out, in, runResultAt(10), true); err != nil {
		t.Fatal(err)
	}
	if e := <-s.got; e.Name != "BIFF" {
		t.Fatalf("name = %q, want stored BIFF", e.Name)
	}
}

func TestMaybeSubmitDecline(t *testing.T) {
	s := newSubmitServer(t)
	t.Setenv("SUPABASE_URL", s.srv.URL)
	t.Setenv("SUPABASE_KEY", "k")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var out bytes.Buffer
	in := strings.NewReader("n\n")
	if err := maybeSubmit(&out, in, runResultAt(10), true); err != nil {
		t.Fatal(err)
	}
	select {
	case e := <-s.got:
		t.Fatalf("declined run must not submit, got %+v", e)
	default:
	}
	if !strings.Contains(out.String(), "not submitted") {
		t.Errorf("out = %q", out.String())
	}
}

func TestMaybeSubmitSkipsWhenUnworthy(t *testing.T) {
	t.Setenv("SUPABASE_URL", "http://unused")
	t.Setenv("SUPABASE_KEY", "k")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	lost := newRecorder()
	for range maxRecordTicks + 1 {
		lost.record(engine.Input{})
	}
	cases := map[string]*runResult{
		"zero score":  {rec: newRecorder(), score: 0},
		"nil result":  nil,
		"lost record": {rec: lost, score: 5},
	}
	for name, res := range cases {
		var out bytes.Buffer
		if err := maybeSubmit(&out, strings.NewReader("y\nX\n"), res, true); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if out.Len() != 0 {
			t.Errorf("%s: must not prompt, out = %q", name, out.String())
		}
	}

	// Custom level runs are unverifiable: no prompt either.
	var out bytes.Buffer
	if err := maybeSubmit(&out, strings.NewReader("y\nX\n"), runResultAt(9), false); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Errorf("custom level: must not prompt, out = %q", out.String())
	}
}

func TestMaybeSubmitRetriesBadName(t *testing.T) {
	s := newSubmitServer(t)
	t.Setenv("SUPABASE_URL", s.srv.URL)
	t.Setenv("SUPABASE_KEY", "k")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var out bytes.Buffer
	in := strings.NewReader("\ntoolongname9\nDAVE\n")
	if err := maybeSubmit(&out, in, runResultAt(7), true); err != nil {
		t.Fatal(err)
	}
	if e := <-s.got; e.Name != "DAVE" {
		t.Fatalf("name = %q", e.Name)
	}
	if !strings.Contains(out.String(), "1-8 chars") {
		t.Errorf("missing validation hint: %q", out.String())
	}
}
