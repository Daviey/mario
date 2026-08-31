package sshd

import (
	"strconv"
	"strings"
	"sync"
	"time"
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

// termQuery is written to the client when a shell session starts: DA2
// (CSI > c) then DA3 (CSI = c). Queries produce no output themselves,
// so the stray bytes are invisible to the player; the trailing CRLF
// keeps any line-based consumer behind the terminal from tripping over
// the unterminated sequence.
const termQuery = "\x1b[>c\x1b[=c\r\n"

const defaultProbeWait = 250 * time.Millisecond

// probeBufMax caps how much input the probe will hold while waiting
// for the session's feed: a reply that never terminates (a truncated
// "\x1b[>", a cut DCS) must not keep buffering every later keystroke
// without limit. Past the cap — or once the probe's wait window is
// over and no reply can complete the decision — probing is abandoned
// and everything held flows back: there is no path where input is
// swallowed indefinitely.
const probeBufMax = 4096

// termProbe sniffs DA2/DA3 replies out of the inbound byte stream. It
// starts in buffering mode (keystrokes held until the session installs
// its feed), switches to pure filtering once drained, and can abandon
// itself (gaveUp) when a reply never terminates — see offer.
type termProbe struct {
	mu       sync.Mutex
	decided  bool // a reply yielded a family decision
	trueClr  bool // … and its verdict
	da2      string
	da3      string
	active   bool   // buffering mode (until OnFeed drains it)
	buf      []byte // scratch: partial replies + buffered passthrough
	known    chan struct{}
	deadline time.Time // buffering-mode expiry (creation + ProbeWait)
	gaveUp   bool      // probe abandoned: pure passthrough, nothing held
}

// newTermProbe starts a probe in buffering mode that gives itself up
// after maxWait (defaultProbeWait when zero or negative).
func newTermProbe(maxWait time.Duration) *termProbe {
	if maxWait <= 0 {
		maxWait = defaultProbeWait
	}
	return &termProbe{
		active:   true,
		known:    make(chan struct{}),
		deadline: time.Now().Add(maxWait),
	}
}

// offer hands one inbound chunk to the probe. In buffering mode
// (before the session's feed is installed) everything is held for the
// drain and the bool is true. After the drain the probe keeps
// filtering: any reply arriving late is stripped and the remaining
// bytes are returned for the feed, so a stray DA2/DA3 never reaches
// the game as keystrokes.
//
// The hold is bounded twice over (probeBufMax bytes, and the probe's
// own wait window): whichever runs out first abandons probing and
// flushes everything back, so a terminal that answers with an
// unterminated escape cannot wedge input forever.
func (p *termProbe) offer(b []byte) (rest []byte, buffered bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.gaveUp {
		// Abandoned: pure passthrough, nothing is ever held again.
		return b, false
	}
	p.buf = append(p.buf, b...)
	if p.active && (len(p.buf) > probeBufMax || time.Now().After(p.deadline)) {
		p.gaveUp = true
		p.active = false
		out := p.buf
		p.buf = nil
		return out, false
	}
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

// da3Families maps a terminal's DA3 name (DCS ! | name ST — substring
// match, terminals append versions) to its 24-bit color support.
// Only families with well-known replies appear; anything else leaves
// the decision to the TERM rules. Apple_Terminal has no truecolor
// support (as of 2026 it still discards 38;2). "6954726D" is iTerm2:
// it encodes its name in hex instead of sending it literally ("iTrm",
// VT100Output.m reportTertiaryDeviceAttribute).
var da3Families = []struct {
	name      string
	trueColor bool
}{
	{"Apple_Terminal", false},
	{"kitty", true},
	{"wezterm", true},
	{"ghostty", true},
	{"contour", true},
	{"foot", true},
	{"6954726D", true}, // iTerm2, hex-encoded
}

// da2Families maps a DA2 attribute prefix to 24-bit color support:
// ">65;" is VTE (gnome-terminal & friends), ">84;" is tmux (it
// quantizes 24-bit to its outer terminal, so claiming truecolor is
// safe either way), ">115;" is Konsole. xterm (">41;<patch>;") is not
// here: its verdict depends on the patch level — see daDecision.
var da2Families = []struct {
	prefix    string
	trueColor bool
}{
	{">65;", true},  // VTE
	{">84;", true},  // tmux
	{">115;", true}, // Konsole
}

// xtermPatchTruecolor is the xterm patch level where truecolor landed
// (2020): DA2 ">41;<patch>;" carries the patch level in its second
// field, and only patches at or above this render 38;2.
const xtermPatchTruecolor = 362

// daDecision consults the tables above: DA3 names first, then DA2
// family prefixes, then the xterm patch gate. (decided, _) is false
// when no table row matches — the TERM rules decide instead.
func daDecision(da2, da3 string) (decided, trueColor bool) {
	for _, f := range da3Families {
		if strings.Contains(da3, f.name) {
			return true, f.trueColor
		}
	}
	for _, f := range da2Families {
		if strings.HasPrefix(da2, f.prefix) {
			return true, f.trueColor
		}
	}
	if rest, ok := strings.CutPrefix(da2, ">41;"); ok {
		if patch, err := strconv.Atoi(strings.Split(rest, ";")[0]); err == nil {
			return true, patch >= xtermPatchTruecolor
		}
	}
	return false, false
}

// indexByteSeq finds the first occurrence of sep in b.
func indexByteSeq(b []byte, sep string) int {
	return strings.Index(string(b), sep)
}

func hasPrefixBytes(b []byte, p string) bool {
	return strings.HasPrefix(string(b), p)
}
