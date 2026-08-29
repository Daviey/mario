//go:build linux

package main

// SSH capacity benchmark: how many concurrent players can one -serve
// process sustain before frame output cadence degrades? Runs the real
// server binary and real OpenSSH clients, each inside a pty sized like a
// desktop terminal (200x31 — the default 80x24 would flatter the numbers
// ~3x since render cost scales with cells). Saturation is measured
// client-side as dirty-frame output rate drooping below its single-client
// baseline; server CPU comes from /proc/<pid>/stat so client cost is
// excluded.
//
//	LOAD=1 go test -run TestServeLoadCapacity -v ./cmd/mario
//	LOAD=1 LOAD_N=32 LOAD_SECS=10 go test -run TestServeLoadCapacity -v ./cmd/mario
//
// Sweep N with LOAD_N; compare fps against the LOAD_N=1 baseline run.
//
// Remote sweep (the serving box runs the server, the client box runs
// only this test via LOAD_SERVER): copy the binary to the box and start
// an instrumented instance bound to a management-only IP with the cap
// raised out of the way — e.g.
//
//	nohup ./mario-load -serve 198.51.100.10:2200 -hostkey /tmp/hk \
//		-maxsessions 400 >/tmp/log 2>&1 </dev/null &
//
// (redirect stdin too, or the ssh that started it never exits), then
// from the client box run the same command with
// LOAD_SERVER=198.51.100.10:2200. Per-run server CPU is measured on the
// box itself: snapshot `awk '{print $14+$15}' /proc/$(pgrep -x
// mario-load)/stat` before and after each run — jiffies divided by
// 100*LOAD_SECS is cores (pgrep -x; -f matches wrappers). Kill the
// instance and delete the binary afterwards. Keep LOAD_SECS identical
// across a sweep: the fps metric depends on the game content on screen.

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestServeLoadCapacity(t *testing.T) {
	if os.Getenv("LOAD") == "" {
		t.Skip("set LOAD=1 to run the SSH capacity benchmark")
	}
	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("no ssh client available")
	}
	n := envInt("LOAD_N", 16)
	secs := envInt("LOAD_SECS", 8)
	const cols, rows = 200, 31 // desktop-ish; 33x9-tile viewport

	// Split mode: LOAD_SERVER=host:port runs only the clients against an
	// already-running server (e.g. on the VPS, reachable over private network) —
	// the server's CPU is then measured on its own box, without client
	// processes competing on the same machine.
	host := "127.0.0.1"
	var srv *exec.Cmd
	var port int
	if addr := os.Getenv("LOAD_SERVER"); addr != "" {
		h, p, err := net.SplitHostPort(addr)
		if err != nil {
			t.Fatalf("LOAD_SERVER: %v", err)
		}
		host = h
		fmt.Sscanf(p, "%d", &port)
		waitListenerHost(t, host, port)
	} else {
		bin := t.TempDir() + "/mario"
		if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
			t.Fatalf("build: %v\n%s", err, out)
		}
		port = freePort(t)
		srv = exec.Command(bin, "-serve", fmt.Sprintf("127.0.0.1:%d", port),
			"-hostkey", t.TempDir()+"/hk", "-maxsessions", fmt.Sprint(n+8))
		srv.Stderr = os.Stderr
		if err := srv.Start(); err != nil {
			t.Fatal(err)
		}
		defer func() {
			srv.Process.Kill()
			srv.Wait()
		}()
		waitListener(t, port)
	}

	var cpu0, cpu1 float64
	var rss int64
	if srv != nil {
		cpu0, _ = procCPU(t, srv.Process.Pid)
	}
	start := time.Now()

	clients := make([]loadClient, n)
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			runLoadClient(t, sshPath, host, port, cols, rows, secs, i*37%400, &clients[i])
		}(i)
	}
	wg.Wait()

	if srv != nil {
		cpu1, rss = procCPU(t, srv.Process.Pid)
	}
	elapsed := time.Since(start).Seconds()
	measSecs := float64(secs) - 3 // post-warmup window

	var fps []float64
	var bps float64
	var gaps []float64
	for i := range n {
		fps = append(fps, float64(clients[i].frames.Load())/measSecs)
		bps += float64(clients[i].bytes.Load()) / measSecs
		for _, g := range clients[i].gaps {
			gaps = append(gaps, g.Seconds())
		}
	}
	sort.Float64s(fps)
	p95Gap := 0.0
	if len(gaps) > 0 {
		sort.Float64s(gaps)
		p95Gap = gaps[len(gaps)*95/100]
	}
	cores := (cpu1 - cpu0) / elapsed
	cpuNote := fmt.Sprintf("server CPU=%.2f cores (%.1fms/session/s) RSS=%dMiB", cores, cores*1000/float64(n), rss>>20)
	if srv == nil {
		cpuNote = "server CPU=n/a (remote; measure on its box)"
	}
	t.Logf("N=%d  view=%dx%d  fps: min=%.1f med=%.1f max=%.1f | p95 frame gap=%.0fms | out=%.0f KiB/s | %s",
		n, cols, rows, fps[0], fps[len(fps)/2], fps[len(fps)-1], p95Gap*1000, bps/1024, cpuNote)
}

