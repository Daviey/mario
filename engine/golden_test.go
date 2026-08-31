package engine

import (
	"fmt"
	"hash/fnv"
	"testing"

	"github.com/Daviey/mario/board"
)

// Golden level fixtures: the replay verifier byte-compares leaderboard
// scores against board.EngineVersion, so level bytes may only change
// together with a version bump. These digests pin one built-in level and
// one fixed daily seed to the version: an accidental gameplay change
// without a bump fails here, long before it can diverge a replay.

// levelDigest FNV-hashes every byte that defines a level's gameplay: the
// tile grid, every spawn list in a fixed order, the flag column and the
// player start. It deliberately excludes the display name.
func levelDigest(l *Level) uint64 {
	h := fnv.New64a()
	for _, t := range l.Tiles {
		h.Write([]byte{byte(t)})
	}
	vecs := func(vs []Vec) {
		for _, v := range vs {
			fmt.Fprintf(h, "|%v", v)
		}
	}
	vecs(l.GoombaSpawns)
	vecs(l.KoopaSpawns)
	vecs(l.ParaSpawns)
	vecs(l.CoinSpawns)
	vecs(l.PlantSpawns)
	for _, fb := range l.BarSpawns {
		fmt.Fprintf(h, "|%v,%v", fb.X, fb.Y)
	}
	fmt.Fprintf(h, "|flag=%d,start=%v", l.FlagX, l.PlayerStart)
	return h.Sum64()
}

func TestLevelBytesPinnedToEngineVersion(t *testing.T) {
	for _, tc := range []struct {
		name  string
		level *Level
		want  uint64
	}{
		{"1-1", level1(), 0x1d797ae0096ba9b5},
		{"daily 2026-02-03", DailyLevelFor(2026, 2, 3), 0x1a8e7be7c146cfee},
	} {
		got := levelDigest(tc.level)
		t.Logf("engine version %s: %s digest %016x", board.EngineVersion, tc.name, got)
		if got != tc.want {
			t.Errorf("%s: level bytes changed under engine version %s: digest %016x, want %016x — "+
				"if this is an intended gameplay change, bump board.EngineVersion and refresh the digest; "+
				"if not, the engine changed level generation by accident",
				tc.name, board.EngineVersion, got, tc.want)
		}
	}
}
