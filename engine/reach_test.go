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
