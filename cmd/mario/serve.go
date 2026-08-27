package main

// The -serve entry: an unauthenticated SSH server whose only service is
// this game. Every connection gets its own App, its own player identity
// and its own render stream; the SSH session is just a terminal pipe.

import (
	"time"

	"github.com/Daviey/mario"
	"github.com/Daviey/mario/engine"
	"github.com/Daviey/mario/internal/persist"
	"github.com/Daviey/mario/internal/sshd"
	"github.com/Daviey/mario/render"
)

// runServe serves the game over SSH until the listener fails.
func runServe(levels []*engine.Level, addr, hostKeyFile string, trueColor bool) error {
	srv := &sshd.Server{
		Addr:        addr,
		HostKeyFile: hostKeyFile,
		Handler:     func(s *sshd.Session) { playSession(levels, s, trueColor) },
	}
	return srv.ListenAndServe()
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
func playSession(levels []*engine.Level, s *sshd.Session, trueColor bool) {
	// Fill the terminal like the native runner does: width in tiles,
	// height minus HUD/status rows, Pix/2 terminal rows per tile.
	cols, rows := s.Size()
	viewW := cols / render.Pix
	viewH := (rows - 2) * 2 / render.Pix

	app := mario.New(&mario.Options{
		Levels:  levels,
		ViewW:   viewW,
		ViewH:   viewH,
		Session: persist.BeginSession(), // per-connection player identity
	})
	s.OnFeed(app.Feed)

	// Terminal setup/teardown mirrors run()'s (kitty keyboard protocol,
	// alt screen, hidden cursor, window title). Unsupported terminals
	// ignore these.
	s.Write([]byte("\x1b[<u\x1b[>11u\x1b[?1049h\x1b[?25l\x1b[2J\x1b[22t\x1b]0;SUPER CLI MARIO\a"))
	defer s.Write([]byte("\x1b[?2026l\x1b[<u\x1b[?25h\x1b[23t\x1b[?1049l\x1b[0m\r\n"))

	out := sessWriter{s: s}
	st := render.NewStream(out, render.NewPalette(trueColor))

	tick := time.NewTicker(time.Second / engine.TicksPerSecond)
	defer tick.Stop()
	for {
		select {
		case <-s.Done(): // client disconnected
			return
		case <-tick.C:
		}
		app.Step()
		if ui := app.UI(); ui != nil {
			st.Draw(app.Game, ui)
		} else {
			st.Draw(app.Game)
		}
		if app.Quit() {
			return
		}
	}
}
