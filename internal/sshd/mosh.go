package sshd

import (
	"io"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
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

// defaultMoshMax is the fallback for Server.MoshMax: the cap on
// concurrently running mosh servers. Two-tier accounting by design:
// the admission queue caps SHELL sessions at MaxSessions, but a mosh
// child outlives the SSH connection that spawned it, so it holds a
// slot in neither — it gets its own hard cap (MoshMax) instead.
const defaultMoshMax = 16

// moshRunning counts live mosh children. Acquisition is CAS-style
// (acquireMoshSlot) before the spawn, release on spawn failure or
// child exit, so the cap holds exactly even under bursts.
var moshRunning atomic.Int32

// RunningMosh reports the number of live mosh servers.
func RunningMosh() int { return int(moshRunning.Load()) }

// acquireMoshSlot atomically claims one of the MoshMax child slots:
// compare-and-swap, not check-then-increment, so a burst of concurrent
// handshakes cannot all observe the same headroom and oversubscribe
// the cap. The claim happens BEFORE the exec request is answered, so
// a refused client sees a CHANNEL_FAILURE instead of a silent yes.
func (s *Server) acquireMoshSlot() bool {
	for {
		old := moshRunning.Load()
		if old >= int32(s.moshMax()) {
			return false
		}
		if moshRunning.CompareAndSwap(old, old+1) {
			return true
		}
	}
}

// moshMax is the resolved child cap for this server (default
// defaultMoshMax; see Server.MoshMax).
func (s *Server) moshMax() int {
	if s.MoshMax > 0 {
		return s.MoshMax
	}
	return defaultMoshMax
}

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
// game binary, this port range and our own login name. No field of
// the request may ever be echoed into the spawn argv — the safety of
// this path lives in startMosh's rebuild from constants.
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
//
// The MoshMax slot has already been claimed by the caller
// (acquireMoshSlot, before the exec request was answered): every error
// path here releases it again, and the reaper below releases it when
// the spawned parent exits — the count never drifts.
func (s *Server) startMosh(c *conn, req *moshRequest) (err error) {
	defer func() {
		if err != nil {
			moshRunning.Add(-1) // rollback the caller's claim
		}
	}()
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
	// mosh client does); xterm-256color is the fallback. mosh-server
	// hard-fails without a UTF-8 native locale — and the locale vars
	// the client sends via -l flags are stripped with the rest of the
	// untrusted argv — so force C.UTF-8 (always present with glibc;
	// the client's rendering is UTF-8 regardless).
	if t := c.ch.termValue(); t != "" {
		req.term = t
	}
	cmd.Env = moshEnv(os.Environ(), req.term, c.ch.decideColorTerm())
	// Own process group: the SSH connection ending (or the server
	// process being signalled) must not end the game session.
	cmd.SysProcAttr = ownProcessGroup()

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
		reap(pid)
		// The CONNECT line is in the pipe buffer before the parent
		// exits, but the relays may not have pumped it to the wire
		// yet — and the forked child holds the pipes open, so there
		// is no EOF to wait for. Give them a beat to drain.
		time.Sleep(moshConnectDrain)
		c.ch.shutdown()
	}()
	return nil
}

// moshConnectDrain is the beat the reaper gives the output relays to
// pump the CONNECT line to the wire before the channel teardown (the
// forked child holds the pipes open, so there is no EOF to wait for).
const moshConnectDrain = 100 * time.Millisecond

// moshEnv builds the environment handed to mosh-server. Inherited
// TERM/COLORTERM/locale entries are stripped first: mosh-server
// overwrites the child's TERM unconditionally ("xterm-256color" with
// -c 256 — the client's TERM never survives mosh, verified against
// mosh 1.4.0 source), and a duplicate earlier entry would shadow the
// appended one anyway (getenv returns the first match). COLORTERM is
// the one truecolor signal that reaches the game through mosh-server
// untouched: colorTerm is already fully decided by the caller
// (channel.decideColorTerm: forwarded env request > TERM family >
// DA2/DA3 probe), so the game runs its truecolor palette instead of
// ANSI-16 — the "less colours over mosh" bug. mosh-client emits 38;2
// unconditionally, so this must stay honest for 256-only terminals.
func moshEnv(inherited []string, term, colorTerm string) []string {
	var out []string
	for _, kv := range inherited {
		k := kv
		if i := strings.IndexByte(kv, '='); i >= 0 {
			k = kv[:i]
		}
		if k == "TERM" || k == "COLORTERM" || k == "LANG" || strings.HasPrefix(k, "LC_") {
			continue
		}
		out = append(out, kv)
	}
	out = append(out, "TERM="+term, "LC_ALL=C.UTF-8")
	if colorTerm != "" {
		out = append(out, "COLORTERM="+colorTerm)
	}
	return out
}
