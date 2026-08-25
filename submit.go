package main

// Post-game score submission: prompt (cooked terminal, after run() restored
// it), then insert an unverified row. The run only reaches the board once the
// verifier replays it and the score reproduces.

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"mario/board"
)

// requestTimeout bounds every leaderboard HTTP call.
const requestTimeout = 15 * time.Second

// writeRecording saves a recording as JSON (used by -demo-recording and
// debugging).
func writeRecording(path string, rec *recorder) error {
	data, err := json.Marshal(rec.rec)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// maybeSubmit offers to upload a finished run. stdin is expected to be a
// cooked terminal; EOF or a non-TTY just skips the prompt. defaultLevels is
// false for custom-level runs, which cannot be replayed by the verifier.
func maybeSubmit(out io.Writer, in io.Reader, res *runResult, defaultLevels bool) error {
	if res == nil || res.score <= 0 || !defaultLevels || !res.rec.submittable() {
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
	fmt.Fprintf(out, "\nscore %d — submit to the leaderboard? [Y/n] ", res.score)
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

	replay, err := json.Marshal(res.rec.rec)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()
	if err := client.Submit(ctx, board.Entry{
		Name:          name,
		Score:         res.score,
		Replay:        replay,
		EngineVersion: scoreEngineVersion,
		DeviceID:      player.DeviceID,
	}); err != nil {
		return err
	}
	if name != player.Name {
		player.saveName(name)
	}
	fmt.Fprintln(out, "submitted — pending verification; it appears on the board once replayed (usually <15 min)")
	return nil
}
