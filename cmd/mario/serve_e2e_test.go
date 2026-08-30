package main

// E2E against the real OpenSSH client, gated like the LIVE tests: the
// in-package protocol tests cross-check framing against a second
// implementation, but only the real client catches interop slips (e.g.
// sequence-number semantics at NEWKEYS, missing reply fields).
//
//	SSHE2E=1 go test -run TestServeSSHClientE2E -v ./cmd/mario

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Daviey/mario/board"
	"github.com/Daviey/mario/engine"
	"github.com/Daviey/mario/replay"
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
	// ORDER (kitty spec, Quickstart): the keyboard stack is screen-
	// scoped — the prologue pushes only after entering the alt screen,
	// and the epilogue pops while still on it, before the exit. The old
	// prologue pushed on the main screen's stack, so the pop (on the alt
	// screen) was a no-op and kitty mode survived the session — the
	// player's shell then emitted CSI-u garbage (regression, live).
	if !strings.Contains(got, "\x1b[<u\x1b[?25h\x1b[?23t\x1b[?1049l") {
		t.Error("epilogue must pop kitty keyboard flags before leaving the alt screen")
	}
	if !strings.Contains(got, "\x1b[?1049h\x1b[>11u") {
		t.Error("prologue must enter the alt screen before pushing kitty keyboard flags")
	}
	// Wedged-session tripwire, not a bandwidth contract: a healthy
	// scripted session streams ~10 KB (9,982 B stable across 5 runs on
	// biro 2026-08-30; ≥10 KB on the box the old 10_000 floor was
	// calibrated on — the gap is a frame or two of diff, systematic,
	// not noise), while a session that never renders or dies mid-
	// handshake produces only banners + prologue (<1 KB). 8_000 keeps
	// ~20% headroom below the slowest healthy floor and stays an order
	// of magnitude above wedged. The marker assertions above are the
	// real protocol contract.
	if len(got) < 8_000 {
		t.Fatalf("suspiciously little game output (%d bytes)", len(got))
	}
}

// Keypress-to-output latency through the real OpenSSH client on
// loopback: press 'q' once the title is up and time until the teardown
// epilogue appears on the client side. Loopback removes network RTT, so
// the measurement is the server's own contribution: ssh client
// packetisation, our transport, the tick quantisation (≤17ms) and the
// render/write pipeline. A regression here (buffering, a delayed flush,
// a scheduling hiccup between the tick and writer goroutines) is
// exactly what makes remote play feel wrong.
func TestServeSSHClientLatencyE2E(t *testing.T) {
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

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			c.Close()
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	type timed struct {
		at time.Time
		b  []byte
	}
	chunks := make(chan timed, 64)
	stdoutR, stdoutW := io.Pipe()
	stdin, stdinW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := stdoutR.Read(buf)
			if n > 0 {
				chunks <- timed{at: time.Now(), b: append([]byte(nil), buf[:n]...)}
			}
			if err != nil {
				close(chunks)
				return
			}
		}
	}()

	cmd := exec.Command(sshPath, "-tt", "-p", fmt.Sprint(port),
		"-o", "StrictHostKeyChecking=no",
		"-o", "UserKnownHostsFile=/dev/null",
		"-o", "LogLevel=ERROR",
		"player@127.0.0.1")
	cmd.Stdin = stdin
	cmd.Stdout = stdoutW
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer cmd.Process.Kill()

	waitMarker := func(marker string) time.Time {
		t.Helper()
		var acc []byte
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			select {
			case c, ok := <-chunks:
				if !ok {
					t.Fatalf("session output ended before %q", marker)
				}
				acc = append(acc, c.b...)
				if strings.Contains(string(acc), marker) {
					return c.at
				}
				// Markers appear in early output; keep the tail bounded.
				if len(acc) > 1<<20 {
					acc = acc[len(acc)-4096:]
				}
			case <-time.After(time.Until(deadline)):
				t.Fatalf("timed out waiting for %q", marker)
			}
		}
		t.Fatalf("timed out waiting for %q", marker)
		return time.Time{}
	}

	waitMarker("\x1b[?1049h") // title screen is up
	sent := time.Now()
	stdinW.Write([]byte("q"))
	got := waitMarker("\x1b[?1049l") // teardown epilogue

	latency := got.Sub(sent)
	t.Logf("key press -> epilogue on loopback: %v", latency)
	if latency > 120*time.Millisecond {
		t.Fatalf("loopback key-to-output latency %v exceeds 120ms", latency)
	}

	stdinW.Close()
	cmd.Wait()
}

