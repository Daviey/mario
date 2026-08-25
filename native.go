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

// Flags:
//
//	-demo        run a headless scripted demo and exit
//	-level FILE  play a custom ASCII level file instead of the built-ins
//	-width N     viewport width in tiles (0 = terminal width)
//	-basic       force 16-color ANSI output instead of truecolor
//	-scores N    print the top N leaderboard scores and exit
func main() {
	demo := flag.Bool("demo", false, "run a headless scripted demo and exit")
	demoTicks := flag.Int("demoticks", 6000, "demo length in ticks (with -demo)")
	levelPath := flag.String("level", "", "play a custom ASCII level file")
	width := flag.Int("width", 0, "viewport width in tiles (0 = terminal width)")
	basic := flag.Bool("basic", false, "force 16-color ANSI output instead of truecolor")
	topN := flag.Int("scores", 0, "print the top N leaderboard scores and exit")
	flag.Parse()
	trueColor := !*basic && trueColorSupported()

	board.LoadDotEnv(".env")

	if *topN > 0 {
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
		runDemo(os.Stdout, levels, trueColor, *demoTicks)
		return
	}

	score, err := run(levels, *width, trueColor)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mario: %v\n", err)
		os.Exit(1)
	}
	if err := maybeSubmit(os.Stdout, os.Stdin, score); err != nil {
		// Submission problems never cost the player their game.
		fmt.Fprintf(os.Stdout, "score submission skipped: %v\n", err)
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
	play(g, mapper, st)
	return g.Score, nil
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
