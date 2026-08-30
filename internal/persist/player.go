package persist

// Player identity for the leaderboard: a device UUID and a display name.
// No accounts anywhere, and NOTHING is written to the user's machine by
// native builds — the identity is process-lifetime only (a fresh UUID per
// launch, cached so every LoadPlayer in one run agrees). The one allowed
// store is the browser's localStorage (player_store_js.go), the only
// place a persistent device id is needed: without it every web submit
// would send device_id "" and be rejected by the server's uuid column.

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strings"
	"sync"
	"unicode"
	"unicode/utf8"
)

// NameCharSet is the player-name charset: the single Go-side source of
// truth for what a leaderboard display name may contain (exactly what
// the 3×5 pixel font can draw). The DB CHECK mirrors it in SQL; ui's
// name entry and board.SanitizeDisplayName consume this const.
const NameCharSet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 .-"

type PlayerConfig struct {
	DeviceID string `json:"device_id"`
	Name     string `json:"name"`
	Best     int    `json:"best,omitempty"` // best score ever, local only
}

// processPlayer caches this process's identity: with no native disk
// store, it is the only thing keeping one run on a single device id.
// processMu guards it — the leaderboard UI reads the cache from the tick
// goroutine while a completed submission's goroutine writes the saved
// name (a real data race, caught by -race in the ui tests).
var (
	processMu     sync.Mutex
	processPlayer *PlayerConfig
)

// LoadPlayer returns the player identity. On first call it loads the
// store (browser localStorage) or generates a fresh UUID (native, which
// stores nothing); the result is cached for the process lifetime so the
// game, the leaderboard UI and submissions all agree on one device id.
func LoadPlayer() (PlayerConfig, error) {
	processMu.Lock()
	defer processMu.Unlock()
	if processPlayer != nil {
		return *processPlayer, nil
	}
	var pc PlayerConfig
	if data := loadPlayerBytes(); len(data) > 0 {
		if json.Unmarshal(data, &pc) == nil && pc.DeviceID != "" {
			processPlayer = &pc
			return pc, nil
		}
	}
	pc = PlayerConfig{DeviceID: newDeviceID()}
	processPlayer = &pc
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
	pc.Best = score
	pc.SaveName(pc.Name)
}

// SaveName sets the display name on both the receiver and the process
// cache, then best-effort stores it (a no-op natively).
func (pc *PlayerConfig) SaveName(name string) {
	processMu.Lock()
	pc.Name = name
	if processPlayer != nil {
		processPlayer.Name = name
		processPlayer.Best = pc.Best
	}
	processMu.Unlock()
	if data, err := json.MarshalIndent(*pc, "", "  "); err == nil {
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
		if r >= utf8.RuneSelf || !strings.ContainsRune(NameCharSet, r) {
			return "", false
		}
	}
	return s, true
}
