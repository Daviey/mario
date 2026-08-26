package render

import (
	"testing"

	"github.com/Daviey/mario/engine"
)

func TestSanitizeArcade(t *testing.T) {
	cases := []struct{ in, want string }{
		{"v0.1.0-38-gf5c876b", "V0.1.0-38-GF5C876B"},
		{"v1.2.3-dirty", "V1.2.3-DIRTY"},
		{"DEV", "DEV"},
		{"v2_beta", "V2BETA"},     // underscore has no glyph: dropped
		{"v1 ~beta!", "V1 BETA!"}, // space kept, tilde dropped
		{"", ""},
	}
	for _, c := range cases {
		if got := sanitizeArcade(c.in); got != c.want {
			t.Errorf("sanitizeArcade(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestVersionCandidates(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		// Exact tagged build: nothing to trim.
		{"v1.2.3", []string{"V1.2.3"}},
		// Commits past the tag: full, hash dropped, bare tag.
		{"v0.1.0-38-gf5c876b", []string{"V0.1.0-38-GF5C876B", "V0.1.0-38", "V0.1.0"}},
		// Dirty tree keeps DIRTY through the ladder.
		{"v0.1.0-38-gf5c876b-dirty", []string{"V0.1.0-38-GF5C876B-DIRTY", "V0.1.0-38-DIRTY", "V0.1.0"}},
		// No tags yet: the short hash is the identity and stays whole.
		{"f5c876b", []string{"F5C876B"}},
		{"f5c876b-dirty", []string{"F5C876B-DIRTY", "F5C876B"}},
		// Unset or unsanitizable versions render nothing.
		{"", nil},
		{"___", nil},
	}
	for _, c := range cases {
		got := versionCandidates(c.in)
		if len(got) != len(c.want) {
			t.Fatalf("versionCandidates(%q) = %q, want %q", c.in, got, c.want)
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Fatalf("versionCandidates(%q) = %q, want %q", c.in, got, c.want)
			}
		}
	}
}

// TestVersionDrawnOnTitle proves the build identity reaches actual pixels
// on a tall-enough title screen and registers its cloud keep-clear band;
// with the ladder trimmed it also proves narrow frames degrade gracefully.
func TestVersionDrawnOnTitle(t *testing.T) {
	old := Version
	t.Cleanup(func() { Version = old })

	g := newGame(t)
	g.State = engine.StateTitle
	g.ViewH = engine.LevelHeight // tall frame: room for the version line

	Version = ""
	f0 := worldFrame(g, testPal)
	nBands0 := len(titleTextBands(f0, g))

	Version = "V9.9.9-TEST"
	f1 := worldFrame(g, testPal)

	found := false
	for _, e := range titleTextEls(f1, g) {
		if e.s == "V9.9.9-TEST" {
			found = true
		}
	}
	if !found {
		t.Fatalf("version element missing from title els")
	}

	diff := 0
	for y := range min(f0.H, f1.H) {
		for x := range min(f0.W, f1.W) {
			if f0.At(x, y) != f1.At(x, y) {
				diff++
			}
		}
	}
	if diff == 0 {
		t.Fatal("set Version drew no pixels on the title frame")
	}

	if nBands := len(titleTextBands(f1, g)); nBands != nBands0+1 {
		t.Fatalf("title bands with version = %d, want %d (+1 keep-clear rect)", nBands, nBands0+1)
	}

	// Narrow frame: the full string cannot fit, so pickTextPx must have
	// laddered to a shorter candidate rather than overflowing.
	g.ViewW = 12 // 72px wide frame
	f2 := worldFrame(g, testPal)
	for _, e := range titleTextEls(f2, g) {
		if w := textWidthPx(e.s, e.scale); e.s != "MARIO" && w > f2.W {
			t.Fatalf("element %q overflows %dpx frame (%dpx)", e.s, f2.W, w)
		}
	}
}
