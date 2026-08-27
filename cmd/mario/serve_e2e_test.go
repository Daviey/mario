package main

// E2E against the real OpenSSH client, gated like the LIVE tests: the
// in-package protocol tests cross-check framing against a second
// implementation, but only the real client catches interop slips (e.g.
// sequence-number semantics at NEWKEYS, missing reply fields).
//
//	SSHE2E=1 go test -run TestServeSSHClientE2E -v ./cmd/mario

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestServeSSHClientE2E(t *testing.T) {
	if os.Getenv("SSHE2E") == "" {
		t.Skip("set SSHE2E=1 to run the real-ssh-client end-to-end test")
	}
	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("no ssh client available")
	}

	bin := t.TempDir() + "/mario"
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	// Pick a free port, then hand it to the server.
	probe, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := probe.Addr().(*net.TCPAddr).Port
	probe.Close()

	srv := exec.Command(bin, "-serve", fmt.Sprintf("127.0.0.1:%d", port),
		"-hostkey", t.TempDir()+"/hk")
	srv.Stderr = os.Stderr
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		srv.Process.Kill()
		srv.Wait()
	}()

	// Wait for the listener.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			c.Close()
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// Scripted session: wait for the title, press a game key, then quit.
	// Stdin must be a pipe — a bytes.Buffer would EOF before the delayed
	// key writes land.
	stdin, stdinW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		defer stdinW.Close()
		time.Sleep(1200 * time.Millisecond)
		stdinW.Write([]byte(" "))
		time.Sleep(400 * time.Millisecond)
		stdinW.Write([]byte("q"))
	}()

	var out bytes.Buffer
	cmd := exec.Command(sshPath, "-tt", "-p", fmt.Sprint(port),
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
		"player@127.0.0.1")
	cmd.Stdin = stdin
	cmd.Stdout = &out
	cmd.Stderr = os.Stderr
	done := make(chan error, 1)
	go func() { done <- cmd.Run() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ssh exited with error: %v", err)
		}
	case <-time.After(15 * time.Second):
		cmd.Process.Kill()
		t.Fatal("ssh session did not finish")
	}

	got := out.String()
	// The terminal prologue/epilogue the game's loop writes: alt screen
	// in, kitty keyboard push, and the matching restore on the way out.
	for _, marker := range []string{
		"\x1b[?1049h", // enter alt screen
		"\x1b[>11u",   // kitty keyboard flags push
		"\x1b[?1049l", // leave alt screen (cleanup ran)
	} {
		if !strings.Contains(got, marker) {
			t.Errorf("session output missing %q (len=%d)", marker, len(got))
		}
	}
	if len(got) < 10_000 {
		t.Fatalf("suspiciously little game output (%d bytes)", len(got))
	}
}
