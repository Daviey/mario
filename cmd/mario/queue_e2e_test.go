//go:build linux

package main

// Real-OpenSSH-client queue E2E: with a one-slot server, the second
// player waits in line, and the moment the first quits the second's game
// starts. Run like the other real-client tests:
//
//	SSHE2E=1 go test -run TestServeQueueHandoverE2E -v ./cmd/mario

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type ptyClient struct {
	m        *os.File
	cmd      *exec.Cmd
	mu       sync.Mutex
	out      string
	cond     *sync.Cond
	sawQueue chan struct{}
	sawGame  chan struct{}
}

func (p *ptyClient) Out() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.out
}

// waitFor polls the accumulated output for want.
func (p *ptyClient) waitFor(t *testing.T, want string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(p.Out(), want) {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

func TestServeQueueHandoverE2E(t *testing.T) {
	if os.Getenv("SSHE2E") == "" {
		t.Skip("set SSHE2E=1 to run the real-ssh-client queue test")
	}
	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("no ssh client available")
	}

	bin := t.TempDir() + "/mario"
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	port := freePort(t)
	srv := exec.Command(bin, "-serve", fmt.Sprintf("127.0.0.1:%d", port),
		"-hostkey", t.TempDir()+"/hk", "-maxsessions", "1")
	srv.Stderr = os.Stderr
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		srv.Process.Kill()
		srv.Wait()
	}()
	waitListener(t, port)

	p1 := startPTYSSH(t, sshPath, port)
	defer p1.close()
	p2 := startPTYSSH(t, sshPath, port)
	defer p2.close()

	// Player 1 is in the game (alt screen); player 2 is queued.
	if !p1.waitFor(t, "\x1b[?1049h", 10*time.Second) {
		t.Fatalf("player 1 never entered the game: %q", p1.Out())
	}
	if !p2.waitFor(t, "in line", 10*time.Second) {
		t.Fatalf("player 2 never saw the queue screen: %q", p2.Out())
	}
	if !strings.Contains(p2.Out(), "Estimated wait") {
		t.Errorf("queue screen missing ETA: %q", p2.Out())
	}

	// Player 1 quits; the slot hands over and player 2's game begins.
	writeAll(p1.m, "q")
	if !p2.waitFor(t, "\x1b[?1049h", 5*time.Second) {
		t.Fatalf("player 2 never took over the freed slot: %q", p2.Out())
	}
	writeAll(p2.m, "q")
	if !p2.waitFor(t, "\x1b[?1049l", 5*time.Second) {
		t.Fatalf("player 2 session did not tear down: %q", p2.Out())
	}
}

func startPTYSSH(t *testing.T, sshPath string, port int) *ptyClient {
	m, s, err := openRawPTY(120, 32)
	if err != nil {
		t.Fatalf("pty: %v", err)
	}
	cmd := exec.Command(sshPath, "-tt", "-p", fmt.Sprint(port),
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
		"player@127.0.0.1")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = s, s, os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		t.Fatalf("ssh: %v", err)
	}
	p := &ptyClient{m: m, cmd: cmd}
	p.cond = sync.NewCond(&p.mu)
	go func() {
		buf := make([]byte, 16*1024)
		for {
			m.SetReadDeadline(time.Now().Add(30 * time.Second))
			nr, err := m.Read(buf)
			if nr > 0 {
				p.mu.Lock()
				p.out += string(buf[:nr])
				p.mu.Unlock()
				p.cond.Broadcast()
			}
			if err != nil {
				return
			}
		}
	}()
	return p
}

func (p *ptyClient) close() {
	p.cmd.Process.Kill()
	p.cmd.Wait()
	p.m.Close()
}

// freePort/waitListener/writeAll/openRawPTY come from the load test file.
