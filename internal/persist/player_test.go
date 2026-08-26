package persist

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestSanitizeName(t *testing.T) {
	ok := []struct{ in, want string }{
		{"DAVE", "DAVE"},
		{"  dave  ", "DAVE"},
		{"a b\tc", "A B C"}, // any whitespace collapses to plain spaces
		{"8chars--", "8CHARS--"},
		{"a.b c-d", "A.B C-D"},
		{"12345678", "12345678"},
	}
	for _, c := range ok {
		if got, ok := SanitizeName(c.in); !ok || got != c.want {
			t.Errorf("SanitizeName(%q) = %q,%v want %q,true", c.in, got, ok, c.want)
		}
	}
	// '_' has no pixel-font glyph: rejected like any other unsupported rune.
	bad := []string{"", " ", "123456789", "héllo", "x@y", "n:o", "semi;colon", "under_score"}
	for _, in := range bad {
		if _, ok := SanitizeName(in); ok {
			t.Errorf("SanitizeName(%q) should fail", in)
		}
	}
}

func TestNewDeviceID(t *testing.T) {
	seen := map[string]bool{}
	uuidRe := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	for range 100 {
		id := newDeviceID()
		if !uuidRe.MatchString(id) {
			t.Fatalf("id %q is not a v4 UUID", id)
		}
		if seen[id] {
			t.Fatalf("duplicate id %q", id)
		}
		seen[id] = true
	}
}

func TestLoadPlayerStableInProcessAndWritesNothing(t *testing.T) {
	// Even if a config dir exists (and even if a stale player.json sits
	// in it from an older build), native loads must not read or write it.
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	stale := filepath.Join(dir, "github.com/Daviey/mario")
	os.MkdirAll(stale, 0o755)
	os.WriteFile(filepath.Join(stale, "player.json"), []byte(`{"device_id":"00000000-0000-4000-8000-000000000000"}`), 0o644)

	pc1, err := LoadPlayer()
	if err != nil {
		t.Fatal(err)
	}
	if pc1.DeviceID == "" || pc1.DeviceID == "00000000-0000-4000-8000-000000000000" {
		t.Fatalf("fresh device id expected, got %q", pc1.DeviceID)
	}

	// Every load within the process agrees on one identity.
	pc2, _ := LoadPlayer()
	if pc1.DeviceID != pc2.DeviceID {
		t.Fatalf("device id not stable in process: %q vs %q", pc1.DeviceID, pc2.DeviceID)
	}

	// Name writes go through the cache, not the disk.
	pc2.SaveName("DAVE")
	pc3, _ := LoadPlayer()
	if pc3.Name != "DAVE" {
		t.Fatalf("name not kept in process: %+v", pc3)
	}

	// Session bests update the cache too.
	SaveBest(500)
	if pc4, _ := LoadPlayer(); pc4.Best != 500 {
		t.Fatalf("best not kept in process: %+v", pc4)
	}

	// And nothing was written anywhere under the config dir.
	entries, err := os.ReadDir(stale)
	if err != nil || len(entries) != 1 || entries[0].Name() != "player.json" {
		t.Fatalf("native build wrote to the config dir: %v (%v)", entries, err)
	}
	info, err := os.Stat(filepath.Join(stale, "player.json"))
	if err != nil || info.Size() != int64(len(`{"device_id":"00000000-0000-4000-8000-000000000000"}`)) {
		t.Fatalf("player.json was rewritten: %v", info)
	}
}
