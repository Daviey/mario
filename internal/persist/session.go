package persist

import "sync"

// Session is an independent player identity for hosts that run many game
// instances in one process (the SSH server gives one to every connection).
// The process-wide LoadPlayer cache cannot serve them: all players would
// share one device id — tripping the server's 2/min per-device submit rate
// limit collectively and marking every leaderboard row as "mine" — and the
// cache itself is not goroutine-safe across sessions.
//
// A Session touches nothing on disk: like the native per-launch identity,
// it lives for its owner's lifetime only.
type Session struct {
	mu sync.Mutex
	pc PlayerConfig
}

// BeginSession returns a fresh identity with a new device id.
func BeginSession() *Session {
	return &Session{pc: PlayerConfig{DeviceID: newDeviceID()}}
}

// Player returns the session's current identity snapshot.
func (s *Session) Player() PlayerConfig {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pc
}

// SaveName records the display name typed at the entry screen.
func (s *Session) SaveName(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pc.Name = name
}

// SaveBest records the best score of the session (no-op otherwise).
func (s *Session) SaveBest(score int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if score > s.pc.Best {
		s.pc.Best = score
	}
}