// TestServeDeathRunRecordingE2E drives the real OpenSSH client through
// a run that contains deaths, submits the score against a capture sink
// standing in for Supabase, and replays the submitted recording — the
// leaderboard's verification contract, end to end over the wire.
//
// Regression for the recorder-wipe bug (fixed 2026-08-30): every death
// respawn used to reset the input recording, so the submission carried
// only the final life's segment. The verifier then replayed it to a
// different (smaller) game and deleted the row — live, every
// death-containing run was silently dropped. The old-bug signature this
// test refuses: a recording too short to span the deaths, or a replay
// that does not reproduce the submitted score/level.
//
// Unlike the SSHE2E-gated tests above, this one runs in the default
// suite (CI included): it needs only the ssh binary, which the runner
// image carries, and its ~40s runtime is the price of the only
// coverage that spans ssh transport → input mapper → recorder →
// leaderboard submit → replay verification.
func TestServeDeathRunRecordingE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}
	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("no ssh client available")
	}

	bin := t.TempDir() + "/mario"
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}

	// Capture sink: answers as PostgREST would and keeps every scores
	// insert body for the replay assertions.
	var mu sync.Mutex
	var inserts []board.Entry
	sink := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/rest/v1/scores") {
			body, _ := io.ReadAll(r.Body)
			var e board.Entry
			if json.Unmarshal(body, &e) == nil {
				mu.Lock()
				inserts = append(inserts, e)
				mu.Unlock()
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("[]"))
	}))
	defer sink.Close()

	// One scripted session: title → run (with the demo input pattern,
	// which scores) → suicide the remaining lives → submit → quit.
	session := func() bool {
		probe, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		port := probe.Addr().(*net.TCPAddr).Port
		probe.Close()

		srv := exec.Command(bin, "-serve", fmt.Sprintf("127.0.0.1:%d", port),
			"-hostkey", t.TempDir()+"/hk")
		srv.Env = append(os.Environ(),
			"SUPABASE_URL="+sink.URL, "SUPABASE_KEY=testkey")
		srv.Stderr = os.Stderr
		if err := srv.Start(); err != nil {
			t.Fatal(err)
		}
		defer func() {
			srv.Process.Kill()
			srv.Wait()
		}()

		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			c, err := net.Dial("tcp", fmt.Sprintf("127.0.0.1:%d", port))
			if err == nil {
				c.Close()
				break
			}
			time.Sleep(50 * time.Millisecond)
		}

		stdin, stdinW, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			defer stdinW.Close()
			step := func(keys string, d time.Duration) {
				stdinW.Write([]byte(keys))
				time.Sleep(d)
			}
			time.Sleep(1200 * time.Millisecond)
			step("\r", 2*time.Second) // title → world card
			// Demo-script drive: right + run always, hop every ~1.6s.
			// Raw repeats emulate OS autorepeat for the legacy mapper.
			t0 := time.Now()
			for time.Since(t0) < 11*time.Second {
				keys := "dx"
				if math.Mod(time.Since(t0).Seconds(), 1.617) < 0.37 {
					keys = "dxw"
				}
				step(keys, 25*time.Millisecond)
			}
			// Suicide the remaining lives: three deaths end the run.
			for i := 0; i < 6; i++ {
				step("k", 2500*time.Millisecond)
			}
			// Ask → yes → name → accept (solve PoW, POST) → close → quit.
			step("y", 1200*time.Millisecond)
			step("zz", 400*time.Millisecond)
			step("\r", 6000*time.Millisecond)
			step("\x1b", 800*time.Millisecond)
			step("q", 3*time.Second)
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
				t.Logf("ssh exited with error: %v", err)
			}
		case <-time.After(75 * time.Second):
			cmd.Process.Kill()
			t.Fatal("ssh session did not finish")
		}
		if !strings.Contains(out.String(), "\x1b[<u\x1b[?25h\x1b[?23t\x1b[?1049l") {
			t.Errorf("session output missing clean epilogue")
		}
		mu.Lock()
		defer mu.Unlock()
		return len(inserts) > 0
	}

	// The blind drive scores on the coin blocks most of the time; retry
	// once so a mistimed hop does not flake CI.
	if !session() {
		t.Log("first session submitted nothing (drive missed the coins); retrying")
		if !session() {
			t.Fatal("no scores insert reached the sink in two sessions")
		}
	}

	mu.Lock()
	e := inserts[len(inserts)-1]
	mu.Unlock()
	if e.EngineVersion != board.EngineVersion {
		t.Errorf("submission engine version %q != build %q", e.EngineVersion, board.EngineVersion)
	}
	var wire struct {
		Ticks int `json:"ticks"`
	}
	if err := json.Unmarshal([]byte(e.Replay), &wire); err != nil {
		t.Fatalf("submitted replay is not recorder JSON: %v", err)
	}
	// The drive alone is >11s (~700 ticks); a final-life fragment (the
	// recorder-wipe bug shipped ~310-tick recordings) cannot span it.
	if wire.Ticks < 600 {
		t.Errorf("recording too short to span the deaths: %d ticks", wire.Ticks)
	}

	// The verification contract: replaying the submitted recording must
	// reproduce the submitted row exactly.
	levels, err := replay.DayLevels(e.Mode, e.Day)
	if err != nil {
		t.Fatalf("DayLevels: %v", err)
	}
	res, err := replay.Run(levels, e.Mode, e.Replay)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if res.Score != e.Score || res.Level != e.Level || res.State != engine.StateGameOver {
		t.Fatalf("replay mismatch (the verifier would DROP this row): replay scored=%d level=%d state=%s, row claims score=%d level=%d",
			res.Score, res.Level, res.State, e.Score, e.Level)
	}
	t.Logf("submitted score=%d level=%d recording=%d ticks — replays clean", e.Score, e.Level, wire.Ticks)
}
