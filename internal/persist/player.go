package persist

// Player identity for the leaderboard: a stable device UUID (generated
// once, stored per platform) and a display name. No accounts anywhere.
//
// The store itself is build-tagged: natively a 0600 JSON file under the
// OS config dir (player_store_other.go); in the browser, localStorage
// (player_store_js.go) — js has no filesystem, and without a persistent
// device id every web submit would send device_id "" and be rejected by
// the server's uuid column.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strings"
	"unicode"
)

type PlayerConfig struct {
	DeviceID string `json:"device_id"`
	Name     string `json:"name"`
	Best     int    `json:"best,omitempty"` // best score ever, local only
}

// LoadPlayer loads the stored player identity, creating a fresh one on
// first run. Store failures are best-effort: a fresh in-memory identity
// is still returned so play (and offline bests) keep working.
func LoadPlayer() (PlayerConfig, error) {
	if data := loadPlayerBytes(); len(data) > 0 {
		var pc PlayerConfig
		if json.Unmarshal(data, &pc) == nil && pc.DeviceID != "" {
			return pc, nil
		}
	}
	pc := PlayerConfig{DeviceID: newDeviceID()}
	if data, err := json.MarshalIndent(pc, "", "  "); err == nil {
		storePlayerBytes(data)
	}
	return pc, nil
}

// SaveBest records the best score (no-op for scores that do not beat it).
func SaveBest(score int) {
	pc, err := LoadPlayer()
	if err != nil || score <= pc.Best {
		return
	}
	name := pc.Name
	pc.Best = score
	pc.SaveName(name)
}

func (pc *PlayerConfig) SaveName(name string) {
	pc.Name = name
	if data, err := json.MarshalIndent(pc, "", "  "); err == nil {
		storePlayerBytes(data)
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
func SanitizeName(s string) (string, bool) {
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
