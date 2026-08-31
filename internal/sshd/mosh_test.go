package sshd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestParseMoshArgv(t *testing.T) {
	cases := []struct {
		name   string
		cmd    string
		ok     bool
		colors bool
	}{
		{"handshake", "mosh-server new -c -l dave -p 60000:61000", true, true},
		{"bare", "mosh-server new", true, false},
		{"colors without value", "mosh-server new -c", true, true},
		{"with command", "mosh-server new -- mario", true, false},
		{"dashdash alone", "mosh-server new --", true, false},
		{"flag-looking value", "mosh-server new -l -p 60000:61000", true, false},
		{"verbose", "mosh-server new -v -l dave", true, false},
		{"other binary", "sh -c mosh-server", false, false},
		{"not new", "mosh-server kill 1234", false, false},
		{"unknown flag", "mosh-server new -x", false, false},
		{"shell metachar", "mosh-server new; rm -rf /", false, false}, // "new;" fails the exact match; argv is rebuilt anyway
		{"empty", "", false, false},
	}
	for _, c := range cases {
		req, ok := parseMoshArgv(c.cmd)
		if ok != c.ok {
			t.Errorf("%s: parseMoshArgv(%q) ok=%v, want %v", c.name, c.cmd, ok, c.ok)
			continue
		}
		if ok && req.colors != c.colors {
			t.Errorf("%s: colors=%v, want %v (a -c without its value word still counts)", c.name, req.colors, c.colors)
		}
	}
}

// stubMoshServer writes a fake mosh-server that prints the CONNECT line,
// records its pid, and stays alive — the shape the real one has during
// and long after the SSH handshake.
func stubMoshServer(t *testing.T) (bin string, pidfile string) {
	t.Helper()
	dir := t.TempDir()
	pidfile = filepath.Join(dir, "pid")
	bin = filepath.Join(dir, "mosh-server")
	script := "#!/bin/sh\n" +
		"printf '\\n\\nMOSH CONNECT 60000 c3R1YmtleQ==\\n\\n'\n" +
		"echo $$ > " + pidfile + "\n" +
		"sleep 30\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return bin, pidfile
}

