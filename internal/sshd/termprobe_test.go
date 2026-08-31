package sshd

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Daviey/mario/render"
	"time"
)

func TestDADecision(t *testing.T) {
	type daCase struct {
		da2, da3 string
		decided  bool
		trueClr  bool
	}
	// Every table row is itself a case: the tables in termprobe.go are
	// the source of truth, the assertions below cannot drift from them.
	var cases []daCase
	for _, f := range da3Families {
		cases = append(cases, daCase{"", f.name, true, f.trueColor})
	}
	for _, f := range da2Families {
		cases = append(cases, daCase{f.prefix + "58021;0", "", true, f.trueColor})
	}
	cases = append(cases,
		daCase{"", "", false, false},               // silent
		daCase{"", "someUnknownEmu", false, false}, // unknown family: no decision
		daCase{">41;330;0", "", true, false},       // pre-2020 xterm: no truecolor
		daCase{">41;377;0", "", true, true},        // xterm >= patch 362
		daCase{">1;10;0", "Apple_Terminal", true, false},
		daCase{">1;10;0", "", false, false}, // Apple's DA2 alone is not in the table
		daCase{"garbage", "", false, false}, // malformed
	)
	for _, tc := range cases {
		decided, trueClr := daDecision(tc.da2, tc.da3)
		if decided != tc.decided || trueClr != tc.trueClr {
			t.Errorf("daDecision(%q, %q) = (%v, %v), want (%v, %v)",
				tc.da2, tc.da3, decided, trueClr, tc.decided, tc.trueClr)
		}
	}
}

// The probe must preserve player keystrokes around replies, strip
// replies split across packets, and stop buffering once drained —
// late replies must not leak into the game's input stream.
func TestTermProbeOfferDrainFilter(t *testing.T) {
	p := newTermProbe(time.Minute)

	// Keystrokes before any reply: buffered.
	if _, buffered := p.offer([]byte("ab")); !buffered {
		t.Fatal("pre-reply offer not buffered")
	}
	// Reply split across two packets amid keystrokes.
	if _, buffered := p.offer([]byte("\x1b[>6")); !buffered {
		t.Fatal("partial offer not buffered")
	}
	rest, buffered := p.offer([]byte("5;1;0ccdef"))
	if !buffered {
		t.Fatal("completed-reply offer not buffered")
	}
	if decided, tc := p.result(0); !decided || !tc {
		t.Fatalf("VTE reply split across packets: decided=%v truecolor=%v", decided, tc)
	}
	// Drain replays the keystrokes, reply stripped.
	if got := string(p.drain()); got != "abcdef" {
		t.Fatalf("drained %q, want %q", got, "abcdef")
	}
	// Post-drain: filter mode. A late reply is stripped, input passes.
	rest, buffered = p.offer([]byte("g\x1b[>1;10;0ch"))
	if buffered {
		t.Fatal("post-drain offer still buffering")
	}
	if got := string(rest); got != "gh" {
		t.Fatalf("filtered passthrough %q, want %q", got, "gh")
	}
	// DCS-form reply with BEL terminator, post-drain.
	rest, _ = p.offer([]byte("\x1bP!|Apple_Terminal\aij"))
	if got := string(rest); got != "ij" {
		t.Fatalf("BEL-terminated DCS passthrough %q, want %q", got, "ij")
	}
}

