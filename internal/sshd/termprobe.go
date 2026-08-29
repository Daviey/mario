package sshd

import (
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Daviey/mario/render"
)

// Terminal color-depth probing.
//
// COLORTERM — the only reliable truecolor hint — never crosses an ssh
// session by default, and mosh-server overwrites the child's TERM to
// xterm-256color, so the game cannot learn the client terminal's color
// depth from its environment alone. TERM cannot separate the truecolor
// terminals (VTE, Konsole, …) from the 256-only ones (Apple Terminal,
// urxvt, pre-2020 xterm): they all speak xterm-256color.
//
// But during the ssh phase we hold a live line to the client's
// terminal, and terminals identify themselves when asked: the
// secondary/tertiary device attributes (DA2/DA3, the queries behind
// vim's 'termresponse') name the emulator family, and 24-bit support
// is a property of the family. On pty-req we send both queries and
// sniff the replies out of the inbound stream; anything the player
// types meanwhile is buffered and replayed when the session installs
// its feed (Session.OnFeed drains the probe).
//
// Decision precedence (channel.decideColorTerm): an explicit COLORTERM
// env request beats the TERM family, which beats the probe. Terminals
// that stay silent (piped clients, high latency) fall back to the TERM
// rules — never worse than before the probe existed.

// termQuery is written to the client at pty-req: DA2 (CSI > c) then
// DA3 (CSI = c). Queries produce no output themselves, so the stray
// bytes are invisible to the player.
const termQuery = "\x1b[>c\x1b[=c"

const defaultProbeWait = 250 * time.Millisecond

type termProbe struct {
	mu      sync.Mutex
	decided bool // a reply yielded a family decision
	trueClr bool // … and its verdict
	da2     string
	da3     string
	active  bool   // buffering mode (until OnFeed drains it)
	buf     []byte // scratch: partial replies + buffered passthrough
	known   chan struct{}
}

func newTermProbe() *termProbe {
	return &termProbe{active: true, known: make(chan struct{})}
}

// offer hands one inbound chunk to the probe. In buffering mode
// (before the session's feed is installed) everything is held for the
// drain and the bool is true. After the drain the probe keeps
// filtering: any reply arriving late is stripped and the remaining
// bytes are returned for the feed, so a stray DA2/DA3 never reaches
// the game as keystrokes.
func (p *termProbe) offer(b []byte) (rest []byte, buffered bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.buf = append(p.buf, b...)
	p.parseLocked()
	if p.active {
		return nil, true
	}
	out := p.buf
	p.buf = nil
	return out, false
}

// parseLocked extracts every complete DA2/DA3 reply in buf, removing
// it. Bytes that are not replies stay in buf: buffered mode holds them
// for the drain, filter mode passes them through. Incomplete replies
// wait for more data. Records a family decision the first time the
// replies identify a known terminal.
func (p *termProbe) parseLocked() {
	out := p.buf[:0]
	scan := p.buf
	for len(scan) > 0 {
		i := indexByteSeq(scan, "\x1b")
		if i < 0 {
			out = append(out, scan...)
			scan = nil
			break
		}
		out = append(out, scan[:i]...)
		rest := scan[i:]
		switch {
		case hasPrefixBytes(rest, "\x1b[>"):
			end := indexByteSeq(rest, "c")
			if end < 0 { // partial DA2: hold for more data
				out = append(out, rest...)
				scan = nil
			} else {
				p.da2 = ">" + string(rest[3:end]) // table keys carry the ">"
				scan = rest[end+1:]
			}
		case hasPrefixBytes(rest, "\x1bP!|"):
			best, bestLen := -1, 0
			for _, term := range []string{"\x1b\\", "\a"} {
				if j := indexByteSeq(rest, term); j > 4 && (best < 0 || j < best) {
					best, bestLen = j, len(term)
				}
			}
			if best < 0 { // partial DA3
				out = append(out, rest...)
				scan = nil
			} else {
				p.da3 = string(rest[4:best])
				scan = rest[best+bestLen:]
			}
		default:
			// Some other escape sequence (user input): pass it
			// through and keep scanning behind it.
			out = append(out, scan[:1]...)
			scan = rest[1:]
		}
	}
	if len(scan) > 0 {
		out = append(out, scan...)
	}
	p.buf = out
	if !p.decided {
		if d, ok := daDecision(p.da2, p.da3); ok {
			p.decided, p.trueClr = true, d
			close(p.known)
		}
	}
}

