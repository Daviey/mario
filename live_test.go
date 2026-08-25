package main

// Live integration test: exercises the real submit path against the
// configured Supabase project. Skipped unless LIVE=1:
//
//	LIVE=1 go test -run TestLiveSubmitAndTop -v .
//
// Requires SUPABASE_URL/SUPABASE_KEY in the environment or ./.env.

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"mario/board"
)

func TestLiveSubmitAndTop(t *testing.T) {
	if os.Getenv("LIVE") != "1" {
		t.Skip("set LIVE=1 to hit the real backend")
	}
	board.LoadDotEnv(".env")
	client, err := board.FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	player, err := loadPlayer()
	if err != nil {
		t.Fatal(err)
	}
	name := "LIVETST2"
	if err := client.Submit(ctx, board.Entry{Name: name, Score: 7777, DeviceID: player.DeviceID}); err != nil {
		t.Fatalf("submit: %v", err)
	}

	rows, err := client.Top(ctx, 10)
	if err != nil {
		t.Fatalf("top: %v", err)
	}
	found := false
	var out strings.Builder
	printScores(&out, rows)
	for _, r := range rows {
		if r.Name == name && r.Score == 7777 {
			found = true
		}
	}
	if !found {
		t.Fatalf("submitted score not immediately visible:\n%s", out.String())
	}
	t.Logf("board now shows %s:\n%s", name, out.String())
}
