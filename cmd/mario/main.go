// Command mario is a fully terminal-based Mario-style platformer, built
// on the importable mario library.
//
// Controls: a/d or arrows move, w/space jump, x run, p pause, q quit,
// r restart (after game over / win).
//
// Flags:
//
//	-demo         run a headless scripted demo and exit
//	-demoticks N  demo length in ticks (with -demo)
//	-level FILE   play a custom ASCII level instead of the built-ins
//	-width N      viewport width in tiles (0 = terminal width)
//	-basic        force 16-color ANSI output instead of truecolor
//	-scores N     print the top N leaderboard scores and exit
//	-daily        with -scores: the daily board; alone: play today's challenge
//	-ui-preview M render a leaderboard UI screen headless (ask|entry|board|title-board)
//	-serve ADDR    run an unauthenticated SSH game server on ADDR (e.g. :2222)
//	-hostkey PATH  with -serve: persist the host key at PATH (created if missing)
//
// Scores can be submitted to a Supabase-backed leaderboard after game over;
// every submission carries the run's input recording — a GitHub Action
// replays it and keeps only rows that reproduce their score. See the board
// and replay packages and .env.

package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/Daviey/mario"
	"github.com/Daviey/mario/board"
	"github.com/Daviey/mario/engine"
	"github.com/Daviey/mario/render"
)

