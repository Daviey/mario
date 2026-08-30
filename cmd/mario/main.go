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
//	-basic        force the 16-color palette instead of auto color depth
//	-nobell       disable terminal-bell sound feedback (on by default)
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
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"

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
	basic := flag.Bool("basic", false, "force the 16-color palette instead of auto color depth")
	topN := flag.Int("scores", 0, "print the top `N` leaderboard scores and exit")
	daily := flag.Bool("daily", false, "play today's daily challenge (or with -scores, print the daily board)")
	uiPreview := flag.String("ui-preview", "", "render a leaderboard UI screen headless (`MODE`: ask, entry, board, title-board)")
	verifyPending := flag.Bool("verify-pending", false, "verify pending replay-backed scores (service key) and exit")
	serveAddr := flag.String("serve", "", "run an unauthenticated SSH game server on `ADDR` (e.g. :2222) instead of playing")
	hostKeyPath := flag.String("hostkey", "", "with -serve: persist the SSH host key at `PATH` (created if missing)")
	moshBin := flag.String("mosh", "", "with -serve: enable the mosh handshake via mosh-server at `PATH` (\"\" = off, \"auto\" = look up in PATH)")
	moshPorts := flag.String("mosh-ports", "60000:60100", "with -mosh: UDP `RANGE` for mosh sessions, \"lo:hi\"")
	maxSessions := flag.Int("maxsessions", 0, "with -serve: concurrent session cap (0 = 16); excess connections are refused pre-handshake")
	cheats := flag.Bool("cheats", false, "cheat mode: unlimited fireballs; the run is not recorded and cannot be submitted to the leaderboard")
	nobell := flag.Bool("nobell", false, "disable terminal-bell sound feedback (coins, stomps, power-ups...)")
	dumpReplays := flag.Int("dump-replays", 0, "dump the latest N replay recordings as replay-<id>.json and exit (direct DB, needs SUPABASE_DB_PASSWORD)")
	replayFile := flag.String("replay", "", "trace a recorded replay `FILE` (from -dump-replays) tick by tick and exit")
	showVersion := flag.Bool("version", false, "print build and engine versions and exit")
	dayFlag := flag.String("day", "", "with -replay -daily: the challenge `DAY` to rebuild (default today)")
	flag.Parse()
	if *showVersion {
		fmt.Printf("mario %s (engine %s)\n", render.Version, board.EngineVersion)
		return
	}
	// Auto color depth (render.ColorDepthFor — the rule shared with the
	// SSH host and the mosh child): 24-bit when the terminal is known
	// truecolor (a COLORTERM hint or a TrueColorTerm TERM family), the
	// fixed 256-color cube when TERM advertises 256 colors (every
	// -256color terminal, tmux, and mosh's cell model), base-16 only
	// for terminals that claim neither. -basic forces 16.
	colors := render.Colors16
	if !*basic {
		colors = render.ColorDepthFor(os.Getenv("TERM"), os.Getenv("COLORTERM"))
	}

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
	if *dumpReplays > 0 {
		if err := runDumpReplays(*dumpReplays); err != nil {
			fmt.Fprintf(os.Stderr, "mario: %v\n", err)
			os.Exit(1)
		}
		return
	}
	if *replayFile != "" {
		if err := runReplayTrace(*replayFile, *daily, *dayFlag); err != nil {
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
		if err := runTopScores(client, *topN, *daily); err != nil {
			fmt.Fprintf(os.Stderr, "mario: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if *uiPreview != "" {
		if err := mario.UIPreview(os.Stdout, *uiPreview, colors); err != nil {
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
		// Color depth is per-session over SSH — the operator cannot know
		// every client's terminal; -basic forces the 16-color palette.
		mb, moshNote := resolveMoshBin(*moshBin)
		if moshNote != "" {
			fmt.Fprintf(os.Stderr, "mario: %s\n", moshNote)
		}
		if err := runServe(levels, *serveAddr, *hostKeyPath, *basic, mb, *moshPorts, *maxSessions, !*nobell); err != nil {
			fmt.Fprintf(os.Stderr, "mario: %v\n", err)
			os.Exit(1)
		}
		return
	}

	if *demo {
		mario.RunDemo(os.Stdout, levels, colors, *demoTicks)
		return
	}

	if _, err := run(levels, *width, colors, *daily, *cheats, !*nobell); err != nil {
		fmt.Fprintf(os.Stderr, "mario: %v\n", err)
		os.Exit(1)
	}
}

// resolveMoshBin turns the -mosh flag into the mosh-server binary to
// launch per session: "" disables the mosh handshake, any other value is
// an explicit path taken as given, and "auto" resolves mosh-server via
// PATH — falling back to disabled, with an operator note, when absent.
func resolveMoshBin(flag string) (bin, note string) {
	if flag != "auto" {
		return flag, ""
	}
	p, err := exec.LookPath("mosh-server")
	if err != nil {
		return "", "-mosh auto: mosh-server not found in PATH; mosh disabled"
	}
	return p, ""
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
	row("mario -dump-replays 5", "service tool: dump recent submissions' recordings")
	row("mario -replay replay-<id>.json", "service tool: trace a recording tick by tick")
	row("mario -ui-preview board", "render a leaderboard screen")

	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Scores submit to a Supabase-backed leaderboard after game over;")
	fmt.Fprintln(w, "server config comes from .env or SUPABASE_URL/SUPABASE_KEY")
	fmt.Fprintln(w, "in the environment (see the board package).")
}

// run plays the game on the real terminal and returns the final score.
func run(levels []*engine.Level, width int, colors render.ColorMode, daily, cheats, bellOn bool) (int, error) {
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
		// Reset every terminal mode we touched — synchronized output
		// off, kitty pop, cursor and title back, alt-screen exit, SGR
		// reset — then hand echo back. termEpilogue pins the byte order
		// (pop before the alt-screen exit) and its rationale.
		os.Stdout.WriteString(termEpilogue)
		os.Stdout.Sync()
		restore()
	})
	defer cleanup()
	go func() {
		<-sig
		cleanup()
		os.Exit(0) // deliberate: Ctrl+C/SIGTERM is a normal quit, not a failure
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
	// killed without cleanup, before we push our own entry. The push
	// comes AFTER the alt-screen enter and cleanup's pop BEFORE its exit
	// (termPrologue/termEpilogue pin the order): a terminal that
	// snapshots keyboard state with the screen would otherwise
	// resurrect the pushed level on exit.
	os.Stdout.WriteString(termPrologue)

	// Fill the terminal via the shared fit (viewFor): width in tiles,
	// height minus HUD/status rows. A taller window shows more
	// sky/world, same sprite size. An explicit -width keeps its width.
	viewW, viewH := viewFor(termWidth(), termHeight())
	if width > 0 {
		viewW = width
	}

	opts := &mario.Options{
		Levels:    levels,
		ViewW:     viewW,
		ViewH:     viewH,
		Cheats:    cheats,
		Surface:   "local", // play-context diagnostics with each submission
		Term:      os.Getenv("TERM"),
		ColorTerm: os.Getenv("COLORTERM"),
	}
	if bellOn {
		// Sound feedback: BEL bytes on stdout — the one audio channel
		// every terminal carries. Mosh runs this same binary as a
		// plain local process under mosh-server, so local, SSH and
		// mosh all ring the same bell.
		opts.Sound = newBell(os.Stdout).ring
	}
	app := mario.New(opts)
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
		w, h := viewFor(termWidth(), rows)
		if width > 0 {
			w = width // an explicit -width keeps its width; only the height re-fits
		}
		if w <= 0 {
			return
		}
		app.Resize(w, h)
	})
	defer stopResize()

	// Differential rendering: each frame only the changed cells are sent,
	// wrapped in synchronized-output mode so updates never tear. This keeps
	// 60 fps responsive even over an SSH link.
	app.Run(render.NewStream(os.Stdout, render.NewPalette(colors)))
	return app.Game.Score, nil
}

func isTTY(f *os.File) bool {
	st, err := f.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}