// DCS-form (DA3) replies: ST-terminated amid keystrokes in buffering
// mode, reassembly across a split landing exactly at the DCS
// header/name boundary, and — the sharp edge — a SECOND reply after
// the decision, which must strip cleanly without a second close of
// the known channel (a panic pre-guard) or a changed verdict.
func TestTermProbeDCSSplitAndSecondReply(t *testing.T) {
	// ST-terminated DCS amid keystrokes, buffering mode.
	p := newTermProbe(time.Minute)
	if _, buffered := p.offer([]byte("a\x1bP!|kitty\x1b\\b")); !buffered {
		t.Fatal("DCS offer not buffered")
	}
	if decided, ok := p.result(0); !decided || !ok {
		t.Fatal("kitty DA3 did not decide truecolor")
	}
	if got := string(p.drain()); got != "ab" {
		t.Fatalf("drained %q, want %q with the DCS stripped", got, "ab")
	}

	// Split exactly at the DCS header/name boundary: the complete
	// header ("\x1bP!|") is held, the name arrives later.
	p2 := newTermProbe(time.Minute)
	if _, buffered := p2.offer([]byte("x\x1bP!|")); !buffered {
		t.Fatal("partial DCS header not buffered")
	}
	if _, buffered := p2.offer([]byte("wezterm\x1b\\y")); !buffered {
		t.Fatal("DCS completion not buffered")
	}
	if decided, ok := p2.result(0); !decided || !ok {
		t.Fatal("wezterm DA3 split across offers did not decide truecolor")
	}
	if got := string(p2.drain()); got != "xy" {
		t.Fatalf("drained %q, want %q", got, "xy")
	}

	// A second DA2 reply after the decision: stripped, no panic, and
	// the verdict cannot flip.
	rest, _ := p2.offer([]byte("z\x1b[>65;1;0cw"))
	if got := string(rest); got != "zw" {
		t.Fatalf("post-decision DA2 strip = %q, want %q", got, "zw")
	}
	// A second, contradicting DCS reply after the decision: ditto.
	rest, _ = p2.offer([]byte("\x1bP!|Apple_Terminal\x1b\\v"))
	if got := string(rest); got != "v" {
		t.Fatalf("post-decision DCS strip = %q, want %q", got, "v")
	}
	if decided, ok := p2.result(0); !decided || !ok {
		t.Fatal("verdict changed by a second reply")
	}
}

// End to end: the query goes out at pty-req, the client's reply sets
// the session's color depth, and keystrokes typed during the probe
// window survive into the feed.
func TestTrueColorProbeSession(t *testing.T) {
	for _, tc := range []struct {
		name  string
		reply string
		want  bool
	}{
		{"vte da2", "\x1b[>65;58021;0c", true},
		{"apple da3", "\x1bP!|Apple_Terminal\x1b\\", false},
		{"tmux da2", "\x1b[>84;0;0c", true},
		{"old xterm da2", "\x1b[>41;330;0c", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			trueColor := make(chan bool, 1)
			fed := make(chan string, 4)
			srv := startServer(t, func(s *Session) {
				trueColor <- s.TrueColor()
				s.OnFeed(func(b []byte) { fed <- string(b) })
				<-s.Done()
			})
			tclient := dial(t, srv.Addr)
			tclient.playPrologue() // pty consumes the query
			// The handler is now blocked in TrueColor waiting for the
			// probe; the player types around the reply, and both the
			// keystrokes and the decision must survive.
			tclient.sendData([]byte("a" + tc.reply + "b"))
			select {
			case got := <-trueColor:
				if got != tc.want {
					t.Fatalf("TrueColor = %v, want %v", got, tc.want)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("no TrueColor decision")
			}
			select {
			case got := <-fed:
				if got != "ab" {
					t.Fatalf("fed %q, want %q (reply must be stripped)", got, "ab")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("buffered keystrokes never reached the feed")
			}
			// A reply arriving after the drain must not leak as input.
			tclient.sendData([]byte(tc.reply + "c"))
			select {
			case got := <-fed:
				if got != "c" {
					t.Fatalf("post-drain fed %q, want %q", got, "c")
				}
			case <-time.After(5 * time.Second):
				t.Fatal("post-drain input never reached the feed")
			}
		})
	}
}

// A terminal that never answers: the decision falls back to the TERM
// rules after the (shortened) wait instead of hanging the session.
func TestTrueColorProbeSilentFallsBackToTerm(t *testing.T) {
	trueColor := make(chan bool, 1)
	srv := startServer(t, func(s *Session) {
		trueColor <- s.TrueColor()
		<-s.Done()
	}, func(s *Server) { s.ProbeWait = 20 * time.Millisecond })
	tclient := dial(t, srv.Addr)
	tclient.playPrologue() // TERM=xterm-256color: ambiguous, needs the probe
	start := time.Now()
	select {
	case got := <-trueColor:
		if got {
			t.Fatal("silent xterm-256color terminal decided truecolor")
		}
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Fatalf("fallback took %v, ProbeWait not honored", elapsed)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no decision for silent terminal")
	}
}

// ColorDepth layers the 256-color cube under the truecolor decision: a
// silent terminal whose TERM advertises 256 colors gets the fixed cube
// (not the profile-dependent base 16 — the gnome-terminal-over-mosh
// case), a TERM with no color claim stays base-16, and a forwarded
// COLORTERM or a truecolor TERM family wins outright.
func TestSessionColorDepth(t *testing.T) {
	for _, tc := range []struct {
		name string
		term string
		env  [2]string
		want render.ColorMode
	}{
		{"silent 256color", "xterm-256color", [2]string{"", ""}, render.Colors256},
		{"plain xterm", "xterm", [2]string{"", ""}, render.Colors16},
		{"forwarded colorterm", "xterm-256color", [2]string{"COLORTERM", "truecolor"}, render.Colors24},
		{"ghostty family", "xterm-ghostty", [2]string{"", ""}, render.Colors24},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := make(chan render.ColorMode, 1)
			srv := startServer(t, func(s *Session) {
				got <- s.ColorDepth()
				<-s.Done()
			}, func(s *Server) { s.ProbeWait = 20 * time.Millisecond })
			tclient := dial(t, srv.Addr)
			tclient.authNone()
			tclient.openSession(1<<20, 32768)
			tclient.ptyReqTerm(80, 24, tc.term)
			if tc.env[0] != "" {
				tclient.envReq(tc.env[0], tc.env[1])
			}
			tclient.shell()
			select {
			case d := <-got:
				if d != tc.want {
					t.Fatalf("ColorDepth = %d, want %d", d, tc.want)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("no ColorDepth decision")
			}
		})
	}
}

