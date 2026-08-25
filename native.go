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
	"time"

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
//	-ui-preview M  render a leaderboard UI screen headless (ask|entry|board|title-board)
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
		rows, err := client.Top(context.Background(), *topN)
		if err != nil {
			fmt.Fprintf(os.Stderr, "mario: %v\n", err)
			os.Exit(1)
		}
		printScores(os.Stdout, rows)
		return
	}

	if *uiPreview != "" {
		if err := uiPreviewScreen(os.Stdout, *uiPreview, trueColor); err != nil {
			fmt.Fprintf(os.Stderr, "mario: %v\n", err)
			os.Exit(1)
		}
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
	// Fill the terminal vertically too: rows minus HUD/status, Pix/2
	// terminal rows per tile. A taller window shows more sky/world, same
	// sprite size.
	viewH := (termHeight() - 2) * 2 / render.Pix
	if viewH < 4 {
		viewH = 4
	}
	if viewH > levels[0].Height {
		viewH = levels[0].Height
	}
	g := engine.NewGame(levels, viewW, viewH)

	// One goroutine owns fd 0 for the life of the process; gameIO routes
	// each chunk to the game mapper or the leaderboard UI (never both).
	// Calibration (repeat delay, hold habits) persists across runs so the
	// first hold of a session is as smooth as the last of the previous one.
	mapper := input.NewMapper()
	loadKeyCalibration(mapper)
	saveCalibration = func() { saveKeyCalibration(mapper) }
	io := newGameIO(mapper, newScoreUI(nil, nil))
	go func() {
		buf := make([]byte, 64)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				io.feed(append([]byte(nil), buf[:n]...))
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
	play(g, io, st)
	return g.Score, nil
}

// uiPreviewScreen renders one leaderboard UI screen headless: the demo
// script runs to game over, then the machine is stepped to the requested
// mode and one ANSI frame is printed (for visual checks and debugging).
func uiPreviewScreen(w *os.File, mode string, trueColor bool) error {
	g := engine.NewGame(engine.DefaultLevels(), 40, engine.LevelHeight)
	for t := range 6000 {
		g.Update(scriptInput(t))
	}
	if g.Score == 0 {
		return fmt.Errorf("demo script scored 0; cannot preview")
	}

	pc, _ := loadPlayer()
	canned := []board.Row{
		{Name: "BIFF", Score: 32100, DeviceID: "x"},
		{Name: "DAVE", Score: 12500, DeviceID: pc.DeviceID}, // "you"
		{Name: "KIM", Score: 9900, DeviceID: "z"},
	}
	ui := newScoreUI(nil, func() ([]board.Row, error) { return canned, nil })

	var frameG *engine.Game
	switch mode {
	case "ask":
		ui.tick(g) // game over auto-asks
	case "entry":
		ui.tick(g)
		ui.feedKeys([]byte("yDAVE")) // a half-typed name, cursor after it
		ui.tick(g)
		frameG = g
	case "board":
		// Direct board view (the submit path needs a real backend).
		ui.tick(g)
		ui.showBoard()
		time.Sleep(100 * time.Millisecond) // let the fake fetch land
		ui.tick(g)
	case "title-board":
		g2 := engine.NewGame(engine.DefaultLevels(), 40, engine.LevelHeight)
		ui.tick(g2)
		ui.showBoard()
		time.Sleep(100 * time.Millisecond)
		frameG = g2
		fmt.Fprint(w, render.FrameANSI(frameG, render.NewPalette(trueColor), ui.tick(frameG)))
		return nil
	default:
		return fmt.Errorf("unknown preview %q (want ask, entry, board, title-board)", mode)
	}
	if frameG == nil {
		frameG = g
	}
	fmt.Fprint(w, render.FrameANSI(frameG, render.NewPalette(trueColor), ui.tick(frameG)))
	return nil
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
