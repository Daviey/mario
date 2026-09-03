package engine

// World 4 (contract: SMB1 worlds 4-8 land one world per release; world
// 4 un-pins the 1-2 warp zone's third pipe and moves the quest's end —
// the princess — to 4-4, flipping 3-4's retainer to the toad's
// "another castle" beat, exactly the original's ladder of endings).

// level13 is 4-1: the paratroopa gauntlet — hopping greens in pairs the
// whole way, a red ledge-koopa roost, the springboard over a four-brick
// wall to the high coin shelf, and the shell-bowling koopa row before
// the staircase.
func level13() *Level {
	b := NewBuilder(180, LevelHeight)
	b.Ground(0, 37)
	b.Ground(42, 89)  // pit 38-41
	b.Ground(94, 179) // pit 90-93
	b.Set(3, 12, 'M')

	// The gauntlet opens on hopping paratroopa pairs.
	b.Set(12, 12, 'W')
	b.Set(15, 12, 'W')
	b.Set(18, 12, 'G')
	b.Set(20, 12, 'G')

	// The opening question row: the right-most pays the power-up.
	b.Set(24, 9, '?')
	b.Set(26, 9, 'U')
	b.Set(28, 9, '?')

	// The red koopa's roost: a brick ledge a ledge-turner patrols.
	b.Fill(32, 9, 35, 9, 'B')
	b.Set(33, 8, 'R')

	// Plant pipes pace the middle third.
	b.Pipe(44, 2)
	b.Plant(44, 2)
	b.Set(48, 12, 'W')
	b.Set(52, 12, 'W')

	// The springboard wall: four bricks tall — a small player can just
	// climb it with a running jump — with the springboard out front as
	// the easy hop and the coin shelf above the far side.
	b.Springboard(58, 12)
	b.Fill(61, 9, 61, 12, 'B')
	b.Fill(64, 5, 68, 5, 'B')
	b.Coins(4, 65, 66, 67)
	b.Set(55, 12, 'G')

	// The shell-bowling row: kick the first, collect the chain.
	b.Set(72, 12, 'K')
	b.Set(75, 12, 'K')
	b.Set(78, 12, 'K')
	b.Pipe(84, 3)
	b.Plant(84, 3)

	// The pit crossing is watched by a flying red.
	b.Set(88, 7, 'r')

	// The far bank: the multi-coin brick hides in the row.
	b.Fill(100, 9, 104, 9, 'B')
	b.Set(102, 9, 'C')
	b.Set(101, 12, 'G')
	b.Set(103, 12, 'G')
	b.Set(108, 5, '1') // the hidden 1-UP over the brick row's right end

	// More paratroopa pairs on the home stretch.
	b.Set(112, 12, 'W')
	b.Set(115, 12, 'W')
	b.Pipe(120, 2)
	b.Plant(120, 2)
	b.Set(124, 9, 'S') // the starman brick for the closing rush
	b.Coins(9, 128, 129, 130)

	// The closing gauntlet and the staircase.
	b.Set(132, 12, 'G')
	b.Set(134, 12, 'G')
	b.Set(136, 12, 'K')
	b.Coins(9, 140, 141, 142, 143)
	b.StairsUp(148, 8)
	b.Flag(164)
	l := mustLevel("4-1", b)
	l.Time = 400
	return l
}

