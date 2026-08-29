package sshd

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// Mosh support: anonymous players can upgrade their SSH transport to
// mosh's UDP roaming protocol. The mosh client connects over SSH exactly
// once, with an exec request shaped like:
//
//	mosh-server new [-c] [-l NAME] [-p N|M:N] ... [--] CMD...
//
// prints "\n\nMOSH CONNECT <udp-port> <key>\n\n" on stdout, and keeps
// running attached to its own pty long after the client has torn the SSH
// connection down. We accept ONLY that handshake: the argv is rebuilt
// from a strict whitelist (client flags are dropped, not trusted), the
// command is always this very game binary, and the port range is always
// ours. There is no shell behind this path — same rule as the rest of
// the server.

// moshMax caps concurrently running mosh servers. The SSH MaxSessions
// cap cannot cover them: a mosh server outlives its SSH connection.
const moshMax = 16

var moshRunning atomic.Int32

// RunningMosh reports the number of live mosh servers.
func RunningMosh() int { return int(moshRunning.Load()) }

// moshRequest is the sanitized form of a client's exec request.
type moshRequest struct {
	colors bool   // -c: 256-color default
	term   string // TERM for the game on mosh's pty
}

// splitQuoted splits a command line the way a POSIX shell would, for
// the quoting the mosh client uses: every argv element arrives
// single-quoted ("mosh-server 'new' '-c' '256' …").
func splitQuoted(s string) []string {
	var out []string
	var cur strings.Builder
	inField := false
	flush := func() {
		if inField {
			out = append(out, cur.String())
			cur.Reset()
			inField = false
		}
	}
	for i := 0; i < len(s); i++ {
		switch ch := s[i]; {
		case ch == '\'' || ch == '"':
			q := ch
			i++
			for i < len(s) && s[i] != q {
				cur.WriteByte(s[i])
				i++
			}
			inField = true // closing quote skipped by loop increment
		case ch == ' ' || ch == '\t':
			flush()
		default:
			cur.WriteByte(ch)
			inField = true
		}
	}
	flush()
	return out
}

// parseMoshArgv validates a client exec command for the mosh handshake.
// Only "mosh-server new" with known-safe flags passes; everything else
// (shells, other binaries, unknown flags) is rejected. Values of known
// flags are ignored, not trusted: the spawn below always uses this
// game binary, this port range and our own login name.
func parseMoshArgv(cmdline string) (*moshRequest, bool) {
	fields := splitQuoted(cmdline)
	if len(fields) < 2 || fields[0] != "mosh-server" || fields[1] != "new" {
		return nil, false
	}
	req := &moshRequest{term: "xterm-256color"}
	for _, f := range fields[2:] {
		switch {
		case f == "-c":
			req.colors = true // its colorspace value is the next bare word
		case f == "-v", f == "-vv":
			// verbose: harmless, logs to stderr
		case f == "-s":
			// -s: bind-to-ssh-address (never forwarded; see startMosh)
		case f == "-l", f == "-p", f == "-i":
			// login name / port / interface: values arrive as the next
			// bare word and are dropped — we force our own.
		case f == "--":
			// the client's trailing command: never trusted, stop here.
			return req, true
		case strings.HasPrefix(f, "-"):
			return nil, false // unknown or value-attached flag: refuse
		default:
			// bare word: a flag value or the command name; ignored.
		}
	}
	return req, true
}

// moshPorts is the UDP port range handed to mosh-server.
func (s *Server) moshPorts() string {
	if s.MoshPortRange != "" {
		return s.MoshPortRange
	}
	return "60000:60100"
}

// startMosh spawns the real mosh-server for one client and relays its
// output to the SSH channel. The child is deliberately NOT killed when
// the SSH side ends — mosh-server outlives the connection by design.
func (s *Server) startMosh(c *conn, req *moshRequest) error {
	if moshRunning.Load() >= moshMax {
		return fmt.Errorf("sshd: too many mosh sessions")
	}
	game, err := os.Executable()
	if err != nil {
		return err
	}

	argv := []string{"new"}
	if req.colors {
		argv = append(argv, "-c", "256")
	}
	// NB: nix mosh 1.4's client-side "-s" means "bind the UDP socket
	// to the SSH connection's source address" — passing it through
	// makes mosh-server bind one specific interface (wrong for a
	// multi-homed host) while the client dials the address it
	// resolved. Never forward it; bind the wildcard instead.
	argv = append(argv, "-l", "mario", "-p", s.moshPorts())
	argv = append(argv, "--", game)

	cmd := exec.Command(s.MoshBin, argv...)
	// TERM travels with the pty request when the client sent one (the
	// mosh client does); xterm-256color is the fallback.
	if t := c.ch.term; t != "" {
		req.term = t
	}
	cmd.Env = append(os.Environ(), "TERM="+req.term)
	// Own process group: the SSH connection ending (or the server
	// process being signalled) must not end the game session.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	moshRunning.Add(1)
	pid := cmd.Process.Pid

	// Relay mosh-server output to the client. Once the client has the
	// key it tears the SSH connection down; writes then fail and the
	// relays end while the process keeps running.
	relay := func(rc io.ReadCloser) {
		defer rc.Close()
		buf := make([]byte, 4096)
		for {
			n, err := rc.Read(buf)
			if n > 0 {
				if _, werr := (&Session{ch: c.ch}).Write(buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}
	go relay(stdout)
	go relay(stderr)

	// mosh-server prints CONNECT and then double-forks: the parent we
	// spawned exits almost immediately while the real session server
	// keeps running (own process group — never signalled by us). The
	// parent's exit is the SSH channel's end, exactly like sshd
	// tearing down when its direct child exits: the mosh client's
	// wrapper waits for ssh to finish before starting the UDP client.
	// Without this teardown it waits forever — the classic hang.
	go func() {
		defer moshRunning.Add(-1)
		var ws syscall.WaitStatus
		syscall.Wait4(pid, &ws, 0, nil)
		// The CONNECT line is in the pipe buffer before the parent
		// exits, but the relays may not have pumped it to the wire
		// yet — and the forked child holds the pipes open, so there
		// is no EOF to wait for. Give them a beat to drain.
		time.Sleep(100 * time.Millisecond)
		c.ch.shutdown()
	}()
	return nil
}
