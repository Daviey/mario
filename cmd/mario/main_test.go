package main

import (
	"os"
	"path/filepath"
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