// level14 is 4-2: the underground vine cellar — the beanstalk brick
// early (the original hides it in plain sight), its Coin Heaven above,
// the shell-bowling stretch after the pit, and the exit pipe trading
// the cave for the overworld flag room (2-2's shape).
func level14() *Level {
	b := NewBuilder(190, LevelHeight)
	b.Theme(ThemeUnderground)
	b.Ground(0, 59)
	b.Ground(64, 189) // pit 60-63
	b.Ceiling()
	b.Set(3, 12, 'M')

	// The opening brick row: coin, power-up, coin.
	b.Set(10, 9, 'B')
	b.Set(11, 9, '?')
	b.Set(12, 9, 'U')
	b.Set(13, 9, '?')
	b.Set(14, 9, 'B')
	b.Set(16, 12, 'G')
	b.Set(18, 12, 'G')

	// THE VINE BRICK: bump, climb, Coin Heaven (level14CoinHeaven).
	b.Set(22, 9, 'J')
	b.Set(26, 12, 'K')
	b.Set(28, 12, 'K')

	// The hidden coin block paces the mid stretch.
	b.Set(34, 9, 'H')
	b.Fill(38, 9, 43, 9, 'B')
	b.Set(40, 9, 'U')
	b.Set(46, 12, 'G')
	b.Set(48, 12, 'W')
	b.Set(52, 12, 'G')
	b.Coins(9, 54, 55, 56)

	// The pit crossing, then the shell-bowling row on the far bank.
	b.Set(58, 7, 'r')
	b.Set(68, 12, 'K')
	b.Set(71, 12, 'K')
	b.Set(74, 12, 'K')

	// The heaven's drop lands here (column 100): a coin run greets the
	// faller, then the cave's last stretch.
	b.Coins(10, 98, 99, 100, 101, 102)
	b.Fill(106, 9, 110, 9, 'B')
	b.Set(108, 9, 'C') // the row's multi-coin brick
	b.Set(112, 12, 'W')
	b.Set(114, 12, 'W')
	b.Set(118, 12, 'G')
	b.Pipe(124, 3)
	b.Plant(124, 3)
	b.Set(130, 12, 'K')
	b.Set(133, 12, 'G')
	b.Set(136, 12, 'G')
	b.Set(140, 9, '?')
	b.Set(142, 9, 'f') // the fire flower for the road out
	b.Coins(9, 146, 147, 148)
	b.Set(152, 12, 'W')
	b.Set(155, 12, 'K')
	b.Set(158, 12, 'W')
	b.Coins(9, 162, 163, 164)

	// The exit pipe: 2-2's shape — warp to the overworld flag room.
	b.Pipe(176, 3)
	// The late warp zone (the original hides it behind the second
	// beanstalk; ours is a plant pipe before the exit — worlds 6, 7
	// and 8, the roof-pocket pattern).
	b.Pipe(162, 2)
	b.Plant(162, 2)
	l := mustLevel("4-2", b)
	l.Time = 400
	room := level14FlagRoom()
	l.Warps = []Warp{
		{X: 162, Top: GroundTop - 2, Dest: level14WarpRoom(), DestX: 2, DestTop: GroundTop - 2},
		{X: 176, Top: GroundTop - 3, Dest: room, DestX: 2, DestTop: GroundTop - 2},
	}
	l.VineRoom = level14CoinHeaven()
	return l
}

// level14WarpRoom is 4-2's warp zone: worlds 6, 7 and 8 left to right
// (the original's second warp zone, unlocked by worlds 6-8 existing).
func level14WarpRoom() *Level {
	b := NewBuilder(26, LevelHeight)
	b.Theme(ThemeUnderground)
	b.Ground(0, 25)
	b.Ceiling()
	for y := 1; y < GroundTop; y++ { // brick walls close the pocket
		b.Set(0, y, 'B')
		b.Set(25, y, 'B')
	}
	b.Pipe(2, 2)  // arrival
	b.Pipe(8, 2)  // world 6
	b.Pipe(14, 2) // world 7
	b.Pipe(20, 2) // world 8
	b.Set(6, 12, 'M')
	l := mustLevel("4-2 warp", b)
	l.Warps = []Warp{
		{X: 8, Top: GroundTop - 2, JumpTo: 20, DestX: 2, DestTop: GroundTop - 2},
		{X: 14, Top: GroundTop - 2, JumpTo: 24, DestX: 2, DestTop: GroundTop - 2},
		{X: 20, Top: GroundTop - 2, JumpTo: 28, DestX: 2, DestTop: GroundTop - 2},
	}
	return l
}