func main() {
	// Explicit help prints to stdout; usage after a parse error stays on
	// flag's default stderr. flag.Usage closes over out, so flipping it
	// before Parse covers both paths.
	out := io.Writer(os.Stderr)
	flag.Usage = func() { usage(out) }
	if helpRequested(os.Args[1:]) {
		out = os.Stdout
	}

	demo := flag.Bool("demo", false, "run a headless scripted demo and exit")
	demoTicks := flag.Int("demoticks", 6000, "run demo for `N` ticks (with -demo)")
	levelPath := flag.String("level", "", "play a custom ASCII level `FILE`")
	width := flag.Int("width", 0, "viewport `WIDTH` in tiles (0 = terminal width)")
	basic := flag.Bool("basic", false, "force 16-color ANSI output instead of truecolor")
	topN := flag.Int("scores", 0, "print the top `N` leaderboard scores and exit")
	daily := flag.Bool("daily", false, "play today's daily challenge (or with -scores, print the daily board)")
	uiPreview := flag.String("ui-preview", "", "render a leaderboard UI screen headless (`MODE`: ask, entry, board, title-board)")
	verifyPending := flag.Bool("verify-pending", false, "verify pending replay-backed scores (service key) and exit")
	serveAddr := flag.String("serve", "", "run an unauthenticated SSH game server on `ADDR` (e.g. :2222) instead of playing")
	hostKeyPath := flag.String("hostkey", "", "with -serve: persist the SSH host key at `PATH` (created if missing)")
	moshBin := flag.String("mosh", "", "with -serve: enable the mosh handshake via mosh-server at `PATH` (\"\" = off, \"auto\" = look up in PATH)")
	moshPorts := flag.String("mosh-ports", "60000:60100", "with -mosh: UDP `RANGE` for mosh sessions, \"lo:hi\"")
	flag.Parse()
	trueColor := !*basic && trueColorSupported()

	// The verifier holds the service key: it must not honor a CWD-relative
	// .env before its own guarded load (see runVerifyPending).
	if !*verifyPending {
		board.LoadDotEnv(".env")
	}
	if *verifyPending {
		if err := runVerifyPending(); err != nil {
			fmt.Fprintf(os.Stderr, "mario: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if *topN > 0 {
		client, err := board.FromEnv()
		if err != nil {
			fmt.Fprintf(os.Stderr, "mario: %v\n", err)
			os.Exit(1)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		var rows []board.Row
		var terr error
		if *daily {
			rows, terr = client.TopMode(ctx, *topN, "", "daily", time.Now().UTC().Format("2006-01-02"))
		} else {
			rows, terr = client.Top(ctx, *topN, "")
		}
		if terr != nil {
			fmt.Fprintf(os.Stderr, "mario: %v\n", terr)
			os.Exit(1)
		}
		printScores(os.Stdout, rows)
		return
	}

	if *uiPreview != "" {
		if err := mario.UIPreview(os.Stdout, *uiPreview, trueColor); err != nil {
			fmt.Fprintf(os.Stderr, "mario: %v\n", err)
			os.Exit(1)
		}
		return
	}

	levels, err := mario.LoadLevels(*levelPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mario: %v\n", err)
		os.Exit(1)
	}

	if *serveAddr != "" {
		// Truecolor is the default over SSH — the operator cannot know
		// every client's terminal; -basic forces the 16-color palette.
		mb := *moshBin
		if mb == "auto" {
			if p, err := exec.LookPath("mosh-server"); err == nil {
				mb = p
			} else {
				fmt.Fprintf(os.Stderr, "mario: -mosh auto: mosh-server not found in PATH; mosh disabled\n")
				mb = ""
			}
		}
		if err := runServe(levels, *serveAddr, *hostKeyPath, !*basic, mb, *moshPorts); err != nil {
			fmt.Fprintf(os.Stderr, "mario: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if *demo {
		mario.RunDemo(os.Stdout, levels, trueColor, *demoTicks)
		return
	}

	if _, err := run(levels, *width, trueColor, *daily); err != nil {
		fmt.Fprintf(os.Stderr, "mario: %v\n", err)
		os.Exit(1)
	}
}

// helpRequested reports whether any argument explicitly asks for help.
// flag.Parse would print usage for these too (exit 0), but only to its
// default stderr stream.
func helpRequested(args []string) bool {
	for _, a := range args {
		if a == "-h" || a == "-help" || a == "--help" {
			return true
		}
	}
	return false
}

// usage prints the CLI help. The flag reference is generated from the
// registered flag definitions (VisitAll plus the backquoted-placeholder
// convention UnquoteUsage implements), so it cannot drift from them.
func usage(w io.Writer) {
	fmt.Fprintf(w, "mario %s — SUPER CLI MARIO, a terminal Mario-style platformer\n", render.Version)
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "usage: mario [flags]")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "flags:")
	flag.VisitAll(func(f *flag.Flag) {
		name, text := flag.UnquoteUsage(f)
		var b strings.Builder
		fmt.Fprintf(&b, "-%s", f.Name)
		if name != "" {
			fmt.Fprintf(&b, " %s", name)
		}
		pad := 22 - b.Len()
		if pad < 2 {
			pad = 2
		}
		if f.DefValue != "" && f.DefValue != "0" && f.DefValue != "false" {
			text += fmt.Sprintf(" (default %s)", f.DefValue)
		}
		fmt.Fprintf(w, "  %s%s%s\n", b.String(), strings.Repeat(" ", pad), text)
	})

	row := func(keys, what string) {
		fmt.Fprintf(w, "  %-23s  %s\n", keys, what)
	}

	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "game controls:")
	row("a/d, arrows", "move")
	row("w, space, up", "jump")
	row("x (hold)", "run")
	row("p", "pause")
	row("q", "quit")
	row("r", "restart after game over or win")

	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "leaderboard keys:")
	row("l", "open board from the title screen; close it (q/esc too)")
	row("y / n", "after game over: submit score / skip")
	row("letters, digits", "type a name (A-Z 0-9 . -, max 8 chars)")
	row("enter", "submit name (saved player name if left blank)")
	row("backspace", "delete a character")
	row("esc", "back out")
	row("r", "on the board: restart a new game")

	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "examples:")
	row("mario", "play in this terminal")
	row("mario -width 30", "fixed narrow viewport")
	row("mario -level lvl.txt", "play a custom ASCII level")
	row("mario -demo", "headless scripted demo (no TTY needed)")
	row("mario -scores 10", "print the online top 10")
	row("mario -verify-pending", "service tool: replay-verify pending scores")
	row("mario -ui-preview board", "render a leaderboard screen")

	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Scores submit to a Supabase-backed leaderboard after game over;")
	fmt.Fprintln(w, "server config comes from .env or SUPABASE_URL/SUPABASE_KEY")
	fmt.Fprintln(w, "in the environment (see the board package).")
}

