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

// Entry accepts exactly persist.NameCharSet — the one Go-side charset
// shared with SanitizeName/SanitizeDisplayName and the DB CHECK.
const maxNameLen = 8

type UI struct {
	mu      sync.Mutex
	mode    render.UIMode
	keys    []byte // captured while a UI screen owns input
	noted   []byte // bytes seen while inactive (title 'l' trigger)
	name    []byte
	score   int
	level   int // 1-based level reached at game over/win
	player  persist.PlayerConfig
	status  string
	rows    []render.ScoreRow
	loading bool
	asked   bool // asked about this game-over already
	quit    bool
	done    bool // submitted or declined: don't re-ask
	restart bool // board 'r': close the board and play again
	daily   bool // showing the daily-challenge board
	dailyGo bool // title 'd': the App should start a daily run
	day     string
	rank    int  // post-submit rank in the fetched rows, 0 = unknown
	subOK   bool // a submission landed successfully (telemetry funnel flag)

	replaySrc func() (string, bool) // the App's recording, for verification
	submit    func(e board.Entry) error
	fetch     func() ([]board.Row, error)
	playCtx   func() board.Entry // App-supplied play context (surface/term/UA/…)

	// loadIdentity/saveName override the process-wide player identity for
	// multi-session hosts (the SSH server): one player identity per
	// connection instead of one per process. Nil keeps the native path.
	loadIdentity func() persist.PlayerConfig
	saveName     func(string)
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
				dev := u.identityLocked().DeviceID
				ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
				defer cancel()
				if u.dailyMode() {
					return client.TopMode(ctx, 50, dev, "daily", u.dayString())
				}
				return client.Top(ctx, 50, dev)
			}
		}
	}
	return u
}

// SetIdentity routes the machine's player identity (and name persistence)
// through the given functions. Multi-session hosts call it before the
// first Tick; the native single-session path never does.
func (u *UI) SetIdentity(load func() persist.PlayerConfig, saveName func(string)) {
	u.mu.Lock()
	u.loadIdentity, u.saveName = load, saveName
	u.mu.Unlock()
}

// SetReplaySource wires in the App's input recorder; submissions carry the
// recording so the server can verify the run. Without a shippable
// recording the submit is refused client-side.
func (u *UI) SetReplaySource(src func() (string, bool)) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.replaySrc = src
}

// SetPlayContext wires the App-supplied play context (surface, TERM,
// user agent, input regime, viewport): evaluated at submit time so the
// values describe the run as it ended. Entries start from it; Submit
// clamps whatever lands here.
func (u *UI) SetPlayContext(ctx func() board.Entry) {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.playCtx = ctx
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

// dailyMode reports whether the machine currently targets the daily board.
func (u *UI) dailyMode() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.daily
}

// today is the UTC challenge day (YYYY-MM-DD). The engine stays
// wall-clock free; the UI layer owns the calendar.
func today() string { return time.Now().UTC().Format("2006-01-02") }

// dayString returns the challenge day the machine is currently showing.
func (u *UI) dayString() string {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.day == "" {
		return today()
	}
	return u.day
}

// TakeDailyAtTitle reports and clears a title-screen 'd' press (the
// daily challenge trigger). It must run BEFORE the engine update: the
// same kitty 'd' press also maps to Right, which would otherwise dismiss
// the title as a movement key before Tick ever saw the note.
func (u *UI) TakeDailyAtTitle(g *engine.Game) bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.mode != render.UIOff || g.State != engine.StateTitle || len(u.noted) == 0 {
		return false
	}
	if bytesContain(u.noted, 'd') || bytesContain(u.noted, 'D') {
		u.noted = nil
		return true
	}
	return false
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
	defer u.mu.Unlock()
	u.showBoardLocked()
}

// showBoardLocked is the one board-open path (caller holds the mutex):
// the classic board, a reset day, a fresh identity snapshot, and the
// fetch started — or the OFFLINE status when no fetch is wired. The
// title-'l' path and ShowBoard must agree, or the board a daily run
// leaves behind leaks into the next title-open.
func (u *UI) showBoardLocked() {
	u.daily = false // opened outside a run: the classic board
	u.day = ""
	u.mode = render.UIBoard
	u.player = u.identityLocked()
	if u.fetch == nil {
		// Offline build (no credentials): still show the screen, with a
		// status — never an eternal LOADING.
		u.loading = false
		u.status = "OFFLINE"
		return
	}
	u.loading = true
	u.status = ""
	fetch := u.fetch
	go u.fetchInto(fetch)
}

