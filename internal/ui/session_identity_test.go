package ui

import (
	"sync"
	"testing"
	"time"

	"github.com/Daviey/mario/board"
	"github.com/Daviey/mario/internal/persist"
	"github.com/Daviey/mario/render"
)

// A session-injected identity must drive submissions end-to-end: the
// entry carries the per-connection device id and the typed name is saved
// through the injected hook (multi-session hosts: one player identity
// per SSH connection).
func TestSessionIdentitySubmission(t *testing.T) {
	t.Setenv("SUPABASE_URL", "")
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	ident := persist.PlayerConfig{DeviceID: "11111111-2222-4333-8444-555555555555"}
	var mu sync.Mutex
	var savedName string
	var got board.Entry
	submitted := make(chan struct{})

	ui := NewUI(func(e board.Entry) error {
		mu.Lock()
		got = e
		mu.Unlock()
		close(submitted)
		return nil
	}, func() ([]board.Row, error) { return nil, nil })
	ui.SetIdentity(func() persist.PlayerConfig { return ident }, func(name string) {
		mu.Lock()
		savedName = name
		mu.Unlock()
	})
	ui.SetReplaySource(testReplaySource)

	g := gameOverGame(t)
	snap := tickUntil(t, ui, g, 2)
	if snap == nil || snap.Mode != render.UIAsk {
		t.Fatalf("expected ask prompt, got %+v", snap)
	}
	if ui.player.DeviceID != ident.DeviceID {
		t.Fatalf("game over should refresh the session identity, got %+v", ui.player)
	}

	ui.FeedKeys([]byte("y"))
	tickUntil(t, ui, g, 1)
	ui.FeedKeys([]byte("zed\r"))
	tickUntil(t, ui, g, 1)

	select {
	case <-submitted:
	case <-time.After(2 * time.Second):
		t.Fatal("submit never called")
	}
	mu.Lock()
	dev, name, saved := got.DeviceID, got.Name, savedName
	mu.Unlock()
	if dev != ident.DeviceID {
		t.Fatalf("entry device id = %q, want the session identity", dev)
	}
	if name != "ZED" {
		t.Fatalf("entry name = %q", name)
	}
	waitFor(t, 2*time.Second, func() bool { return saved == "ZED" })
}
