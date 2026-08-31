package board

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// scoresInsertGrantRe matches one column-scoped insert grant on scores.
// The history is append-only and every re-issue revokes first, so the
// LAST match in filename order is the grant the live database runs.
var scoresInsertGrantRe = regexp.MustCompile(`(?is)grant insert \(([^)]+)\)\s+on public\.scores\s+to anon`)

// latestScoresInsertGrant walks the migration history in order and
// returns the column list of the final scores insert grant — the
// effective one once every migration has been applied.
func latestScoresInsertGrant(dir string) ([]string, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.sql"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	var cols []string
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		for _, m := range scoresInsertGrantRe.FindAllStringSubmatch(string(b), -1) {
			cols = cols[:0]
			for _, c := range strings.Split(m[1], ",") {
				if c = strings.TrimSpace(c); c != "" {
					cols = append(cols, c)
				}
			}
		}
	}
	if len(cols) == 0 {
		return nil, errors.New("no insert grant on public.scores found in migration history")
	}
	return cols, nil
}

// TestEntryColumnsGranted: the anon INSERT grant on scores is
// column-scoped, so a new Entry field without a matching grant column
// only fails as a live 400 from PostgREST — typically after release.
// This test parses the effective grant out of the migration history and
// asserts every Entry json tag is a granted column, so the mistake fails
// offline, at the moment the field is added.
func TestEntryColumnsGranted(t *testing.T) {
	cols, err := latestScoresInsertGrant("../supabase/migrations")
	if err != nil {
		t.Fatal(err)
	}
	granted := make(map[string]bool, len(cols))
	for _, c := range cols {
		granted[c] = true
	}
	// A degenerate parse (zero columns matched) must not silently pass.
	if len(granted) < 10 {
		t.Fatalf("parsed only %d granted columns %v; grant regex drifted from the SQL shape", len(granted), cols)
	}
	typ := reflect.TypeOf(Entry{})
	for i := range typ.NumField() {
		tag := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		if tag == "" || tag == "-" {
			continue
		}
		if !granted[tag] {
			t.Errorf("Entry.%s ships json %q but the anon insert grant has no %s column — extend the grant in the next migration or the insert 400s live",
				typ.Field(i).Name, tag, tag)
		}
	}
}
