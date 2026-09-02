package engine

// Coin reachability: the fairness contract every level must satisfy. A
// floating coin is only fair if a small, unpowered player can collect it
// by ordinary play — running and jumping from terrain they can actually
// stand on, starting at the spawn. Coins that need super-Mario brick
// breaking or an enemy bounce chain do not count: power-ups and enemies
// are not guaranteed to still be there when the player arrives.
//
// The check drives the engine's own player physics (updatePlayer) on a
// scratch copy of the level, so the verdict can never drift from the
// real simulation. (The 2026-08-30 incident: both daily coin emitters
// could place shelves five tiles above the floor — 0.6 tiles beyond the
// 4.40-tile jump apex — making whole coin rows decorative.)

type stand struct{ tx, ty int }

// unreachableCoins reports every coin spawn of l that a small player
// cannot collect, as spawn positions (top-left of the coin box).
//
// It flood-fills the tiles a small player can stand on, starting from
// the spawn surface: from every reachable surface it runs the same
// scripts a player has available (run or walk, either direction, one
// full jump immediately or after a short run-up, or no jump at all) and
// records every tile the runs land on as newly reachable. Any coin a
// run touches along the way is collectible.
func unreachableCoins(l *Level) []Vec {
	g := NewGame([]*Level{l}, 40, 15)
	g.Enemies = nil // enemies move; reachability must not depend on them
	g.Plants = nil
	g.FireBars = nil
	g.CoinItems = nil

	coins := l.CoinSpawns
	touched := make([]bool, len(coins))

	visited := map[stand]bool{}
	root := spawnSurface(l)
	visited[root] = true
	queue := []stand{root}
	done := 0 // coins touched so far; the flood stops once all are safe

	onTick := func(p *Player) {
		for i, c := range coins {
			if !touched[i] && overlap(p.Pos.X, p.Pos.Y, p.W, p.H, c.X, c.Y, CoinSize, CoinSize) {
				touched[i] = true
				done++
			}
		}
		if p.Grounded {
			ty := int(p.Pos.Y + p.H + 0.1)
			for _, fx := range [2]float64{p.Pos.X + 0.1, p.Pos.X + p.W - 0.1} {
				if s := (stand{int(fx), ty}); !visited[s] {
					visited[s] = true
					queue = append(queue, s)
				}
			}
		}
	}

	for len(queue) > 0 && done < len(coins) {
		s := queue[0]
		queue = queue[1:]
		// Precision landings only matter near a coin: far from any
		// untouched coin, full scripts are wasted transit. The short
		// set still runs (full-speed jumps and plain runs) so the
		// flood keeps crossing pits and mounting terrain everywhere.
		nearCoin := false
		for i, c := range coins {
			if !touched[i] && abs(int(c.X)-s.tx) <= 6 {
				nearCoin = true
				break
			}
		}
		var scripts [][2]int // {jumpAt, cutAt}; -1: never
		if nearCoin {
			scripts = [][2]int{
				{0, -1}, {8, -1}, {16, -1}, // full jumps, three run-ups
				{-1, -1},                 // plain run (walk-offs, falls)
				{0, 4}, {0, 8}, {16, 20}, // cut hops for coins under ceilings
			}
		} else {
			scripts = [][2]int{{16, -1}, {-1, -1}} // transit: full-speed jump or run
		}
		runs := []bool{true, false}
		if !nearCoin {
			runs = []bool{true} // walk precision only matters near coins
		}
		for _, dir := range []int{-1, 1} {
			for _, run := range runs {
				for _, v := range scripts {
					script(g, s, dir, run, v[0], v[1], onTick)
				}
			}
		}
	}

	var bad []Vec
	for i, ok := range touched {
		if !ok {
			bad = append(bad, coins[i])
		}
	}
	return bad
}

