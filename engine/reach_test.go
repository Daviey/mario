package engine

import "testing"

// The reachability contract applies to every level, hand-built or
// generated: a small player must be able to collect every floating coin
// by running and jumping. Regression guard for the 2026-08-30 incident
// (daily coin shelves placed five tiles up, past the 4.40-tile jump).

func TestDefaultLevelsCoinsReachable(t *testing.T) {
	for _, l := range DefaultLevels() {
		if bad := unreachableCoins(l); len(bad) > 0 {
			t.Errorf("%s: unreachable coins at %v", l.Name, bad)
		}
		// Warp rooms are part of the level's contract: their coins must
		// be collectable from the room's own spawn too.
		for _, w := range l.Warps {
			if w.Dest == nil {
				continue
			}
			if bad := unreachableCoins(w.Dest); len(bad) > 0 {
				t.Errorf("%s room: unreachable coins at %v", l.Name, bad)
			}
		}
	}
}

func TestDailyLevelsCoinsReachable(t *testing.T) {
	// Every (month, day) pair matters: the seed is the raw date numbers,
	// so 08-30 and 08-31 are different levels, not the same as 08-02.
	for month := 1; month <= 12; month++ {
		for day := 1; day <= 31; day++ {
			l := DailyLevelFor(2026, month, day)
			if bad := unreachableCoins(l); len(bad) > 0 {
				t.Errorf("daily %02d-%02d: unreachable coins at %v", month, day, bad)
			}
		}
	}
}

// Completability: every shippable level must be finishable. No other
// test proves any level's goal reachable — the shape checks pin pits and
// stairs, the coin checks pin pickups, but only this proves the route
// from spawn to the goal (flag, or the boss arena's axe) exists under
// the real physics.
func TestDefaultLevelsFlagReachable(t *testing.T) {
	for _, l := range DefaultLevels() {
		if !flagReachable(l) {
			t.Errorf("%s: goal at column %d unreachable from the spawn by a small player", l.Name, l.GoalX())
		}
		// Rooms have no flag by construction; the enterable pipe must
		// still exist as real pipe tiles at the wired mouth.
		for _, w := range l.Warps {
			if l.At(w.X, w.Top) != Pipe || l.At(w.X+1, w.Top) != Pipe {
				t.Errorf("%s: warp mouth at (%d,%d) is not pipe tiles", l.Name, w.X, w.Top)
			}
			if w.Dest == nil {
				continue
			}
			if w.Dest.At(w.DestX, w.DestTop) != Pipe || w.Dest.At(w.DestX+1, w.DestTop) != Pipe {
				t.Errorf("%s room: warp mouth at (%d,%d) is not pipe tiles", l.Name, w.DestX, w.DestTop)
			}
		}
	}
}

// Sampled dailies: twelve dates spread across the year (the exhaustive
// date sweep is the coin test's job) prove the generator's segment
// grammar never walls the flag route off.
func TestDailyLevelsFlagReachable(t *testing.T) {
	dates := [][2]int{{1, 1}, {2, 3}, {3, 7}, {4, 18}, {5, 29}, {6, 15},
		{7, 23}, {8, 31}, {9, 9}, {10, 2}, {11, 11}, {12, 25}}
	for _, md := range dates {
		l := DailyLevelFor(2026, md[0], md[1])
		if !flagReachable(l) {
			t.Errorf("daily %02d-%02d: flag at column %d unreachable from the spawn", md[0], md[1], l.FlagX)
		}
	}
}

// Negative control: a floor-to-sky wall before the flag must read
// unreachable — without this, a silently broken flood (say, scripts that
// teleport or a grab test that always fires) would make the reachability
// suite vacuously green.
func TestFlagReachableNegativeControl(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) {
		b.Fill(30, 0, 30, GroundTop-1, '#') // wall between spawn and the flag at 55
	})
	if flagReachable(l) {
		t.Error("flag reported reachable through a floor-to-sky wall")
	}
}
