package ui

// The in-game leaderboard UI state machine: game-over flows into a
// submit prompt, pixel-font name entry, then the board screen (also
// reachable from the title screen with 'l'/'L'). Network calls run off the
// tick loop; submit/fetch are injectable for tests.

import (
	"context"
	"sync"
	"time"

	"github.com/Daviey/mario/board"
	"github.com/Daviey/mario/engine"
	"github.com/Daviey/mario/internal/persist"
	"github.com/Daviey/mario/render"
)

// requestTimeout bounds every leaderboard HTTP call.
const requestTimeout = 15 * time.Second

// nameCharSet is the entry charset: exactly what the pixel font can draw.
const nameCharSet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789 .-"

const maxNameLen = 8

type UI struct {
	mu      sync.Mutex
	mode    render.UIMode
	keys    []byte // captured while a UI screen owns input
	noted   []byte // bytes seen while inactive (title 'l' trigger)
	name    []byte
	score   int
	player  persist.PlayerConfig
	status  string
	rows    []render.ScoreRow
	loading bool
	asked   bool // asked about this game-over already
	quit    bool
	done    bool // submitted or declined: don't re-ask
	restart bool // board 'r': close the board and play again

	submit func(e board.Entry) error
	fetch  func() ([]board.Row, error)
}

// NewUI wires the machine. Nil submit/fetch default to the real
// board client from the environment (a nil pair stays offline).
func NewUI(submit func(board.Entry) error, fetch func() ([]board.Row, error)) *UI {
	u := &UI{mode: render.UIOff, submit: submit, fetch: fetch}
	if u.submit == nil && u.fetch == nil {
		if client, err := board.FromEnv(); err == nil {
			u.submit = func(e board.Entry) error {
				ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
				defer cancel()
				return client.Submit(ctx, e)
			}
			u.fetch = func() ([]board.Row, error) {
				dev := ""
				if pc, err := persist.LoadPlayer(); err == nil {
					dev = pc.DeviceID
				}
				ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
				defer cancel()
				return client.Top(ctx, 50, dev)
			}
		}
	}
	return u
}

// capturing reports whether a UI screen currently owns the keyboard.
func (u *UI) capturing() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.mode != render.UIOff
}

// FeedKeys buffers raw bytes while a UI screen owns input.
func (u *UI) FeedKeys(b []byte) {
	u.mu.Lock()
	defer u.mu.Unlock()

	// Terminals and the web build send multi-byte sequences for arrows and
	// key releases. If processed raw, the leading 0x1b instantly dismisses
	// the UI screens. Drop any chunk that looks like an escape sequence.
	if len(b) > 1 && b[0] == 0x1b && (b[1] == '[' || b[1] == 'O') {
		return
	}

	u.keys = append(u.keys, b...)
}

// note records bytes seen while the game owns input (bounded: only the
// newest bytes matter for the 'l' trigger).
func (u *UI) note(b []byte) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if n := len(u.noted) + len(b); n > 64 {
		drop := min(n-64, len(u.noted))
		u.noted = append(u.noted[:0], u.noted[drop:]...)
	}
	u.noted = append(u.noted, b...)
}

func (u *UI) quitRequested() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.quit
}

// takeRestart reports and clears a pending restart request from the
// board screen ('r'): consumed as a single edge, like a mapped key press.
func (u *UI) takeRestart() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	r := u.restart
	u.restart = false
	return r
}

// ShowBoard switches to the board screen and starts a fetch.
func (u *UI) ShowBoard() {
	u.mu.Lock()
	if u.fetch == nil {
		// Offline build (no credentials): still show the screen, with a
		// status — never an eternal LOADING.
		u.mode = render.UIBoard
		u.loading = false
		u.status = "OFFLINE"
		u.mu.Unlock()
		return
	}
	u.mode = render.UIBoard
	u.loading = true
	u.status = ""
	u.player, _ = persist.LoadPlayer()
	fetch := u.fetch
	u.mu.Unlock()
	go u.fetchInto(fetch)
}

func (u *UI) fetchInto(fetch func() ([]board.Row, error)) {
	defer func() {
		if r := recover(); r != nil {
			u.mu.Lock()
			u.loading = false
			u.status = "OFFLINE"
			u.rows = nil
			u.mu.Unlock()
		}
	}()
	rows, err := fetch()
	u.mu.Lock()
	defer u.mu.Unlock()
	u.loading = false
	if err != nil {
		u.status = "OFFLINE"
		u.rows = nil
		return
	}
	u.rows = boardRowsFor(rows)
}

// boardRowsFor converts API rows to render rows, marking the player's own
// entries. The board shows the top ten.
func boardRowsFor(rows []board.Row) []render.ScoreRow {
	if len(rows) > 10 {
		rows = rows[:10] // the board shows the top ten
	}
	out := make([]render.ScoreRow, 0, len(rows))
	for i, r := range rows {
		out = append(out, render.ScoreRow{
			Rank:  i + 1,
			Name:  r.Name,
			Score: r.Score,
			Mine:  r.Mine,
		})
	}
	return out
}

