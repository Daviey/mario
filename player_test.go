package main

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

func TestSanitizeName(t *testing.T) {
	ok := []struct{ in, want string }{
		{"DAVE", "DAVE"},
		{"  dave  ", "dave"},
		{"a b\tc", "a b c"}, // any whitespace collapses to plain spaces
		{"8chars--", "8chars--"},
		{"a.b_c-d", "a.b_c-d"},
		{"12345678", "12345678"},
		{"__", "__"}, // underscores are allowed
	}
	for _, c := range ok {
		if got, ok := sanitizeName(c.in); !ok || got != c.want {
			t.Errorf("sanitizeName(%q) = %q,%v want %q,true", c.in, got, ok, c.want)
		}
	}
	bad := []string{"", " ", "123456789", "héllo", "x@y", "n:o", "semi;colon"}
	for _, in := range bad {
		if _, ok := sanitizeName(in); ok {
			t.Errorf("sanitizeName(%q) should fail", in)
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

func TestLoadPlayerCreatesAndPersists(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	pc1, err := loadPlayer()
	if err != nil {
		t.Fatal(err)
	}
	if pc1.DeviceID == "" {
		t.Fatal("device id must be generated on first run")
	}

	// Second load must return the same identity from disk.
	pc2, err := loadPlayer()
	if err != nil {
		t.Fatal(err)
	}
	if pc1.DeviceID != pc2.DeviceID {
		t.Fatalf("device id not persisted: %q vs %q", pc1.DeviceID, pc2.DeviceID)
	}

	pc2.saveName("DAVE")
	pc3, _ := loadPlayer()
	if pc3.Name != "DAVE" {
		t.Fatalf("name not persisted: %+v", pc3)
	}

	// Corrupt config regenerates rather than failing.
	path := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "mario", "player.json")
	os.WriteFile(path, []byte(`{"device_id":`), 0o644)
	pc4, err := loadPlayer()
	if err != nil || pc4.DeviceID == "" {
		t.Fatalf("corrupt config must regenerate: %+v, %v", pc4, err)
	}
}