// level14FlagRoom is 4-2's overworld exit: rise out of the pipe, cross
// the short surface stretch to the flagpole (2-2's flag-room shape).
func level14FlagRoom() *Level {
	b := NewBuilder(44, LevelHeight)
	b.Ground(0, 43)
	b.Pipe(2, 2)
	b.Coins(9, 20, 21, 22)
	b.StairsUp(28, 4)
	b.Flag(38)
	b.Set(6, 12, 'M')
	return mustLevel("4-2", b)
}

// level14CoinHeaven is 4-2's beanstalk room: its own template (rooms
// cache live state per pointer — sharing 1-1's would bleed collected
// coins between the two levels), a wider harvest than 1-1's, and the
// drop back into the cave at column 100, past the pit.
func level14CoinHeaven() *Level {
	b := NewBuilder(24, LevelHeight)
	b.Theme(ThemeSky)
	b.Ground(0, 15) // the ledge: open sky from column 16 rightward
	for y := 1; y < GroundTop; y++ {
		b.Set(0, y, 'B') // brick wall closes the left edge
	}
	b.Coins(10, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11)
	b.Coins(7, 8, 9, 10, 11)
	l := mustLevel("4-2 heaven", b)
	l.DropExitX = 100
	return l
}

// level15 is 4-3: the day-lit athletic flight — island hops under
// horizontal lifts, the flimsy board up to the brick coin run, and the
// balance-lift pair over the last gap, red koopas on every island.
func level15() *Level {
	b := NewBuilder(200, LevelHeight)
	b.Theme(ThemeSky)
	b.Ground(0, 15)
	b.Ground(20, 47)   // pit 16-19
	b.Ground(52, 79)   // pit 48-51 (lift)
	b.Ground(84, 111)  // pit 80-83 (flimsy lift)
	b.Ground(116, 151) // pit 112-115 (balance lifts)
	b.Ground(156, 199) // pit 152-155 (lift)
	b.Set(3, 12, 'M')

	b.Set(8, 12, 'R')
	b.Set(12, 12, 'G')

	// First island pair: the red pair and the power-up ledge.
	b.Set(24, 12, 'R')
	b.Fill(28, 9, 31, 9, 'B')
	b.Set(29, 9, 'U')
	b.Set(36, 12, 'G')
	b.Set(40, 12, 'R')
	b.Coins(9, 42, 43, 44)

	// The horizontal lift over the first gap, coins above its sweep.
	b.Lift(49, 10, 3, LiftHoriz, 4)
	b.Coins(7, 48, 49, 50, 51)

	// The mid island: the starman brick behind the koopa.
	b.Set(56, 9, 'S')
	b.Set(60, 12, 'R')
	b.Set(64, 12, 'G')
	b.Set(68, 12, 'R')
	b.Coins(9, 72, 73, 74)

	// The flimsy lift: board it, jump quick — it falls — up to the
	// high brick path and its coin run.
	b.Lift(81, 9, 3, LiftFlimsy, 0)
	b.Fill(86, 6, 94, 6, 'B')
	b.Coins(5, 87, 88, 89, 90, 91, 92, 93)

	// The long island: the hidden 1-UP over the brick row.
	b.Fill(98, 9, 103, 9, 'B')
	b.Set(101, 5, '1')
	b.Set(100, 12, 'R')
	b.Set(106, 12, 'G')
	b.Coins(9, 108, 109, 110)

	// The balance pair over the wide gap: standing on one lowers it
	// and raises the other.
	b.Lift(113, 9, 3, LiftPulley, 3)
	b.Lift(120, 9, 3, LiftPulley, 3)

	// The home islands: red koopas and the closing coin runs.
	b.Set(126, 12, 'R')
	b.Coins(9, 130, 131, 132)
	b.Set(136, 12, 'G')
	b.Set(140, 12, 'R')
	b.Lift(153, 10, 3, LiftHoriz, 4)
	b.Coins(7, 152, 153, 154, 155)
	b.Set(162, 12, 'R')
	b.Set(166, 12, 'G')
	b.StairsUp(176, 8)
	b.Flag(192)
	l := mustLevel("4-3", b)
	l.Time = 300
	return l
}