// tick advances the machine one game tick and returns the render snapshot
// (nil when no UI screen is showing).
func (u *UI) Tick(g *engine.Game) *render.ScoreUI {
	u.mu.Lock()
	defer u.mu.Unlock()

	// Title-screen 'l'/'L' opens the board. Both cases: the hint renders
	// as "L LEADERBOARD" and shift/caps-lock make case unpredictable.
	if u.mode == render.UIOff && g.State == engine.StateTitle && len(u.noted) > 0 {
		if bytesContain(u.noted, 'l') || bytesContain(u.noted, 'L') {
			u.noted = nil
			u.mode = render.UIBoard
			u.player, _ = persist.LoadPlayer()
			if u.fetch == nil {
				u.loading = false
				u.status = "OFFLINE"
			} else {
				u.loading = true
				u.status = ""
				fetch := u.fetch
				go u.fetchInto(fetch)
			}
			return u.snapshotLocked(g)
		}
		u.noted = nil
	}

	// Game over / win with a score: offer submission once.
	if u.mode == render.UIOff && !u.asked &&
		(g.State == engine.StateGameOver || g.State == engine.StateWin) && g.Score > 0 {
		u.asked = true
		u.player, _ = persist.LoadPlayer()
		u.score = g.Score
		u.mode = render.UIAsk
	}

	keys := u.keys
	u.keys = nil
	for _, b := range keys {
		u.keyLocked(b)
	}

	if u.mode == render.UIOff {
		return nil
	}
	return u.snapshotLocked(g)
}

func (u *UI) keyLocked(b byte) {
	switch u.mode {
	case render.UIAsk:
		switch {
		case b == 'y' || b == 'Y':
			u.mode = render.UIEntry
			u.name = nil
		case b == 'n' || b == 'N' || b == 'q' || b == 'Q' || b == 0x1b:
			u.mode = render.UIOff
			u.done = true
		}
	case render.UIEntry:
		switch {
		case b == '\r' || b == '\n':
			u.submitLocked()
		case b == 0x08 || b == 0x7f:
			if len(u.name) > 0 {
				u.name = u.name[:len(u.name)-1]
			}
		case b == 0x1b:
			u.mode = render.UIAsk // back to the prompt
		default:
			c := upperByte(b)
			if len(u.name) < maxNameLen && byteIn(c, nameCharSet) {
				u.name = append(u.name, c)
			}
		}
	case render.UIBoard:
		switch {
		case b == 'q' || b == 'Q' || b == 'l' || b == 'L' || b == 0x1b:
			u.mode = render.UIOff
			if u.done {
				u.quit = true // submitted/declined: game is over anyway
			}
		case b == 'r' || b == 'R':
			// Close and play again. Clearing the once-per-run flags lets
			// the next game over offer submission again.
			u.mode = render.UIOff
			u.asked = false
			u.done = false
			u.restart = true
		}
	}
}

func (u *UI) submitLocked() {
	name := string(u.name)
	if name == "" {
		name = u.player.Name
	}
	if s, ok := persist.SanitizeName(name); ok {
		name = s
	} else {
		name = "MARIO"
	}
	player := u.player
	player.Name = name
	submit, fetch := u.submit, u.fetch
	score := u.score
	u.mode = render.UIBoard
	u.done = true
	u.loading = true
	if submit == nil {
		u.status = "OFFLINE"
		u.loading = false
		return
	}
	u.status = "SUBMITTING"
	go func() {
		err := submit(board.Entry{Name: name, Score: score, DeviceID: player.DeviceID})
		u.mu.Lock()
		if err != nil {
			u.status = "SUBMIT FAILED"
		} else {
			u.status = "SUBMITTED!"
			player.SaveName(name)
		}
		u.mu.Unlock()
		if fetch != nil {
			u.fetchInto(fetch)
		}
	}()
}

func (u *UI) snapshotLocked(g *engine.Game) *render.ScoreUI {
	return &render.ScoreUI{
		Mode:     u.mode,
		Score:    u.score,
		Name:     string(u.name),
		CursorOn: g.Tick%60 < 30,
		Rows:     u.rows,
		Loading:  u.loading,
		Status:   u.status,
		Title:    g.State == engine.StateTitle,
	}
}

func upperByte(b byte) byte {
	if b >= 'a' && b <= 'z' {
		return b - 'a' + 'A'
	}
	return b
}

func byteIn(b byte, set string) bool {
	for i := 0; i < len(set); i++ {
		if set[i] == b {
			return true
		}
	}
	return false
}

func bytesContain(b []byte, c byte) bool {
	for _, x := range b {
		if x == c {
			return true
		}
	}
	return false
}
