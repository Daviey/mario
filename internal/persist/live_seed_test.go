package persist

// Seeds one real leaderboard row so the in-game board has content to
// display. Skipped unless LIVE=1 (write path — the row is named SEEDCHK,
// delete it from the scores table when done).
import (
	"context"
	"os"
	"testing"
	"time"

	"mario/board"
)

func TestLiveSeedOneRow(t *testing.T) {
	if os.Getenv("LIVE") != "1" {
		t.Skip("set LIVE=1")
	}
	board.LoadDotEnv(".env")
	c, err := board.FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pc, _ := LoadPlayer()
	if err := c.Submit(ctx, board.Entry{Name: "SEEDCHK", Score: 12300, DeviceID: pc.DeviceID}); err != nil {
		t.Fatalf("submit: %v", err)
	}
	rows, err := c.Top(ctx, 10, pc.DeviceID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Fatal("no rows after seed")
	}
	t.Logf("rows: %d, top: %+v", len(rows), rows[0])
}
