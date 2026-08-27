package persist

import "testing"

// Sessions are independent identities: fresh device ids per session, no
// bleed into each other or the process-wide cache (the SSH server hands
// one to every connection).
func TestSessionIdentityIsIndependent(t *testing.T) {
	// Whatever earlier tests in this package left in the process cache,
	// session activity must not change it.
	before := processPlayer

	a, b := BeginSession(), BeginSession()
	if a.Player().DeviceID == "" || b.Player().DeviceID == "" {
		t.Fatal("session without a device id")
	}
	if a.Player().DeviceID == b.Player().DeviceID {
		t.Fatal("two sessions share a device id")
	}

	a.SaveName("ALPHA")
	a.SaveBest(100)
	a.SaveBest(50) // lower scores never regress the best
	if pc := a.Player(); pc.Name != "ALPHA" || pc.Best != 100 {
		t.Fatalf("session state = %+v", pc)
	}
	if pc := b.Player(); pc.Name != "" || pc.Best != 0 {
		t.Fatalf("session b saw session a's state: %+v", pc)
	}

	if processPlayer != before {
		t.Fatalf("session writes leaked into the process cache: %+v", processPlayer)
	}
}
