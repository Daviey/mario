package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"mario/board"
)

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
	if err := maybeSubmit(&out, in, 12500); err != nil {
		t.Fatal(err)
	}
	e := <-s.got
	if e.Name != "DAVE" || e.Score != 12500 || e.DeviceID == "" {
		t.Fatalf("entry = %+v", e)
	}
	if !strings.Contains(out.String(), "submitted") {
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
	if err := maybeSubmit(&out, in, 10); err != nil {
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
	if err := maybeSubmit(&out, in, 10); err != nil {
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

func TestMaybeSubmitSkipsZeroScore(t *testing.T) {
	t.Setenv("SUPABASE_URL", "http://unused")
	t.Setenv("SUPABASE_KEY", "k")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var out bytes.Buffer
	if err := maybeSubmit(&out, strings.NewReader("y\nX\n"), 0); err != nil {
		t.Fatal(err)
	}
	if out.Len() != 0 {
		t.Errorf("zero score must not prompt, out = %q", out.String())
	}
}

func TestMaybeSubmitRetriesBadName(t *testing.T) {
	s := newSubmitServer(t)
	t.Setenv("SUPABASE_URL", s.srv.URL)
	t.Setenv("SUPABASE_KEY", "k")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	var out bytes.Buffer
	in := strings.NewReader("\ntoolongname9\nDAVE\n")
	if err := maybeSubmit(&out, in, 7); err != nil {
		t.Fatal(err)
	}
	if e := <-s.got; e.Name != "DAVE" {
		t.Fatalf("name = %q", e.Name)
	}
	if !strings.Contains(out.String(), "1-8 chars") {
		t.Errorf("missing validation hint: %q", out.String())
	}
}
