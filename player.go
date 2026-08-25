package main

// Player identity for the leaderboard: a stable device UUID (generated once,
// stored in the OS config dir) and a display name. No accounts anywhere.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"unicode"
)

type playerConfig struct {
	DeviceID string `json:"device_id"`
	Name     string `json:"name"`
}

// playerConfigPath returns <UserConfigDir>/mario/player.json, creating the
// directory if needed.
func playerConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	p := filepath.Join(dir, "mario")
	if err := os.MkdirAll(p, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(p, "player.json"), nil
}

// loadPlayer loads the stored player identity, creating a fresh one on first
// run.
func loadPlayer() (playerConfig, error) {
	path, err := playerConfigPath()
	if err != nil {
		return playerConfig{}, err
	}
	if data, err := os.ReadFile(path); err == nil {
		var pc playerConfig
		if json.Unmarshal(data, &pc) == nil && pc.DeviceID != "" {
			return pc, nil
		}
	}
	pc := playerConfig{DeviceID: newDeviceID()}
	if data, err := json.MarshalIndent(pc, "", "  "); err == nil {
		os.WriteFile(path, data, 0o600)
		os.Chmod(path, 0o600) // Ensure tight permissions if file existed
	}
	return pc, nil
}

func (pc *playerConfig) saveName(name string) {
	pc.Name = name
	if path, err := playerConfigPath(); err == nil {
		if data, err := json.MarshalIndent(pc, "", "  "); err == nil {
			os.WriteFile(path, data, 0o600)
			os.Chmod(path, 0o600)
		}
	}
}

// newDeviceID returns a random RFC 4122 v4 UUID string.
func newDeviceID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failure is effectively fatal for uniqueness; fall
		// back to a time-derived id rather than a fixed one.
		return "00000000-0000-4000-8000-000000000000"
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	h := hex.EncodeToString(b[:])
	return h[0:8] + "-" + h[8:12] + "-" + h[12:16] + "-" + h[16:20] + "-" + h[20:32]
}

// sanitizeName trims and validates a leaderboard display name against the
// database CHECK constraints (1-8 chars) plus a conservative charset.
func sanitizeName(s string) (string, bool) {
	s = strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return ' '
		}
		if r >= 'a' && r <= 'z' {
			return r - 'a' + 'A'
		}
		return r
	}, strings.TrimSpace(s))
	s = strings.TrimSpace(s)
	if len(s) < 1 || len(s) > 8 {
		return "", false
	}
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == ' ' || r == '-' || r == '.':
		default:
			return "", false
		}
	}
	return s, true
}
