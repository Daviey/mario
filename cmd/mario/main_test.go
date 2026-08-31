package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// resolveMoshBin must cover every -mosh shape the flag documents: off,
// an explicit path, "auto" resolving through PATH, and "auto" with no
// mosh-server present (disabled, with the operator note main prints).
func TestResolveMoshBin(t *testing.T) {
	// A stub mosh-server on PATH for the found case; only the path is
	// resolved, never executed.
	dir := t.TempDir()
	fake := filepath.Join(dir, "mosh-server")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name     string
		flag     string
		onPATH   bool // stub mosh-server visible?
		wantBin  string
		wantNote string
	}{
		{"empty flag is off", "", false, "", ""},
		{"explicit path passes through", "/opt/mosh/bin/mosh-server", false, "/opt/mosh/bin/mosh-server", ""},
		{"auto resolves via PATH", "auto", true, fake, ""},
		{"auto missing disables with note", "auto", false, "", "-mosh auto: mosh-server not found in PATH; mosh disabled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.onPATH {
				t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			} else {
				t.Setenv("PATH", t.TempDir())
			}
			bin, note := resolveMoshBin(tc.flag)
			if bin != tc.wantBin {
				t.Errorf("bin = %q, want %q", bin, tc.wantBin)
			}
			if note != tc.wantNote {
				t.Errorf("note = %q, want %q", note, tc.wantNote)
			}
		})
	}
}

// serveIgnored must flag exactly the play-only flags -serve never
// consumes — and not the ones it does (-basic, -nobell, -level) nor
// -width's 0 default.
func TestServeIgnored(t *testing.T) {
	for _, tc := range []struct {
		name         string
		daily        bool
		width        int
		demo, cheats bool
		want         []string
	}{
		{"clean serve", false, 0, false, false, nil},
		{"default width is fine", false, 0, false, false, nil},
		{"explicit width is ignored by serve", false, 30, false, false, []string{"-width"}},
		{"daily belongs to play and -scores", true, 0, false, false, []string{"-daily"}},
		{"demo is shadowed by serve", false, 0, true, false, []string{"-demo"}},
		{"cheats never reach sessions", false, 0, false, true, []string{"-cheats"}},
		{"all together", true, 40, true, true, []string{"-daily", "-width", "-demo", "-cheats"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := serveIgnored(tc.daily, tc.width, tc.demo, tc.cheats)
			if !slices.Equal(got, tc.want) {
				t.Errorf("serveIgnored = %v, want %v", got, tc.want)
			}
		})
	}
}

// usage must document the dispatch precedence main actually implements;
// the chain reads the same order the flag checks run in main().
func TestUsageDocumentsModePrecedence(t *testing.T) {
	var b bytes.Buffer
	usage(&b)
	out := b.String()
	for _, chain := range []string{
		"-version > -verify-pending > -dump-replays > -replay > -scores",
		"> -ui-preview > -serve > -demo > interactive play",
	} {
		if !strings.Contains(out, chain) {
			t.Errorf("usage missing precedence chain %q", chain)
		}
	}
}