// level16 is world 4's castle: the fire-bar gauntlet with the original's
// repeating maze corridor restored mid-hall (maze.go) — before the fake
// Bowser (a goomba, as at the quest's start) on his bridge and the axe.
// Toad waits behind the arena: the princess is in another castle.
func level16() *Level {
	b := NewBuilder(180, LevelHeight)
	b.Theme(ThemeCastle)
	b.Ground(0, 29)
	b.Ground(33, 58)   // lava 30-32
	b.Ground(62, 87)   // lava 59-61
	b.Ground(91, 116)  // lava 88-90
	b.Ground(120, 145) // lava 117-119
	b.Ground(149, 179) // lava 146-148
	b.Ceiling()
	b.Fill(30, 13, 32, 14, 'L')
	b.Fill(59, 13, 61, 14, 'L')
	b.Fill(88, 13, 90, 14, 'L')
	b.Fill(117, 13, 119, 14, 'L')
	b.Fill(146, 13, 148, 14, 'L')
	b.Set(3, 12, 'M')

	// Every pool boils.
	b.Set(31, 13, 'o')
	b.Set(60, 13, 'o')
	b.Set(89, 13, 'o')
	b.Set(118, 13, 'o')
	b.Set(147, 13, 'o')

	// Fire bars: a brick pillar with a rotating hub on top.
	pillar := func(x int) {
		b.Fill(x, 11, x, 12, 'B')
		b.Set(x, 10, 'h')
	}

	// The opening hall: the power-up row before the first pool.
	b.Set(8, 9, '?')
	b.Set(10, 9, 'U')
	b.Set(12, 9, '?')
	b.Set(14, 12, 'G')
	b.Set(16, 12, 'G')
	pillar(21)
	pillar(26)

	// The long hall: pillar pairs between every pool, coins over the
	// crossings, koopas walking the ledges.
	b.Set(36, 12, 'K')
	pillar(40)
	pillar(45)
	b.Coins(9, 50, 51, 52)
	b.Set(54, 12, 'W')
	pillar(65)
	pillar(70)
	b.Fill(74, 9, 78, 9, 'B')
	b.Set(76, 9, 'U')
	b.Coins(8, 74, 75, 76, 77, 78)
	b.Set(82, 12, 'K')
	// The maze retrofit (2026-09-03): the mid-hall pillar pair became
	// a looping corridor — the low way out, the high way back to its
	// entry (maze.go; the original 4-4's repeating maze, restored).
	b.Maze(92, 114, false)
	b.Set(104, 12, 'W')
	b.Set(112, 12, 'K')
	pillar(123)
	pillar(128)
	b.Set(132, 9, 'f') // the fire flower for the boss
	b.Set(134, 12, 'G')
	b.Set(138, 12, 'W')
	b.Set(142, 12, 'K')

	// The boss arena: a short stair up, a bridge of planks over the
	// pool, the fake Bowser on it and the axe behind him.
	b.StairsUp(150, 4)
	b.Fill(154, 13, 161, 14, 'L') // the lava pool under the bridge
	for x := 154; x <= 161; x++ {
		b.Set(x, 13, 'b') // planks flush to the ledge at 162
	}
	b.Set(158, 12, 'Z') // fake Bowser (goomba, as at the quest's start)
	b.Set(165, 12, 'x') // the axe
	b.Set(174, 12, 't') // the toad — the princess is in another castle (5-4)
	l := mustLevel("4-4", b)
	l.Time = 300
	l.BowserDisguise = KindGoomba
	return l
}
