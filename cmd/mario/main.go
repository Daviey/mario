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
//	-ui-preview M render a leaderboard UI screen headless (ask|entry|board|title-board)
//
// Scores can be submitted to a Supabase-backed leaderboard after game over;
// see the board package and .env.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"mario"
	"mario/board"
	"mario/engine"
	"mario/render"
)

func main() {
	demo := flag.Bool("demo", false, "run a headless scripted demo and exit")
	demoTicks := flag.Int("demoticks", 6000, "demo length in ticks (with -demo)")
	levelPath := flag.String("level", "", "play a custom ASCII level file")
	width := flag.Int("width", 0, "viewport width in tiles (0 = terminal width)")
	basic := flag.Bool("basic", false, "force 16-color ANSI output instead of truecolor")
	topN := flag.Int("scores", 0, "print the top N leaderboard scores and exit")
	uiPreview := flag.String("ui-preview", "", "render a leaderboard UI screen headless (ask, entry, board, title-board)")
	flag.Parse()
	trueColor := !*basic && trueColorSupported()

	board.LoadDotEnv(".env")

	if *topN > 0 {
		client, err := board.FromEnv()
		if err != nil {
			fmt.Fprintf(os.Stderr, "mario: %v\n", err)
			os.Exit(1)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		rows, err := client.Top(ctx, *topN, "")
		if err != nil {
			fmt.Fprintf(os.Stderr, "mario: %v\n", err)
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

	if *demo {
		mario.RunDemo(os.Stdout, levels, trueColor, *demoTicks)
		return
	}

	if _, err := run(levels, *width, trueColor); err != nil {
		fmt.Fprintf(os.Stderr, "mario: %v\n", err)
		os.Exit(1)
	}
}

// run plays the game on the real terminal and returns the final score.
func run(levels []*engine.Level, width int, trueColor bool) (int, error) {
	// Catch termination from the very first line: a Ctrl+C racing our raw
	// mode setup must still restore the terminal. SIGHUP covers an SSH
	// session drop; without it the process dies with the kitty keyboard
	// protocol still pushed and the shell becomes unusable.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGPIPE)

	if !isTTY(os.Stdin) {
		return 0, fmt.Errorf("stdin is not a terminal (use -demo for a headless run)")
	}
	restore, err := rawMode()
	if err != nil {
		return 0, fmt.Errorf("cannot set raw mode: %w", err)
	}
	var saveCalibration func()
	cleanup := sync.OnceFunc(func() {
		// Reset every terminal mode we touched, then hand echo back.
		os.Stdout.WriteString("\x1b[?2026l\x1b[<u\x1b[?25h\x1b[23t\x1b[?1049l\x1b[0m\r\n")
		os.Stdout.Sync()
		restore()
		// Persist input calibration on every exit path, including the
		// signal handler's os.Exit below (defers don't run there).
		if saveCalibration != nil {
			saveCalibration()
		}
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
	// The leaderboard UI decodes CSI-u back to plain bytes (gameIO).
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
	saveCalibration = app.SaveCalibration

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
