package engine

import "testing"

// The multi-coin brick (SMB1's ten-coin block): a coin per bump — small
// or super, it never breaks from below — until ten coins are paid or
// the first bump's ~4s window closes, whichever comes first; then the
// brick spends to Used. A sliding shell smashes it like any brick,
// forfeiting the coins.

func TestMultiCoinBrickPaysTenThenSpends(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Set(10, 9, 'C') })
	g := newGame(t, l)
	for want := 1; want <= MultiCoinCount; want++ {
		bumpUnder(t, g, 10, 9)
		// Let the bump animation decay (bumpUnder returns the moment it
		// starts) and the player land before the next re-stage.
		run(g, BumpAnimTicks+2, Input{})
		if g.CoinCount != want {
			t.Fatalf("bump %d: coins = %d, want %d", want, g.CoinCount, want)
		}
		if want < MultiCoinCount {
			if got := g.Level.At(10, 9); got != BrickCoin {
				t.Fatalf("bump %d: tile = %v, want BrickCoin still live", want, got)
			}
		}
	}
	if got := g.Level.At(10, 9); got != Used {
		t.Fatalf("tile = %v after the tenth coin, want Used", got)
	}
	if g.Score != MultiCoinCount*CoinScore {
		t.Errorf("score = %d, want %d", g.Score, MultiCoinCount*CoinScore)
	}
	// Spent is spent: another bump pays nothing.
	bumpUnder(t, g, 10, 9)
	if g.CoinCount != MultiCoinCount {
		t.Errorf("coins = %d after bumping the spent brick, want still %d", g.CoinCount, MultiCoinCount)
	}
}

func TestMultiCoinBrickTimesOut(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Set(10, 9, 'C') })
	g := newGame(t, l)
	bumpUnder(t, g, 10, 9)
	if g.CoinCount != 1 {
		t.Fatalf("first bump paid %d coins, want 1", g.CoinCount)
	}
	// The window closes with the brick unattended: it spends itself.
	run(g, MultiCoinTicks+5, Input{})
	if got := g.Level.At(10, 9); got != Used {
		t.Fatalf("tile = %v after the window closed, want Used", got)
	}
	if g.CoinCount != 1 {
		t.Errorf("coins = %d, want 1 (timeout pays nothing)", g.CoinCount)
	}
}

func TestShellSmashesMultiCoinBrick(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Set(20, 12, 'C') })
	g := newGame(t, l)
	shell := kickedShell(g, 15, 1)
	for t := 0; t < 300 && shell.Pos.X < 22; t++ {
		g.Update(Input{})
	}
	if got := g.Level.At(20, 12); got != Empty {
		t.Fatalf("tile = %v, want smashed to Empty", got)
	}
	if g.CoinCount != 0 {
		t.Errorf("coins = %d, want 0 (a shell break forfeits the coins)", g.CoinCount)
	}
	if g.Score != BrickScore {
		t.Errorf("score = %d, want %d (plain brick score)", g.Score, BrickScore)
	}
}

// TestShellSmashDropsArmedCoinBrick guards the ghost-block regression:
// a multi-coin brick whose timer was armed by a bump, then smashed by
// a shell, must vanish for good — its map entry dies with the tile, or
// the decay loop would later spend a solid Used block into mid-air
// where the brick stood (live bug on 74d4078, reachable on 3-1).
func TestShellSmashDropsArmedCoinBrick(t *testing.T) {
	l := buildLevel(t, 60, func(b *Builder) { b.Set(20, 12, 'C') })
	g := newGame(t, l)
	// Row 12 sits inside a standing player's body band, so a staged
	// bumpUnder cannot reach it cleanly; drive the production bump
	// entry directly to arm the window (the state under test).
	g.hitBlock(20, 12, g.Player)
	if g.CoinCount != 1 {
		t.Fatalf("setup: coins = %d, want 1", g.CoinCount)
	}
	shell := kickedShell(g, 15, 1)
	for i := 0; i < 300 && shell.Pos.X < 22; i++ {
		g.Update(Input{})
	}
	if got := g.Level.At(20, 12); got != Empty {
		t.Fatalf("immediately after smash: tile = %v, want Empty", got)
	}
	// Long past the armed window: nothing may materialize there.
	run(g, 350, Input{})
	if got := g.Level.At(20, 12); got != Empty {
		t.Fatalf("ghost block: tile = %v after the window expired, want Empty", got)
	}
	if g.CoinCount != 1 {
		t.Errorf("coins = %d after the smash, want still 1", g.CoinCount)
	}
}
