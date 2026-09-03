package engine

// World 6: the plant-pipe gauntlet (6-1), the lift cave (6-2), the
// short hop chain (6-3 — the original's briefest athletic flight) and
// world 6's castle (6-4). The princess walks one world further: 5-4
// fields the toad now, 6-4 the princess, until world 7 lands.

// level21 is 6-1: the plant gauntlet — plant pipes pace the whole
// road, koopa pairs between them, the starman brick for the run-in and
// the hidden 1-UP over the coin row.
func level21() *Level {
	b := NewBuilder(190, LevelHeight)
	b.Ground(0, 37)
	b.Ground(42, 89)  // pit 38-41
	b.Ground(94, 189) // pit 90-93
	b.Set(3, 12, 'M')

	// The opening wave before the first pipe.
	b.Set(10, 12, 'K')
	b.Set(13, 12, 'K')
	b.Set(16, 9, '?')
	b.Set(18, 9, 'U')
	b.Set(20, 9, '?')

	// THE GAUNTLET: plant pipes of every height, koopa pairs between.
	b.Pipe(26, 2)
	b.Plant(26, 2)
	b.Pipe(32, 3)
	b.Plant(32, 3)
	b.Set(38, 7, 'r') // the red para watches the pit
	b.Pipe(46, 2)
	b.Plant(46, 2)
	b.Set(50, 12, 'K')
	b.Set(52, 12, 'G')
	b.Pipe(56, 3)
	b.Plant(56, 3)
	b.Coins(9, 60, 61, 62)

	// The brick ledge over the double-pipe stretch.
	b.Fill(64, 9, 68, 9, 'B')
	b.Set(66, 9, 'C') // the multi-coin brick hides mid-row
	b.Set(70, 12, 'G')
	b.Pipe(74, 2)
	b.Plant(74, 2)
	b.Pipe(80, 3)
	b.Plant(80, 3)
	b.Set(86, 12, 'W')

	// The far bank: the starman and the closing gauntlet.
	b.Set(98, 9, 'S')
	b.Pipe(104, 2)
	b.Plant(104, 2)
	b.Coins(9, 108, 109, 110)
	b.Set(112, 5, '1') // the hidden 1-UP over the coin row
	b.Pipe(116, 3)
	b.Plant(116, 3)
	b.Set(122, 12, 'K')
	b.Set(124, 12, 'K')
	b.Pipe(130, 2)
	b.Plant(130, 2)
	b.Set(136, 12, 'G')
	b.Set(138, 12, 'G')
	b.Coins(9, 142, 143, 144)
	b.StairsUp(152, 8)
	b.Flag(170)
	l := mustLevel("6-1", b)
	l.Time = 400
	return l
}

// level22 is 6-2: the lift cave — vertical lifts ride the shafts
// between brick tiers, koopas walk the ledges, and the exit pipe
// trades the cave for the overworld flag stretch (2-2's exit shape).
func level22() *Level {
	b := NewBuilder(190, LevelHeight)
	b.Theme(ThemeUnderground)
	b.Ground(0, 43)
	b.Ground(48, 189) // pit 44-47
	b.Ceiling()
	b.Set(3, 12, 'M')

	// The opening: brick row and koopa pair.
	b.Set(10, 9, 'B')
	b.Set(11, 9, '?')
	b.Set(12, 9, 'U')
	b.Set(13, 9, 'B')
	b.Set(16, 12, 'K')
	b.Set(18, 12, 'K')
	b.Coins(9, 22, 23, 24)

	// THE LIFT SHAFTS: vertical lifts bridge the tiers, coins ride the
	// columns.
	b.Lift(28, 10, 3, LiftVert, 5)
	b.Coins(6, 27, 28, 29)
	b.Fill(34, 9, 38, 9, 'B')
	b.Set(36, 9, 'f') // the fire flower on the ledge
	b.Set(40, 12, 'G')

	b.Lift(49, 10, 3, LiftVert, 5)
	b.Coins(6, 48, 49, 50)
	b.Set(56, 12, 'K')
	b.Coins(9, 60, 61, 62)
	b.Lift(66, 10, 3, LiftVert, 5)
	b.Coins(6, 65, 66, 67)

	// The mid tiers: koopas walk the brick ledges between shafts.
	b.Fill(74, 9, 78, 9, 'B')
	b.Set(75, 8, 'K')
	b.Coins(8, 74, 75, 76, 77, 78)
	b.Set(82, 12, 'W')
	b.Set(84, 12, 'W')
	b.Lift(88, 10, 3, LiftVert, 5)
	b.Coins(6, 87, 88, 89)

	// The closing stretch: the hidden 1-UP and the exit pipe.
	b.Set(96, 5, '1')
	b.Coins(9, 100, 101, 102)
	b.Set(108, 12, 'K')
	b.Set(110, 12, 'G')
	b.Fill(116, 9, 120, 9, 'B')
	b.Set(118, 9, 'U')
	b.Coins(8, 116, 117, 118, 119, 120)
	b.Set(126, 12, 'K')
	b.Coins(9, 130, 131, 132)
	b.Set(138, 12, 'W')
	b.Pipe(150, 3)
	b.Plant(150, 3)

	// The exit pipe: 2-2's shape — warp to the overworld flag room.
	b.Pipe(176, 3)
	l := mustLevel("6-2", b)
	l.Time = 400
	room := level22FlagRoom()
	l.Warps = []Warp{{X: 176, Top: GroundTop - 3, Dest: room, DestX: 2, DestTop: GroundTop - 2}}
	return l
}

