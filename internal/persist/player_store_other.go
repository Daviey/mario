//go:build !js

package persist

// Native builds store NOTHING on the user's machine — no config files,
// ever. The identity is process-lifetime only (see player.go); these
// no-op store hooks exist so the browser build can keep localStorage,
// which is the one storage medium the project allows itself.
func loadPlayerBytes() []byte { return nil }

func storePlayerBytes([]byte) {}
