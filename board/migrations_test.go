package board

import (
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestMigrationPrefixesUnique guards the migrations README's ordering rule:
// fresh applies run in lexical filename order, so a reused timestamp prefix
// transposes history. One historical pair predates the rule and is papered
// over by the union migration — it stays listed here so any NEW collision
// fails loudly instead of silently reordering.
func TestMigrationPrefixesUnique(t *testing.T) {
	knownDupes := map[string]bool{
		"20260826000000": true, // daily_mode + level, merged in ...000001_board_rows_union
	}
	files, err := filepath.Glob(filepath.Join("..", "supabase", "migrations", "*.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("glob migrations: %v (%d files)", err, len(files))
	}
	sort.Strings(files)
	seen := map[string]string{}
	for _, f := range files {
		base := filepath.Base(f)
		prefix := strings.Split(base, "_")[0]
		if len(prefix) != 14 || !isDigits(prefix) {
			t.Errorf("%s: migration name must start with a 14-digit timestamp prefix", base)
			continue
		}
		if prev, dup := seen[prefix]; dup {
			if !knownDupes[prefix] {
				t.Errorf("timestamp prefix %s reused by %s and %s — fresh applies run lexically, pick a new prefix", prefix, prev, base)
			}
			continue
		}
		seen[prefix] = base
	}
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