// loadClient accumulates one player's output metrics.
type loadClient struct {
	frames atomic.Int64
	bytes  atomic.Int64
	gaps   []time.Duration // inter-frame gaps in the measurement window
}

// runLoadClient drives one scripted player through a real ssh client in a
// pty: start the game, hold right, hop; then quit and record output.
func runLoadClient(t *testing.T, sshPath, host string, port, cols, rows, secs, offsetMs int, c *loadClient) {
	m, s, err := openRawPTY(cols, rows)
	if err != nil {
		t.Errorf("pty: %v", err)
		return
	}
	defer m.Close()
	cmd := exec.Command(sshPath, "-tt", "-p", fmt.Sprint(port),
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
		"-o", "ConnectTimeout=10",
		"player@"+host)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = s, s, os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true, Setctty: true, Ctty: 0}
	if err := cmd.Start(); err != nil {
		t.Errorf("ssh: %v", err)
		s.Close()
		return
	}
	defer func() {
		s.Close()
		cmd.Wait()
	}()
	time.Sleep(time.Duration(offsetMs) * time.Millisecond)

	// Input driver: dismiss title, skip world card, hold right (the
	// mapper's legacy decay needs re-presses), then quit — playSession
	// tears the channel down and ssh exits.
	// Session-live signal: the first bytes prove the game is up, so the
	// scripted input never races a slow handshake (under load the ssh
	// handshake can take seconds; keys sent before the session exists are
	// lost and the player idles on the title screen).
	live := make(chan struct{})
	var once sync.Once
	done := make(chan struct{})
	go func() {
		defer close(done)
		select {
		case <-live:
		case <-time.After(10 * time.Second):
			return // never came up; reader will time out
		}
		sleepMs(800)
		writeAll(m, "\r")
		sleepMs(400)
		writeAll(m, "\r")
		deadline := time.Now().Add(time.Duration(secs) * time.Second)
		for time.Now().Before(deadline) {
			writeAll(m, "d")
			sleepMs(90)
		}
		writeAll(m, "q")
	}()

	// Output counter: frame = one synchronized-output region; only the
	// post-warmup window counts.
	warm := time.Now().Add(3 * time.Second)
	var carry []byte
	var lastFrame time.Time
	buf := make([]byte, 32*1024)
	for {
		m.SetReadDeadline(time.Now().Add(10 * time.Second))
		nr, rerr := m.Read(buf)
		now := time.Now()
		if nr > 0 {
			once.Do(func() { close(live) })
			chunk := append(carry, buf[:nr]...)
			if len(chunk) > 7 {
				carry = append(carry[:0], chunk[len(chunk)-7:]...)
			} else {
				carry = append(carry[:0], chunk...)
			}
			if now.After(warm) {
				c.bytes.Add(int64(nr))
				for range bytes.Count(chunk, []byte("\x1b[?2026h")) {
					c.frames.Add(1)
					if !lastFrame.IsZero() {
						c.gaps = append(c.gaps, now.Sub(lastFrame))
					}
					lastFrame = now
				}
			}
		}
		if rerr != nil {
			break
		}
	}
	<-done
}

