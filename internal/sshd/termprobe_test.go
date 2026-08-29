package sshd

import (
	"testing"
	"time"
)

func TestDADecision(t *testing.T) {
	for _, tc := range []struct {
		da2, da3 string
		decided  bool
		trueClr  bool
	}{
		{"", "", false, false},              // silent
		{"", "Apple_Terminal", true, false}, // macOS Terminal.app: no truecolor
		{"", "kitty", true, true},           // DA3 names 24-bit terminals
		{"", "wezterm", true, true},
		{"", "6954726D", true, true},         // iTerm2 sends its name hex-encoded
		{"", "someUnknownEmu", false, false}, // unknown family: no decision
		{">65;58021;0", "", true, true},      // VTE (gnome-terminal)
		{">84;0;0", "", true, true},          // tmux quantizes to outer
		{">115;30020;0", "", true, true},     // Konsole
		{">41;330;0", "", true, false},       // pre-2020 xterm: no truecolor
		{">41;377;0", "", true, true},        // xterm >= patch 362
		{">1;10;0", "Apple_Terminal", true, false},
		{">1;10;0", "", false, false}, // Apple's DA2 alone is not in the table
		{"garbage", "", false, false}, // malformed
	} {
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
	p := newTermProbe()

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
			tclient.authNone()
			tclient.openSession(1<<20, 32768)
			tclient.ptyReq(80, 24) // consumes the query
			tclient.shell()
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
	tclient.authNone()
	tclient.openSession(1<<20, 32768)
	tclient.ptyReq(80, 24) // TERM=xterm-256color: ambiguous, needs the probe
	tclient.shell()
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