// result reports the probe verdict, waiting up to maxWait for a reply
// to arrive. decided is false when the terminal never identified
// itself in a family the table knows.
func (p *termProbe) result(maxWait time.Duration) (decided, trueColor bool) {
	p.mu.Lock()
	if p.decided || maxWait <= 0 {
		decided, trueColor = p.decided, p.trueClr
		p.mu.Unlock()
		return
	}
	known := p.known
	p.mu.Unlock()
	select {
	case <-known:
	case <-time.After(maxWait):
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.decided, p.trueClr
}

// drain switches the probe from buffering to filtering and returns
// everything held so far (player keystrokes from the probe window).
func (p *termProbe) drain() []byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.active = false
	out := p.buf
	p.buf = nil
	return out
}

// daDecision maps observed DA2/DA3 replies to 24-bit color support.
// Only families with well-known replies appear; anything else leaves
// the decision to the TERM rules. Entries:
//   - DA3 is the terminal's own name (DCS ! | name ST): Apple_Terminal
//     has no truecolor support (as of 2026 it still discards 38;2);
//     kitty/wezterm/ghostty/contour/foot are 24-bit.
//   - DA2 ">65;" is VTE (gnome-terminal & friends): 24-bit.
//   - DA2 ">84;" is tmux: it quantizes 24-bit to its outer terminal,
//     so claiming truecolor is safe either way.
//   - DA2 ">115;" is Konsole: 24-bit.
//   - DA2 ">41;<patch>;" is xterm: truecolor landed in patch 362
//     (2020); the patch level is the second field.
func daDecision(da2, da3 string) (decided, trueColor bool) {
	if strings.Contains(da3, "Apple_Terminal") {
		return true, false
	}
	for _, n := range []string{"kitty", "wezterm", "ghostty", "contour", "foot"} {
		if strings.Contains(da3, n) {
			return true, true
		}
	}
	switch {
	case strings.HasPrefix(da2, ">65;"), strings.HasPrefix(da2, ">84;"), strings.HasPrefix(da2, ">115;"):
		return true, true
	case strings.HasPrefix(da2, ">41;"):
		fields := strings.Split(strings.TrimPrefix(da2, ">41;"), ";")
		if patch, err := strconv.Atoi(fields[0]); err == nil {
			return true, patch >= 362
		}
	}
	return false, false
}

// decideColorTerm resolves the COLORTERM value for a game the server
// is about to start: an explicit env request wins, then the TERM
// family (render.TrueColorTerm), then the DA2/DA3 probe. Empty means
// the 16-color palette.
func (ch *channel) decideColorTerm() string {
	if v := ch.envColorTerm(); v != "" {
		return v
	}
	if render.TrueColorTerm(ch.termValue()) {
		return "truecolor"
	}
	if p := ch.probeRef(); p != nil {
		if decided, ok := p.result(ch.probeWait()); decided && ok {
			return "truecolor"
		}
	}
	return ""
}

func (ch *channel) termValue() string {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	return ch.term
}

func (ch *channel) probeRef() *termProbe {
	ch.mu.Lock()
	defer ch.mu.Unlock()
	return ch.probe
}

func (ch *channel) probeWait() time.Duration {
	if w := ch.wait; w > 0 {
		return w
	}
	return defaultProbeWait
}

// indexByteSeq finds the first occurrence of sep in b.
func indexByteSeq(b []byte, sep string) int {
	return strings.Index(string(b), sep)
}

func hasPrefixBytes(b []byte, p string) bool {
	return strings.HasPrefix(string(b), p)
}
