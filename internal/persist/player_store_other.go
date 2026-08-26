//go:build !js

package persist

import (
	"os"
	"path/filepath"
)

// PlayerConfigPath returns <UserConfigDir>/mario/player.json, creating the
// directory if needed. Native only — the browser build keeps the identity
// in localStorage (player_store_js.go).

func PlayerConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, "github.com/Daviey/mario")
	if err := os.MkdirAll(p, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(p, "player.json"), nil
}

func loadPlayerBytes() []byte {
	path, err := PlayerConfigPath()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return data
}

func storePlayerBytes(data []byte) {
	path, err := PlayerConfigPath()
	if err != nil {
		return
	}
	writeAtomic(path, data)
}
