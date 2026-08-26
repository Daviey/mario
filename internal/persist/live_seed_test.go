package persist

// Seeds one real leaderboard row so the in-game board has content to
// display. Skipped unless LIVE=1 (write path — the row is named SEEDCHK,
// delete it from the scores table when done).
import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/Daviey/mario/board"
	"github.com/Daviey/mario/engine"
	"github.com/Daviey/mario/replay"
)

func TestLiveSeedOneRow(t *testing.T) {
	if os.Getenv("LIVE") != "1" {
		t.Skip("set LIVE=1")
	}
	board.LoadDotEnv("../../.env", "../.env", ".env")
	c, err := board.FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pc, _ := LoadPlayer()
	levels := engine.DefaultLevels()
	g := engine.NewGame(levels, 20, engine.LevelHeight)
	var rec replay.Recorder
	for i := range 6000 {
		in := engine.Input{Right: true, Run: i%3 != 0, Up: i%97 < 22, AnyKey: i == 0}
		g.Update(in)
		if i == 1 {
			rec.Start()
		}
		if i >= 1 {
			rec.Record(in)
		}
	}
	rec.Finish()
	if res, err := replay.Run(levels, "classic", rec.JSON()); err != nil || res.Score != g.Score {
		t.Fatalf("seed replay must reproduce the run: %v %+v vs %d", err, res, g.Score)
	}
	if err := c.Submit(ctx, board.Entry{Name: "SEEDCHK", Score: g.Score, Level: g.LevelIndex() + 1, DeviceID: pc.DeviceID, Replay: rec.JSON()}); err != nil {
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