// run plays the game on the real terminal and returns the final score.
func run(levels []*engine.Level, width int, trueColor, daily bool) (int, error) {
	// Catch termination from the very first line: a Ctrl+C racing our raw
	// mode setup must still restore the terminal. SIGHUP covers an SSH
	// session drop; without it the process dies with the kitty keyboard
	// protocol still pushed and the shell becomes unusable.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGPIPE, syscall.SIGQUIT)

	if !isTTY(os.Stdin) {
		return 0, fmt.Errorf("stdin is not a terminal (use -demo for a headless run)")
	}
	restore, err := rawMode()
	if err != nil {
		return 0, fmt.Errorf("cannot set raw mode: %w", err)
	}
	cleanup := sync.OnceFunc(func() {
		// Reset every terminal mode we touched, then hand echo back.
		os.Stdout.WriteString("\x1b[?2026l\x1b[<u\x1b[?25h\x1b[23t\x1b[?1049l\x1b[0m\r\n")
		os.Stdout.Sync()
		restore()
	})
	defer cleanup()
	go func() {
		<-sig
		cleanup()
		os.Exit(0)
	}()

	// Best-effort: kitty keyboard protocol, alt screen, hidden cursor,
	// window title. Unsupported terminals ignore these. Flags 1|2|8 =
	// disambiguate + event types + report ALL keys as escape codes, so
	// even plain letters and space get press/repeat/release events —
	// without them every hold relies on OS-repeat inference, whose
	// uncalibrated grace is shorter than the ~500-600ms repeat delay, so
	// the first hold of a key stutters (moves, dead gap, resumes) and a
	// player who lets go during the gap never gets smooth holds at all.
	// The leaderboard UI decodes CSI-u back to plain bytes (the Router).
	// The leading pop heals any mode left over by a previous run that was
	// killed without cleanup, before we push our own entry.
	os.Stdout.WriteString("\x1b[<u\x1b[>11u\x1b[?1049h\x1b[?25l\x1b[2J\x1b[22t\x1b]0;SUPER CLI MARIO\a")

	// Fill the terminal: width in tiles, height minus HUD/status rows,
	// Pix/2 terminal rows per tile. A taller window shows more sky/world,
	// same sprite size.
	viewW := width
	if viewW <= 0 {
		viewW = termWidth() / render.Pix // viewport is measured in tiles now
	}
	viewH := (termHeight() - 2) * 2 / render.Pix

	app := mario.New(&mario.Options{Levels: levels, ViewW: viewW, ViewH: viewH})
	if daily {
		app.StartDaily()
	}

	// One goroutine owns fd 0 for the life of the process; the app routes
	// each chunk to the game mapper or the leaderboard UI (never both).
	go func() {
		defer func() {
			if r := recover(); r != nil {
				cleanup()
				panic(r)
			}
		}()
		buf := make([]byte, 64)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				app.Feed(append([]byte(nil), buf[:n]...))
			}
			if err != nil {
				return
			}
		}
	}()

	// Follow window resizes with the same fit as launch (width in tiles,
	// height minus the HUD rows): the viewport changes on the next tick
	// and the next frame repaints in full at the new size. An explicit
	// -width keeps its width; only the height re-fits.
	stopResize := onResize(func() {
		rows := termHeight()
		if rows <= 0 {
			return // size probe failed; keep the current viewport
		}
		w := width
		if w <= 0 {
			if cols := termWidth(); cols > 0 {
				w = cols / render.Pix
			}
		}
		if w <= 0 {
			return
		}
		app.Resize(w, (rows-2)*2/render.Pix)
	})
	defer stopResize()

	// Differential rendering: each frame only the changed cells are sent,
	// wrapped in synchronized-output mode so updates never tear. This keeps
	// 60 fps responsive even over an SSH link.
	app.Run(render.NewStream(os.Stdout, render.NewPalette(trueColor)))
	return app.Game.Score, nil
}

func isTTY(f *os.File) bool {
	st, err := f.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}

// trueColorSupported sniffs the environment for terminals that render
// 24-bit color sequences.
func trueColorSupported() bool {
	term := os.Getenv("TERM")
	if strings.Contains(term, "truecolor") || strings.Contains(term, "direct") {
		return true
	}
	if os.Getenv("COLORTERM") != "" {
		return true
	}
	// Ghostty and WezTerm advertise truecolor capability without either
	// variable naming it directly.
	if term == "ghostty" || term == "xterm-ghostty" || term == "wezterm" {
		return true
	}
	return false
}
