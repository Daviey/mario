package main

// The -serve entry: an unauthenticated SSH server whose only service is
// this game. Every connection gets its own App, its own player identity
// and its own render stream; the SSH session is just a terminal pipe.

import (
	"net"
	"slices"
	"sync"
	"time"

	"github.com/Daviey/mario"
	"github.com/Daviey/mario/engine"
	"github.com/Daviey/mario/input"
	"github.com/Daviey/mario/internal/persist"
	"github.com/Daviey/mario/internal/sshd"
	"github.com/Daviey/mario/render"
)

// runServe serves the game over SSH until the listener fails. moshBin
// (from -mosh, empty = mosh handshake disabled) enables anonymous mosh
func runServe(levels []*engine.Level, addr, hostKeyFile string, trueColor bool, moshBin, moshPorts string, maxSessions int) error {
	// Calibration warm-start, keyed by client host (see calCache). One
	// per server process, shared by every session handler.
	cals := &calCache{}
	srv := &sshd.Server{
		Addr:          addr,
		HostKeyFile:   hostKeyFile,
		Handler:       func(s *sshd.Session) { playSession(levels, s, trueColor, cals) },
		MoshBin:       moshBin,
		MoshPortRange: moshPorts,
	}
	if maxSessions > 0 {
		srv.MaxSessions = maxSessions
	}
	return srv.ListenAndServe()
}

// calCache carries mapper calibration — the measured OS key-repeat delay
// and per-key hold habits — across connections of the same client host,
// in memory only, for the life of the server process. Nothing is ever
// written to a player's machine (that privacy rule is untouched); the
// host remembering how a repeat player's terminal times its repeats is
// the same class of state as the leaderboard's device id. Without it
// every SSH connection starts a cold mapper, and the first hold of each
// movement key stalls for the OS repeat delay (~0.5s) — every connect,
// forever (the contract is modeled in input/feel_test.go,
// TestCalibrationSurvivesRestart).
type calCache struct {
	mu sync.Mutex
	m  map[string]input.Calibration
}

// calCacheMax bounds the map; when full it is dropped wholesale — every
// stored entry is re-learned within seconds of play, so a periodic
// reset is cheaper than an LRU.
const calCacheMax = 128

func (c *calCache) get(key string) (input.Calibration, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	cal, ok := c.m[key]
	return cal, ok
}

func (c *calCache) put(key string, cal input.Calibration) {
	// Only store sessions that learned something: an idle connect must
	// not blank a repeat player's good entry.
	if cal.OSDelay == 0 && !slices.Contains(cal.HeldHabit, true) {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = make(map[string]input.Calibration)
	}
	if len(c.m) >= calCacheMax {
		clear(c.m)
	}
	c.m[key] = cal
}

// calKey reduces a remote address to its host part, so a player's
// reconnects (new source port every time) share one entry.
func calKey(remote string) string {
	if host, _, err := net.SplitHostPort(remote); err == nil {
		return host
	}
	return remote
}

// sessionMapper builds the session's input mapper, warm from the cache
// when this host has played before. The returned func stores what the
// session learned (call it when the session ends).
func sessionMapper(cals *calCache, remote string) (*input.Mapper, func()) {
	key := calKey(remote)
	m := input.NewMapper()
	if cal, ok := cals.get(key); ok {
		m.ApplyCalibration(cal)
	}
	return m, func() { cals.put(key, m.Calibration()) }
}

// sessWriter adapts Session to io.Writer for render.Stream, translating a
// dead connection into a session shutdown so the game loop exits instead
// of spinning on failing writes.
type sessWriter struct{ s *sshd.Session }

func (w sessWriter) Write(p []byte) (int, error) {
	n, err := w.s.Write(p)
	if err != nil {
		w.s.Close()
	}
	return n, err
}

// playSession runs one game per SSH connection — the same wiring as the
// native runner (cmd/mario/main.go run), pointed at the SSH channel
// instead of stdout.
func playSession(levels []*engine.Level, s *sshd.Session, trueColor bool, cals *calCache) {
	// Fill the terminal like the native runner does: width in tiles,
	// height minus HUD/status rows, Pix/2 terminal rows per tile.
	cols, rows := s.Size()
	viewW := cols / render.Pix
	viewH := (rows - 2) * 2 / render.Pix

	mapper, saveCal := sessionMapper(cals, s.RemoteAddr())
	defer saveCal() // the session's learned repeat timing serves the next connect

	app := mario.New(&mario.Options{
		Levels:  levels,
		ViewW:   viewW,
		ViewH:   viewH,
		Mapper:  mapper,                 // warm calibration, per-remote-host
		Session: persist.BeginSession(), // per-connection player identity
	})
	s.OnFeed(app.Feed)
	// Client-side resizes follow like the native runner's SIGWINCH: new
	// viewport on the next tick, full repaint at the new size.
	s.OnResize(func(cols, rows int) {
		app.Resize(cols/render.Pix, (rows-2)*2/render.Pix)
	})

	// Terminal setup/teardown mirrors run()'s (kitty keyboard protocol,
	// alt screen, hidden cursor, window title). Unsupported terminals
	// ignore these. The epilogue waits for the writer below so it lands
	// after any in-flight frame diff — a trailing partial frame after the
	// alt-screen exit would print garbage on the player's shell.
	s.Write([]byte("\x1b[<u\x1b[>11u\x1b[?1049h\x1b[?25l\x1b[2J\x1b[22t\x1b]0;SUPER CLI MARIO\a"))

	out := sessWriter{s: s}
	st := render.NewStream(out, render.NewPalette(trueColor))

	// Rendering and writing are decoupled. The tick goroutine snapshots
	// each frame (Snapshot reads engine state, which is only quiescent
	// between Steps); a writer goroutine diffs and sends. When the
	// client's terminal drains slower than the game produces frames — SSH
	// flow control closes the channel window and Write blocks — only a
	// frame is dropped. The ticks themselves never stall, so a congested
	// link costs a skipped frame instead of dropped ticks: without this,
	// a slow drain slows the simulation itself and the controls turn
	// mushy on top of the network latency.
	frames := make(chan *render.Screen, 1)
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for cur := range frames {
			st.Flush(cur)
		}
	}()
	defer func() {
		close(frames)
		select {
		case <-drained:
		case <-time.After(500 * time.Millisecond): // wedged writer: leave anyway
		}
		s.Write([]byte("\x1b[?2026l\x1b[<u\x1b[?25h\x1b[?23t\x1b[?1049l\x1b[0m\r\n"))
	}()

	tick := time.NewTicker(time.Second / engine.TicksPerSecond)
	defer tick.Stop()
	for {
		select {
		case <-s.Done(): // client disconnected
			return
		case <-tick.C:
		}
		app.Step()
		var cur *render.Screen
		if ui := app.UI(); ui != nil {
			cur = st.Snapshot(app.Game, ui)
		} else {
			cur = st.Snapshot(app.Game)
		}
		// Latest-frame mailbox: a busy writer means the queued frame is
		// stale — supersede it, so the pipe always jumps to the newest
		// state instead of draining a backlog.
		select {
		case frames <- cur:
		default:
			select {
			case <-frames:
			default:
			}
			select {
			case frames <- cur:
			default:
			}
		}
		if app.Quit() {
			return
		}
	}
}
