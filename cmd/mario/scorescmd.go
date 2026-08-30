package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/Daviey/mario/board"
)

// runTopScores fetches and prints the top-n leaderboard — the daily
// board for today when daily — for the -scores flag (no TUI).
func runTopScores(client *board.Client, n int, daily bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var rows []board.Row
	var err error
	if daily {
		rows, err = client.TopMode(ctx, n, "", "daily", time.Now().UTC().Format("2006-01-02"))
	} else {
		rows, err = client.Top(ctx, n, "")
	}
	if err != nil {
		return err
	}
	printScores(os.Stdout, rows)
	return nil
}

// printScores renders the leaderboard as a text table.
func printScores(w io.Writer, rows []board.Row) {
	if len(rows) == 0 {
		fmt.Fprintln(w, "no scores yet — be the first!")
		return
	}
	fmt.Fprintf(w, "%4s  %-8s %9s  %3s  %s\n", "#", "NAME", "SCORE", "LVL", "WHEN")
	for i, r := range rows {
		fmt.Fprintf(w, "%4d  %-8s %9d  %3d  %s\n", i+1, r.Name, r.Score, r.Level, r.CreatedAt.Local().Format("2006-01-02 15:04"))
	}
}