// A reply that never terminates ("\x1b[>" with no "c") used to keep
// the probe buffering every later byte forever — unbounded memory and
// swallowed input. The hold is now bounded by probeBufMax and by the
// probe's wait window: whichever trips first abandons probing and
// flushes everything back.
func TestTermProbeTruncatedEscapeRecovers(t *testing.T) {
	// Overflow: past probeBufMax buffered bytes the probe gives up.
	p := newTermProbe(time.Minute)
	if _, buffered := p.offer([]byte("\x1b[>")); !buffered {
		t.Fatal("truncated DA2 not buffered")
	}
	rest, buffered := p.offer(bytes.Repeat([]byte("x"), probeBufMax))
	if buffered {
		t.Fatal("overflow must abandon buffering")
	}
	if got := string(rest); !strings.HasPrefix(got, "\x1b[>") || len(got) != probeBufMax+3 {
		t.Fatalf("overflow flush = %d bytes starting %q, want the held bytes back", len(got), got[:min(6, len(got))])
	}
	// Abandoned: later input passes straight through, nothing held.
	rest, buffered = p.offer([]byte("LEFT"))
	if buffered || string(rest) != "LEFT" {
		t.Fatalf("post-abandon offer = %q buffered=%v, want passthrough", rest, buffered)
	}
	if got := p.drain(); len(got) != 0 {
		t.Fatalf("post-abandon drain = %q, want nothing held", got)
	}

	// Expiry: once the probe's wait window is over, the next offer
	// abandons and flushes rather than holding for a terminator.
	p = newTermProbe(30 * time.Millisecond)
	if _, buffered := p.offer([]byte("\x1b[>q")); !buffered {
		t.Fatal("truncated DA2 not buffered before expiry")
	}
	time.Sleep(80 * time.Millisecond)
	rest, buffered = p.offer([]byte("LATE"))
	if buffered || string(rest) != "\x1b[>qLATE" {
		t.Fatalf("expired offer = %q buffered=%v, want the hold flushed through", rest, buffered)
	}
}
