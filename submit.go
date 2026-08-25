package main

// Post-game score submission: prompt (cooked terminal, after run() restored
// it), then insert the row — it appears on the board immediately. Scores
// are client-attested; there is no verification layer.

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"mario/board"
)

// requestTimeout bounds every leaderboard HTTP call.
const requestTimeout = 15 * time.Second

// maybeSubmit offers to upload a finished score. stdin is expected to be a
// cooked terminal; EOF just skips the prompt.
func maybeSubmit(out io.Writer, in io.Reader, score int) error {
	if score <= 0 {
		return nil
	}
	client, err := board.FromEnv()
	if err != nil {
		// No leaderboard configured: stay quiet, the game stands alone.
		return nil
	}
	player, err := loadPlayer()
	if err != nil {
		return fmt.Errorf("player config: %w", err)
	}

	rd := bufio.NewReader(in)
	fmt.Fprintf(out, "\nscore %d — submit to the leaderboard? [Y/n] ", score)
	ans, _ := rd.ReadString('\n')
	ans = strings.ToLower(strings.TrimSpace(ans))
	if ans == "n" || ans == "no" {
		fmt.Fprintln(out, "not submitted")
		return nil
	}

	name := player.Name
	for {
		def := ""
		if name != "" {
			def = fmt.Sprintf(" [%s]", name)
		}
		fmt.Fprintf(out, "name%s: ", def)
		line, err := rd.ReadString('\n')
		if err != nil && line == "" {
			return nil // EOF: drop out quietly
		}
		line = strings.TrimSpace(line)
		if line == "" && name != "" {
			break // keep stored/default name
		}
		if s, ok := sanitizeName(line); ok {
			name = s
			break
		}
		fmt.Fprintln(out, "1-8 chars, letters/digits/space . - _ only")
	}

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	if err := client.Submit(ctx, board.Entry{
		Name:     name,
		Score:    score,
		DeviceID: player.DeviceID,
	}); err != nil {
		return err
	}
	if name != player.Name {
		player.saveName(name)
	}
	fmt.Fprintln(out, "submitted — run with -scores to see the board")
	return nil
}

// printScores renders the leaderboard as a plain text table.
func printScores(w io.Writer, rows []board.Row) {
	if len(rows) == 0 {
		fmt.Fprintln(w, "no scores yet — be the first!")
		return
	}
	fmt.Fprintf(w, "%4s  %-8s %9s  %s\n", "#", "NAME", "SCORE", "WHEN")
	for i, r := range rows {
		fmt.Fprintf(w, "%4d  %-8s %9d  %s\n", i+1, r.Name, r.Score, r.CreatedAt.Local().Format("2006-01-02 15:04"))
	}
}