func TestMoshHandshakeSpawnsAndSurvivesTeardown(t *testing.T) {
	bin, pidfile := stubMoshServer(t)
	srv := startServer(t, echoHandler, func(s *Server) {
		s.MoshBin = bin
		s.MoshPortRange = "60000:60100"
	})
	tc := dial(t, srv.Addr)
	tc.authNone()
	tc.openSession(1<<20, 32768)
	tc.ptyReq(80, 24)

	w := &buf{}
	w.u8(msgChannelRequest)
	w.u32(0)
	w.cstr("exec")
	w.boolean(true)
	w.cstr("mosh-server new -c -l whoever -p 12345")
	tc.send(w.b)
	tc.expect(msgChannelSuccess)

	// The CONNECT line arrives over CHANNEL_DATA (possibly split).
	got := ""
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(got, "MOSH CONNECT 60000") {
		if time.Now().After(deadline) {
			t.Fatalf("no MOSH CONNECT line, got %q", got)
		}
		got += string(tc.readData())
	}

	// Client tears the SSH connection down once it has the key…
	if err := tc.nc.Close(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(700 * time.Millisecond)

	// …but mosh-server must still be alive (mosh's whole design).
	pidB, err := os.ReadFile(pidfile)
	if err != nil {
		t.Fatalf("stub never wrote its pid: %v", err)
	}
	pid := 0
	fmt.Sscanf(strings.TrimSpace(string(pidB)), "%d", &pid)
	if pid == 0 {
		t.Fatal("bad pid from stub")
	}
	if err := syscall.Kill(pid, 0); err != nil {
		t.Fatalf("mosh-server died with the SSH connection: %v", err)
	}
	t.Cleanup(func() { syscall.Kill(pid, syscall.SIGKILL) })

	// And the counter reflects the live session.
	if n := RunningMosh(); n < 1 {
		t.Fatalf("RunningMosh() = %d, want >= 1", n)
	}
}

// moshExec sends one mosh-shaped exec request on tc.
func moshExec(tc *testClient, cmdline string) {
	w := &buf{}
	w.u8(msgChannelRequest)
	w.u32(0)
	w.cstr("exec")
	w.boolean(true)
	w.cstr(cmdline)
	tc.send(w.b)
}

// The MoshMax cap is a hard limit: with one slot, the second child's
// handshake is refused outright (CHANNEL_FAILURE, before the exec
// request is ever answered positively) while the first keeps running.
func TestMoshMaxRefusesSecondChild(t *testing.T) {
	bin, pidfile := stubMoshServer(t)
	srv := startServer(t, echoHandler, func(s *Server) {
		s.MoshBin = bin
		s.MoshPortRange = "60000:60100"
		s.MoshMax = 1
	})

	// The slot counter is package-global: earlier tests' stubs release
	// asynchronously (reaped at kill), so wait the fleet out first.
	deadline := time.Now().Add(5 * time.Second)
	for RunningMosh() > 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	// First connection: the handshake succeeds and spawns the stub.
	a := dial(t, srv.Addr)
	a.authNone()
	a.openSession(1<<20, 32768)
	a.ptyReq(80, 24)
	moshExec(a, "mosh-server new -c -l whoever -p 12345")
	a.expect(msgChannelSuccess)
	got := ""
	readDeadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(got, "MOSH CONNECT 60000") {
		if time.Now().After(readDeadline) {
			t.Fatalf("no MOSH CONNECT line, got %q", got)
		}
		got += string(a.readData())
	}
	pidB, err := os.ReadFile(pidfile)
	for err != nil && time.Now().Before(readDeadline) {
		// The stub writes its pid just after the CONNECT line: the
		// channel can beat the file by a beat.
		time.Sleep(10 * time.Millisecond)
		pidB, err = os.ReadFile(pidfile)
	}
	if err != nil {
		t.Fatalf("stub never wrote its pid: %v", err)
	}
	pid := 0
	fmt.Sscanf(strings.TrimSpace(string(pidB)), "%d", &pid)
	t.Cleanup(func() { syscall.Kill(pid, syscall.SIGKILL) })

	// Second connection, second handshake: refused at the cap. The
	// refusal rides the reply lane, so it is observable even while the
	// first connection holds its slot.
	b := dial(t, srv.Addr)
	b.authNone()
	b.openSession(1<<20, 32768)
	b.ptyReq(80, 24)
	moshExec(b, "mosh-server new -c -l whoever -p 12345")
	b.expect(msgChannelFailure)

	// And a retry once the slot frees would work again — but here just
	// pin the accounting: one child live, no leaked claims.
	if n := RunningMosh(); n != 1 {
		t.Fatalf("RunningMosh() = %d, want exactly 1 (cap 1, one live child)", n)
	}
}

func TestMoshDisabledRefusesHandshake(t *testing.T) {
	// No MoshBin configured: even a perfectly shaped handshake is
	// refused exactly like any other exec.
	srv := startServer(t, echoHandler)
	tc := dial(t, srv.Addr)
	tc.authNone()
	tc.openSession(1<<20, 32768)

	w := &buf{}
	w.u8(msgChannelRequest)
	w.u32(0)
	w.cstr("exec")
	w.boolean(true)
	w.cstr("mosh-server new -c")
	tc.send(w.b)
	tc.expect(msgChannelFailure)
}

// The env handed to mosh-server decides the game's color mode: mosh
// overwrites the child TERM to xterm-256color and never forwards
// COLORTERM, so the one signal that survives is the COLORTERM we set
// here. colorTerm arrives fully decided (channel.decideColorTerm).
func TestMoshEnv(t *testing.T) {
	inherited := []string{
		"PATH=/usr/bin",
		"TERM=dumb",        // must never shadow the appended TERM
		"COLORTERM=junk",   // host-side leak; the client decides
		"LANG=en_GB.UTF-8", // mosh-server needs our forced locale
		"LC_NUMERIC=C",     // ditto, any LC_*
		"HOME=/home/mario", // untouched
	}

	// A decided truecolor client: the game runs 24-bit color.
	env := moshEnv(inherited, "xterm-256color", "truecolor")
	if got := termOf(env); got != "xterm-256color" {
		t.Errorf("TERM = %q, want the client's (exactly once)", got)
	}
	if got := colorTermOf(env); got != "truecolor" {
		t.Errorf("COLORTERM = %q, want the decided truecolor", got)
	}
	if !hasEnv(env, "HOME=/home/mario") || hasEnv(env, "LC_NUMERIC=C") {
		t.Errorf("unexpected inherited-env handling: %v", env)
	}

	// Undecided (plain 256-color client, silent probe): 16-color — a
	// genuinely 256-only terminal given 38;2 loses all color.
	env = moshEnv(inherited, "xterm-256color", "")
	if got := colorTermOf(env); got != "" {
		t.Errorf("COLORTERM = %q for undecided client, want none", got)
	}

	// An env-requested COLORTERM (ssh -o SendEnv=COLORTERM) passes
	// through verbatim.
	env = moshEnv(inherited, "xterm-256color", "24bit")
	if got := colorTermOf(env); got != "24bit" {
		t.Errorf("COLORTERM = %q, want the forwarded value", got)
	}
}

// decideColorTerm resolves the session's color depth: env request >
// TERM family > probe. The probe paths are covered in
// termprobe_test.go; this is the TERM/env precedence without a probe.
func TestDecideColorTerm(t *testing.T) {
	ch := &channel{term: "xterm-256color"}
	if got := ch.decideColorTerm(); got != "" {
		t.Errorf("plain 256color TERM = %q, want none", got)
	}
	ch = &channel{term: "xterm-ghostty"}
	if got := ch.decideColorTerm(); got != "truecolor" {
		t.Errorf("ghostty TERM = %q, want truecolor", got)
	}
	ch = &channel{term: "xterm-256color", env: map[string]string{"COLORTERM": "truecolor"}}
	if got := ch.decideColorTerm(); got != "truecolor" {
		t.Errorf("forwarded COLORTERM = %q, want truecolor", got)
	}
}

func termOf(env []string) (got string) {
	for _, kv := range env {
		if strings.HasPrefix(kv, "TERM=") {
			if got != "" {
				return "duplicate!"
			}
			got = kv[len("TERM="):]
		}
	}
	return
}

func colorTermOf(env []string) string {
	for _, kv := range env {
		if strings.HasPrefix(kv, "COLORTERM=") {
			return kv[len("COLORTERM="):]
		}
	}
	return ""
}

func hasEnv(env []string, want string) bool {
	for _, kv := range env {
		if kv == want {
			return true
		}
	}
	return false
}