// flagReachable reports whether a small, unpowered player can reach the
// level's goal — the flagpole, or the axe in the boss arena — from the
// spawn by ordinary play: the completability contract every shippable
// level must satisfy. A goal no amount of running and jumping can touch
// makes the level unwinnable. It flood-fills the same standable-tile
// graph as unreachableCoins with the same movement scripts (the
// engine's own updatePlayer), watching for the engine's grab test: the
// player's right edge past the goal column.
func flagReachable(l *Level) bool {
	goal := l.GoalX()
	if goal < 0 {
		return true // no goal (warp rooms): nothing to complete
	}
	g := NewGame([]*Level{l}, 40, 15)
	g.Enemies = nil  // same fairness contract as the coin check: enemies
	g.Plants = nil   // and hazards move, so completion must never depend
	g.FireBars = nil // on where they happen to sit when the player arrives
	g.CoinItems = nil
	g.Bowsers = nil // the boss included

	grabX := float64(goal) + 0.5 // updatePlaying's goal-grab threshold
	reached := false
	visited := map[stand]bool{}
	root := spawnSurface(l)
	visited[root] = true
	queue := []stand{root}

	onTick := func(p *Player) {
		if !reached && p.Pos.X+p.W >= grabX {
			reached = true
		}
		if p.Grounded {
			ty := int(p.Pos.Y + p.H + 0.1)
			for _, fx := range [2]float64{p.Pos.X + 0.1, p.Pos.X + p.W - 0.1} {
				if s := (stand{int(fx), ty}); !visited[s] {
					visited[s] = true
					queue = append(queue, s)
				}
			}
		}
	}

	for len(queue) > 0 && !reached {
		s := queue[0]
		queue = queue[1:]
		// Precision landings only matter near the goal (the final
		// staircase and the grab itself); far from it the transit set —
		// full-speed jump or plain run — still crosses pits and mounts
		// terrain everywhere.
		nearGoal := abs(goal-s.tx) <= 14
		var scripts [][2]int // {jumpAt, cutAt}; -1: never
		if nearGoal {
			scripts = [][2]int{
				{0, -1}, {8, -1}, {16, -1}, // full jumps, three run-ups
				{-1, -1},                 // plain run (walk-offs, falls)
				{0, 4}, {0, 8}, {16, 20}, // cut hops under a low ceiling
			}
		} else {
			scripts = [][2]int{{16, -1}, {-1, -1}}
		}
		runs := []bool{true, false}
		if !nearGoal {
			runs = []bool{true}
		}
		for _, dir := range []int{-1, 1} {
			for _, run := range runs {
				for _, v := range scripts {
					script(g, s, dir, run, v[0], v[1], onTick)
					if reached {
						return true
					}
				}
			}
		}
	}
	return false
}

// script runs one scripted try on g: a small player starts standing on
// the surface s, holds walk or run toward dir and — from tick jumpAt —
// presses jump. The jump is held for the whole flight unless cutAt is
// reached first: releasing early triggers the engine's JumpCut, the short
// hop a player uses to grab a coin under a low overhang without bonking
// the ceiling. Either value may be -1 (never jump / never cut).
func script(g *Game, s stand, dir int, run bool, jumpAt, cutAt int, onTick func(*Player)) {
	p := newPlayer(Vec{float64(s.tx) + 0.1, float64(s.ty - 1)}, PowerSmall)
	p.Facing = dir
	g.Player = p
	g.prevIn = Input{} // so the first scripted press registers as an edge
	for t := 0; t < 160; t++ {
		in := Input{}
		if run {
			in.Run = true
		}
		if dir < 0 {
			in.Left = true
		} else {
			in.Right = true
		}
		if t >= jumpAt && (cutAt < 0 || t < cutAt) {
			in.Up = true
		}
		g.updatePlayer(in)
		g.prevIn = in
		onTick(p)
		if p.Pos.Y > float64(g.Level.Height)+1 {
			return // fell out of the level; the script is spent
		}
	}
}

// spawnSurface returns the standable tile under the player spawn.
func spawnSurface(l *Level) stand {
	tx := int(l.PlayerStart.X)
	ty := min(l.Height-1, int(l.PlayerStart.Y+SmallH))
	for ; ty < l.Height; ty++ {
		if l.At(tx, ty).Solid() && !l.At(tx, ty-1).Solid() {
			return stand{tx, ty}
		}
	}
	return stand{tx, l.Height - 1}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
