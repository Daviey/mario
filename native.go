//go:build !js

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

	"mario/board"
	"mario/engine"
	"mario/input"
	"mario/render"
)

// -basic       force 16-color ANSI output instead of truecolor
// -scores N    print the top N verified leaderboard scores and exit
// -replay FILE replay a recorded run headless and print the outcome
// -verify-pending  verify pending score submissions (service key)
func main() {
	demo := flag.Bool("demo", false, "run a headless scripted demo and exit")
	demoTicks := flag.Int("demoticks", 6000, "demo length in ticks (with -demo)")
	demoRecPath := flag.String("demo-recording", "", "with -demo: write the recording to this JSON file")
	levelPath := flag.String("level", "", "play a custom ASCII level file")
	width := flag.Int("width", 0, "viewport width in tiles (0 = terminal width)")
	basic := flag.Bool("basic", false, "force 16-color ANSI output instead of truecolor")
	topN := flag.Int("scores", 0, "print the top N leaderboard scores and exit")
	replayPath := flag.String("replay", "", "replay a recorded run headless and print the outcome")
	verify := flag.Bool("verify-pending", false, "verify pending submissions (needs the service role key)")
	flag.Parse()
	trueColor := !*basic && trueColorSupported()

	board.LoadDotEnv(".env")

	switch {
	case *verify:
		client, err := verifyClient()
		if err != nil {
			fmt.Fprintf(os.Stderr, "mario: %v\n", err)
			os.Exit(1)
		}
		if err := verifyPending(context.Background(), client, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "mario: %v\n", err)
			os.Exit(1)
		}
		return
	case *replayPath != "":
		if err := replayFile(*replayPath, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "mario: %v\n", err)
			os.Exit(1)
		}
		return
	case *topN > 0:
		client, err := board.FromEnv()
		if err != nil {
			fmt.Fprintf(os.Stderr, "mario: %v\n", err)
			os.Exit(1)
		}
		rows, err := client.Top(context.Background(), *topN)
		if err != nil {
			fmt.Fprintf(os.Stderr, "mario: %v\n", err)
			os.Exit(1)
		}
		printScores(os.Stdout, rows)
		return
	}

	levels, err := loadLevels(*levelPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mario: %v\n", err)
		os.Exit(1)
	}

	if *demo {
		rec := runDemo(os.Stdout, levels, trueColor, *demoTicks)
		if *demoRecPath != "" {
			if err := writeRecording(*demoRecPath, rec); err != nil {
				fmt.Fprintf(os.Stderr, "mario: %v\n", err)
				os.Exit(1)
			}
		}
		return
	}

	res, err := run(levels, *width, trueColor)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mario: %v\n", err)
		os.Exit(1)
	}
	if err := maybeSubmit(os.Stdout, os.Stdin, res, *levelPath == ""); err != nil {
		// Submission problems never cost the player their game.
		fmt.Fprintf(os.Stdout, "score submission skipped: %v\n", err)
	}
}

// verifyClient builds the service-role client: SUPABASE_SERVICE_KEY when
// present (GitHub Action secret), falling back to SUPABASE_KEY for local
// testing with the key in .env.
func verifyClient() (*board.Client, error) {
	if key := os.Getenv("SUPABASE_SERVICE_KEY"); key != "" {
		if u := os.Getenv("SUPABASE_URL"); u != "" {
			return board.New(u, key), nil
		}
	}
	return board.FromEnv()
}

// runResult is what a finished interactive session leaves behind.
type runResult struct {
	rec      *recorder
	score    int
	coins    int
	state    engine.State
	levelIdx int
}

// run plays the game on the real terminal.
func run(levels []*engine.Level, width int, trueColor bool) (*runResult, error) {
	// Catch termination from the very first line: a Ctrl+C racing our raw
	// mode setup must still restore the terminal. SIGHUP covers an SSH
	// session drop; without it the process dies with the kitty keyboard
	// protocol still pushed and the shell becomes unusable.
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGPIPE)

	if !isTTY(os.Stdin) {
		return nil, fmt.Errorf("stdin is not a terminal (use -demo for a headless run)")
	}
	restore, err := rawMode()
	if err != nil {
		return nil, fmt.Errorf("cannot set raw mode: %w", err)
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
	// without them every letter tap phantom-holds for the legacy repeat
	// window (overrun moves, eaten jumps, mushy left-right). Terminals
	// without the protocol keep the plain-byte fallback.
	// The leading pop heals any mode left over by a previous run that was
	// killed without cleanup, before we push our own entry.
	os.Stdout.WriteString("\x1b[<u\x1b[>11u\x1b[?1049h\x1b[?25l\x1b[2J\x1b[22t\x1b]0;SUPER CLI MARIO\a")

	viewW := width
	if viewW <= 0 {
		viewW = termWidth() / render.Pix // viewport is measured in tiles now
	}
	if viewW < 16 {
		viewW = 16
	}
	if viewW > 60 {
		viewW = 60
	}
	// Fill the terminal vertically too: rows minus HUD/status, two pixel
	// rows per tile. A taller window shows more sky/world, same sprite size.
	viewH := (termHeight() - 2) / 2
	if viewH < 4 {
		viewH = 4
	}
	if viewH > levels[0].Height {
		viewH = levels[0].Height
	}
	g := engine.NewGame(levels, viewW, viewH)

	mapper := input.NewMapper()
	go func() {
		buf := make([]byte, 64)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				mapper.Feed(buf[:n])
			}
			if err != nil {
				return
			}
		}
	}()

	// Differential rendering: each frame only the changed cells are sent,
	// wrapped in synchronized-output mode so updates never tear. This keeps
	// 60 fps responsive even over an SSH link.
	st := newStream(os.Stdout, render.NewPalette(trueColor))
	res := &runResult{
		rec:      play(g, mapper, st),
		score:    g.Score,
		coins:    g.CoinCount,
		state:    g.State,
		levelIdx: g.LevelIndex(),
	}
	return res, nil
}

// trueColorSupported sniffs the environment for terminals that render
// 24-bit color sequences.
func trueColorSupported() bool {
	ct := os.Getenv("COLORTERM")
	if strings.Contains(ct, "truecolor") || strings.Contains(ct, "24bit") {
		return true
	}
	if os.Getenv("WT_SESSION") != "" {
		return true // Windows Terminal
	}
	term := os.Getenv("TERM")
	for _, t := range []string{"ghostty", "kitty", "alacritty", "wezterm", "contour", "foot", "xterm-256color"} {
		if strings.Contains(term, t) {
			return true
		}
	}
	return false
}