// ShowAbout switches to the about screen (title 'i').
func (u *UI) ShowAbout() {
	u.mu.Lock()
	defer u.mu.Unlock()
	u.mode = render.UIAbout
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
	if err != nil && board.Transient(err) {
		// Transient edge slowness must not read as OFFLINE: once in a
		// blue moon a fetch hangs to the 15s timeout against the
		// Supabase/Cloudflare edge (observed live on the SSH host);
		// one immediate retry absorbs it. A status the server chose
		// deliberately (PoW/RLS/4xx/429) would fail identically again,
		// so it is not retried — and neither is anything once the
		// board has closed under the fetch.
		u.mu.Lock()
		open := u.mode == render.UIBoard
		u.mu.Unlock()
		if open {
			rows, err = fetch()
		}
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	u.loading = false
	if err != nil {
		u.status = "OFFLINE"
		u.rows = nil
		return
	}
	u.rows = boardRowsFor(rows)
	u.rank = 0
	for i, r := range rows {
		if r.Mine {
			u.rank = i + 1
			break
		}
	}
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
			Rank:     i + 1,
			Name:     r.Name,
			Score:    r.Score,
			Level:    r.Level,
			Mine:     r.Mine,
			Verified: r.Verified,
		})
	}
	return out
}

// tick advances the machine one game tick and returns the render snapshot
// (nil when no UI screen is showing).
func (u *UI) Tick(g *engine.Game) *render.ScoreUI {
	u.mu.Lock()
	defer u.mu.Unlock()

	// Title-screen 'l'/'L' opens the board, 'i'/'I' the about screen.
	// Both cases each: shift/caps-lock make case unpredictable.
	if u.mode == render.UIOff && g.State == engine.StateTitle && len(u.noted) > 0 {
		if bytesContain(u.noted, 'l') || bytesContain(u.noted, 'L') {
			u.noted = nil
			u.showBoardLocked()
			return u.snapshotLocked(g)
		}
		if bytesContain(u.noted, 'i') || bytesContain(u.noted, 'I') {
			u.noted = nil
			u.mode = render.UIAbout
			return u.snapshotLocked(g)
		}
		u.noted = nil
	}

	// Game over / win with a score: offer submission once.
	if u.mode == render.UIOff && !u.asked &&
		(g.State == engine.StateGameOver || g.State == engine.StateWin) && g.Score > 0 {
		u.asked = true
		u.player = u.identityLocked()
		u.score = g.Score
		u.level = g.LevelIndex() + 1
		u.daily = g.Daily
		u.day = today()
		u.rank = 0
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
			if len(u.name) < maxNameLen && byteIn(c, persist.NameCharSet) {
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
	case render.UIAbout:
		// 'i' toggles, ESC/Q back out. About never follows a run, so no
		// done/quit bookkeeping is needed.
		if b == 'i' || b == 'I' || b == 'q' || b == 'Q' || b == 0x1b {
			u.mode = render.UIOff
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
	score, level := u.score, u.level
	u.mode = render.UIBoard
	u.done = true
	u.loading = true
	if submit == nil {
		u.status = "OFFLINE"
		u.loading = false
		return
	}
	var replayData string
	if u.replaySrc != nil {
		if d, ok := u.replaySrc(); ok && d != "" {
			replayData = d
		}
	}
	if replayData == "" {
		// The server rejects replay-less rows; don't bother it.
		u.status = "UNRECORDED"
		u.loading = false
		return
	}
	u.status = "SUBMITTING"
	mode, day := "", ""
	if u.daily {
		mode, day = "daily", u.day
	}
	entry := board.Entry{Name: name, Score: score, Level: level, DeviceID: player.DeviceID, Mode: mode, Day: day, Replay: replayData}
	if u.playCtx != nil {
		if ctx := u.playCtx(); ctx.DeviceID == "" {
			entry.Surface, entry.UserAgent, entry.Term = ctx.Surface, ctx.UserAgent, ctx.Term
			entry.ColorTerm, entry.InputRegime, entry.Viewport = ctx.ColorTerm, ctx.InputRegime, ctx.Viewport
		}
	}
	go func() {
		err := submit(entry)
		u.mu.Lock()
		if err != nil {
			u.status = "SUBMIT FAILED"
		} else {
			u.status = "SUBMITTED!"
			u.subOK = true
			if u.saveName != nil {
				u.saveName(name)
			} else {
				player.SaveName(name)
			}
		}
		u.mu.Unlock()
		if fetch != nil {
			u.fetchInto(fetch)
		}
	}()
}

// Submitted reports whether any score submission from this machine
// succeeded — the played→submitted funnel flag for session telemetry.
func (u *UI) Submitted() bool {
	u.mu.Lock()
	defer u.mu.Unlock()
	return u.subOK
}

func (u *UI) snapshotLocked(g *engine.Game) *render.ScoreUI {
	best := g.Best
	rank := 0
	if u.mode == render.UIBoard {
		rank = u.rank
	}
	return &render.ScoreUI{
		Mode:     u.mode,
		Score:    u.score,
		Name:     string(u.name),
		CursorOn: g.Tick%60 < 30,
		Rows:     u.rows,
		Loading:  u.loading,
		Status:   u.status,
		Title:    g.State == engine.StateTitle,
		Best:     best,
		Rank:     rank,
		Daily:    u.daily,
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

// identityLocked refreshes the cached player snapshot. Session hosts read
// their per-connection identity; the native path reads the process cache.
func (u *UI) identityLocked() persist.PlayerConfig {
	if u.loadIdentity != nil {
		return u.loadIdentity()
	}
	pc, _ := persist.LoadPlayer()
	return pc
}