func writeAll(f *os.File, s string) {
	for len(s) > 0 {
		n, err := f.WriteString(s)
		if err != nil {
			return
		}
		s = s[n:]
	}
}

func sleepMs(ms int) { time.Sleep(time.Duration(ms) * time.Millisecond) }

func envInt(name string, def int) int {
	if v := os.Getenv(name); v != "" {
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil && n > 0 {
			return n
		}
	}
	return def
}

func freePort(t *testing.T) int {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port
}

func waitListenerHost(t *testing.T, host string, port int) {
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.Dial("tcp", net.JoinHostPort(host, fmt.Sprint(port)))
		if err == nil {
			c.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("server at %s:%d never came up", host, port)
}

func waitListener(t *testing.T, port int) {
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			c.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("server listener never came up")
}

// procCPU returns cumulative CPU seconds and RSS bytes of a pid.
func procCPU(t *testing.T, pid int) (float64, int64) {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		t.Fatalf("proc stat: %v", err)
	}
	// Skip past the comm field (may contain spaces in parens); fields[0]
	// is then the process state (stat field 3), so utime/stime are [11]/[12].
	fields := strings.Fields(string(b[bytes.LastIndexByte(b, ')')+2:]))
	const hz = 100
	var u, s int
	fmt.Sscanf(fields[11], "%d", &u)
	fmt.Sscanf(fields[12], "%d", &s)
	var rss int64
	if rb, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid)); err == nil {
		for _, line := range strings.Split(string(rb), "\n") {
			var kb int64
			if strings.HasPrefix(line, "VmRSS:") {
				fmt.Sscanf(strings.TrimPrefix(line, "VmRSS:"), "%d", &kb)
				rss = kb * 1024
			}
		}
	}
	return (float64(u) + float64(s)) / hz, rss
}

// openRawPTY creates a pty pair with the slave in raw mode (no echo, no
// output post-processing) at the given window size. The master is opened
// non-blocking so os.NewFile registers it with the runtime poller —
// SetReadDeadline silently no-ops otherwise, and a stopped stream then
// hangs the reader forever.
func openRawPTY(cols, rows int) (m, s *os.File, err error) {
	fd, err := syscall.Open("/dev/ptmx", syscall.O_RDWR|syscall.O_NOCTTY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, err
	}
	m = os.NewFile(uintptr(fd), "/dev/ptmx")
	defer func() {
		if err != nil {
			m.Close()
		}
	}()
	var n uint32
	if err = ioctlPtr(m.Fd(), syscall.TIOCSPTLCK, unsafe.Pointer(&n)); err != nil {
		return nil, nil, err
	}
	var sn uint32
	if err = ioctlPtr(m.Fd(), syscall.TIOCGPTN, unsafe.Pointer(&sn)); err != nil {
		return nil, nil, err
	}
	s, err = os.OpenFile(fmt.Sprintf("/dev/pts/%d", sn), os.O_RDWR|syscall.O_NOCTTY, 0)
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if err != nil {
			s.Close()
		}
	}()
	ws := struct{ row, col, x, y uint16 }{uint16(rows), uint16(cols), 0, 0}
	if err = ioctlPtr(s.Fd(), syscall.TIOCSWINSZ, unsafe.Pointer(&ws)); err != nil {
		return nil, nil, err
	}
	// Raw-ish slave: kill ECHO (our input would loop back into the output
	// metrics) and OPOST (no \n -> \r\n rewriting of game output).
	var tio syscall.Termios
	if err = ioctlPtr(s.Fd(), syscall.TCGETS, unsafe.Pointer(&tio)); err != nil {
		return nil, nil, err
	}
	tio.Lflag &^= syscall.ECHO | syscall.ICANON
	tio.Oflag &^= syscall.OPOST
	if err = ioctlPtr(s.Fd(), syscall.TCSETS, unsafe.Pointer(&tio)); err != nil {
		return nil, nil, err
	}
	return m, s, nil
}

func ioctlPtr(fd, req uintptr, arg unsafe.Pointer) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, req, uintptr(arg))
	if errno != 0 {
		return errno
	}
	return nil
}
