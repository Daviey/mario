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
		name string
		cmd  string
		ok   bool
	}{
		{"handshake", "mosh-server new -c -l dave -p 60000:61000", true},
		{"bare", "mosh-server new", true},
		{"with command", "mosh-server new -- mario", true},
		{"verbose", "mosh-server new -v -l dave", true},
		{"other binary", "sh -c mosh-server", false},
		{"not new", "mosh-server kill 1234", false},
		{"unknown flag", "mosh-server new -x", false},
		{"shell metachar", "mosh-server new; rm -rf /", false}, // "new;" fails the exact match; argv is rebuilt anyway
		{"empty", "", false},
	}
	for _, c := range cases {
		if _, ok := parseMoshArgv(c.cmd); ok != c.ok {
			t.Errorf("%s: parseMoshArgv(%q) ok=%v, want %v", c.name, c.cmd, ok, c.ok)
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
