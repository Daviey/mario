package main

// Plain-text leaderboard rendering for the -scores flag (no TUI).

import (
	"fmt"
	"io"

	"github.com/Daviey/mario/board"
)

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