// level22FlagRoom is 6-2's overworld exit: rise out of the pipe into
// the short flag stretch (the house shape 2-2 and 4-2 share).
func level22FlagRoom() *Level {
	b := NewBuilder(44, LevelHeight)
	b.Ground(0, 43)
	b.Pipe(2, 2)
	b.Coins(9, 20, 21, 22)
	b.StairsUp(28, 4)
	b.Flag(38)
	b.Set(6, 12, 'M')
	return mustLevel("6-2", b)
}

// level23 is 6-3: the short hop chain — the original's briefest
// athletic flight: a tight island run with one lift board, koopas on
// every island, and a quick staircase into the flag.
func level23() *Level {
	b := NewBuilder(130, LevelHeight)
	b.Theme(ThemeSky)
	b.Ground(0, 15)
	b.Ground(20, 39)  // pit 16-19
	b.Ground(44, 63)  // pit 40-43 (lift)
	b.Ground(68, 87)  // pit 64-67
	b.Ground(92, 129) // pit 88-91 (lift)
	b.Set(3, 12, 'M')

	b.Set(8, 12, 'R')
	b.Fill(12, 9, 15, 9, 'B')
	b.Set(13, 9, 'U')

	// The island run: reds on every island.
	b.Set(24, 12, 'R')
	b.Coins(9, 28, 29, 30)
	b.Set(34, 12, 'G')

	// The single lift board and its coin arc.
	b.Lift(41, 10, 3, LiftHoriz, 4)
	b.Coins(7, 40, 41, 42, 43)

	b.Set(48, 12, 'R')
	b.Set(52, 7, 'r')
	b.Coins(9, 56, 57, 58)
	b.Set(62, 12, 'G')

	b.Set(72, 12, 'R')
	b.Coins(9, 76, 77, 78)
	b.Set(84, 12, 'R')

	// The last board and the run-in.
	b.Lift(89, 10, 3, LiftHoriz, 4)
	b.Coins(7, 88, 89, 90, 91)
	b.Set(98, 12, 'G')
	b.Coins(9, 104, 105, 106)
	b.StairsUp(112, 8)
	b.Flag(124)
	l := mustLevel("6-3", b)
	l.Time = 300
	return l
}

// level24 is world 6's castle: the pillar gauntlet deepens — fire bars
// in threes between the pools — before the fake Bowser (a buzzy in the
// suit) on his bridge, the axe, and the princess, who ends the quest
// while world 6 is the last world.
func level24() *Level {
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

	// The long hall: pillar trios between the pools.
	b.Set(36, 12, 'K')
	pillar(40)
	pillar(44)
	pillar(48)
	b.Coins(9, 52, 53, 54)
	b.Set(56, 12, 'W')
	pillar(65)
	pillar(69)
	pillar(73)
	b.Fill(76, 9, 80, 9, 'B')
	b.Set(78, 9, 'U')
	b.Coins(8, 76, 77, 78, 79, 80)
	b.Set(84, 12, 'K')
	pillar(94)
	pillar(98)
	pillar(102)
	b.Set(106, 12, 'W')
	b.Coins(9, 110, 111, 112)
	pillar(123)
	pillar(127)
	pillar(131)
	b.Set(134, 9, 'f') // the fire flower for the boss
	b.Set(136, 12, 'G')
	b.Set(140, 12, 'W')
	b.Set(142, 12, 'K')

	// The boss arena: a short stair up, the bridge over the pool, the
	// fake Bowser (a buzzy) and the axe behind him.
	b.StairsUp(150, 4)
	b.Fill(154, 13, 161, 14, 'L') // the lava pool under the bridge
	for x := 154; x <= 161; x++ {
		b.Set(x, 13, 'b') // planks flush to the ledge at 162
	}
	b.Set(158, 12, 'Z') // fake Bowser (buzzy), on the bridge
	b.Set(165, 12, 'x') // the axe
	b.Set(174, 12, 'p') // the princess: the quest's end (while 6 is last)
	l := mustLevel("6-4", b)
	l.Time = 300
	l.BowserDisguise = KindBuzzy
	return l
}
