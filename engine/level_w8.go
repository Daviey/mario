package engine

// World 8: the endgame — the long road with returning blasters and
// the hammer bros (8-1), the buzzy cave with the springboard shelf
// (8-2), the hammer gauntlet (8-3) and THE final castle (8-4): four
// maze corridors, a flooded detour mid-run, and the REAL Bowser — no
// disguise, no corpse, the suit falls — behind him the axe and the
// princess, the quest's true end.

// level29 is 8-1: the long road — the original's longest stretch,
// condensed but relentless: hammer bros open and close it, blasters
// return between the plant pipes, and the para pairs never let up.
func level29() *Level {
	b := NewBuilder(230, LevelHeight)
	b.Ground(0, 51)
	b.Ground(56, 111)  // pit 52-55
	b.Ground(116, 229) // pit 112-115
	b.Set(3, 12, 'M')

	// The opening wave and the first hammer pair.
	b.Set(10, 9, '?')
	b.Set(12, 9, 'U')
	b.Set(14, 9, '?')
	b.Set(18, 12, 'W')
	b.Set(20, 12, 'W')
	b.Fill(26, 9, 30, 9, 'B')
	b.Set(27, 9, '?')
	b.Set(29, 9, 'U')
	b.Set(27, 8, 'm')
	b.Set(29, 8, 'm')

	// The blasters return between the pipes.
	b.Pipe(36, 2)
	b.Plant(36, 2)
	b.Fill(42, 12, 43, 12, 'N')
	b.Set(48, 12, 'K')
	b.Coins(9, 52, 53, 54)

	// The far bank: the para gauntlet and the star brick.
	b.Set(60, 12, 'W')
	b.Set(63, 12, 'W')
	b.Set(66, 12, 'W')
	b.Set(72, 9, 'S')
	b.Coins(9, 76, 77, 78)
	b.Set(84, 12, 'G')
	b.Set(86, 12, 'G')

	// The second blaster pair under the brick shelf.
	b.Fill(92, 9, 97, 9, 'B')
	b.Fill(93, 8, 94, 8, 'N')
	b.Coins(11, 92, 93, 94, 95, 96)
	b.Set(102, 12, 'K')
	b.Pipe(106, 3)
	b.Plant(106, 3)

	// The long home stretch: paras, the hidden 1-UP, the closing pair.
	b.Set(122, 12, 'W')
	b.Set(125, 12, 'W')
	b.Set(128, 12, 'W')
	b.Coins(9, 132, 133, 134)
	b.Set(136, 5, '1')
	b.Fill(140, 12, 141, 12, 'N')
	b.Set(146, 12, 'R')
	b.Set(150, 12, 'G')
	b.Set(152, 12, 'G')
	b.Coins(9, 156, 157, 158)
	b.Set(162, 12, 'W')
	b.Set(165, 12, 'W')
	b.Fill(170, 9, 174, 9, 'B')
	b.Set(171, 9, '?')
	b.Set(173, 9, 'U')
	b.Set(171, 8, 'm')
	b.Set(173, 8, 'm')
	b.Coins(8, 170, 171, 172, 173, 174)
	b.Set(182, 12, 'K')
	b.Set(184, 12, 'W')
	b.Coins(9, 188, 189, 190)
	b.StairsUp(198, 8)
	b.Flag(218)
	l := mustLevel("8-1", b)
	l.Time = 400
	return l
}

// level30 is 8-2: the buzzy cave — hammer bros on the block rows,
// buzzies pacing the floor (fire-immune, the endgame's walkers), the
// springboard shelf mid-cave, and the exit pipe to the flag stretch.
func level30() *Level {
	b := NewBuilder(200, LevelHeight)
	b.Theme(ThemeUnderground)
	b.Ground(0, 43)
	b.Ground(48, 199) // pit 44-47
	b.Ceiling()
	b.Set(3, 12, 'M')

	// The opening: brick row, buzzy pair.
	b.Set(10, 9, 'B')
	b.Set(11, 9, '?')
	b.Set(12, 9, 'U')
	b.Set(13, 9, 'B')
	b.Set(16, 12, 'z')
	b.Set(18, 12, 'z')
	b.Coins(9, 22, 23, 24)

	// THE HAMMER PAIR on the block row.
	b.Fill(28, 9, 32, 9, 'B')
	b.Set(29, 9, '?')
	b.Set(31, 9, 'U')
	b.Set(29, 8, 'm')
	b.Set(31, 8, 'm')
	b.Set(36, 12, 'z')

	// THE SPRINGBOARD SHELF (2-1's exact shape): bounce, brick step,
	// hidden block, tall wall.
	b.Springboard(44, 12)
	b.Fill(45, 10, 46, 10, 'B')
	b.Set(46, 7, 'H')
	b.Fill(49, 6, 49, 12, 'B')
	b.Fill(52, 5, 56, 5, 'B')
	b.Coins(4, 53, 54, 55)

	// The flooded-road shuffle: buzzies and coins to the exit.
	b.Set(60, 12, 'z')
	b.Coins(9, 64, 65, 66)
	b.Set(70, 12, 'z')
	b.Set(72, 12, 'z')
	b.Set(76, 9, 'C') // the lone multi-coin brick
	b.Coins(9, 82, 83, 84)
	b.Set(88, 12, 'z')
	b.Fill(92, 9, 96, 9, 'B')
	b.Set(94, 9, 'U')
	b.Coins(8, 92, 93, 94, 95, 96)
	b.Set(102, 12, 'z')
	b.Set(104, 12, 'z')
	b.Set(108, 9, 'f') // the fire flower for the road out
	b.Coins(9, 112, 113, 114)
	b.Set(120, 12, 'z')
	b.Set(122, 12, 'z')
	b.Coins(9, 128, 129, 130)
	b.Set(136, 12, 'z')
	b.Pipe(142, 2)
	b.Plant(142, 2)

	// The exit pipe: the flag-room shape.
	b.Pipe(186, 3)
	l := mustLevel("8-2", b)
	l.Time = 400
	room := level30FlagRoom()
	l.Warps = []Warp{{X: 186, Top: GroundTop - 3, Dest: room, DestX: 2, DestTop: GroundTop - 2}}
	return l
}

// level30FlagRoom is 8-2's overworld exit: rise out of the pipe into
// the short flag stretch.
func level30FlagRoom() *Level {
	b := NewBuilder(44, LevelHeight)
	b.Ground(0, 43)
	b.Pipe(2, 2)
	b.Coins(9, 20, 21, 22)
	b.StairsUp(28, 4)
	b.Flag(38)
	b.Set(6, 12, 'M')
	return mustLevel("8-2", b)
}

// level31 is 8-3: the hammer gauntlet — three pairs on their block
// formations down the whole road, para pairs between, the star brick
// for the run-in.
func level31() *Level {
	b := NewBuilder(200, LevelHeight)
	b.Ground(0, 41)
	b.Ground(46, 101)  // pit 42-45
	b.Ground(106, 199) // pit 102-105
	b.Set(3, 12, 'M')

	// The opening wave and pair one.
	b.Set(10, 9, '?')
	b.Set(12, 9, 'U')
	b.Set(14, 9, '?')
	b.Set(18, 12, 'W')
	b.Set(20, 12, 'W')
	b.Fill(26, 9, 30, 9, 'B')
	b.Set(27, 9, '?')
	b.Set(29, 9, 'U')
	b.Set(27, 8, 'm')
	b.Set(29, 8, 'm')

	// Pair two, over the pit stretch.
	b.Set(36, 12, 'K')
	b.Coins(9, 48, 49, 50)
	b.Fill(56, 9, 60, 9, 'B')
	b.Set(57, 9, '?')
	b.Set(59, 9, 'U')
	b.Set(57, 8, 'm')
	b.Set(59, 8, 'm')
	b.Set(66, 12, 'G')
	b.Set(68, 12, 'G')
	b.Coins(9, 74, 75, 76)

	// Pair three before the stairs.
	b.Set(82, 12, 'W')
	b.Set(84, 12, 'W')
	b.Set(88, 9, 'S') // the starman brick for the last pair
	b.Fill(94, 9, 98, 9, 'B')
	b.Set(95, 9, '?')
	b.Set(97, 9, 'U')
	b.Set(95, 8, 'm')
	b.Set(97, 8, 'm')
	b.Set(110, 12, 'K')
	b.Set(112, 12, 'G')
	b.Coins(9, 118, 119, 120)
	b.Set(124, 5, '1') // the hidden 1-UP
	b.Set(130, 12, 'W')
	b.Set(133, 12, 'W')
	b.Coins(9, 138, 139, 140)
	b.StairsUp(160, 8)
	b.Flag(184)
	l := mustLevel("8-3", b)
	l.Time = 300
	return l
}

// level32 is THE final castle: four maze corridors — low, high, low,
// high — a flooded detour mid-run (the original's underwater pipe,
// a room here), the fire-bar hall, and the REAL Bowser: no disguise
// is set, the suit falls, and behind the axe the princess ends the
// quest for good.
func level32() *Level {
	b := NewBuilder(260, LevelHeight)
	b.Theme(ThemeCastle)
	b.Ground(0, 29)
	b.Ground(33, 54)   // lava 30-32
	b.Ground(58, 91)   // lava 55-57 (maze one)
	b.Ground(95, 126)  // lava 92-94 (the dive + maze two)
	b.Ground(130, 171) // lava 127-129 (maze three)
	b.Ground(175, 208) // lava 172-174 (maze four)
	b.Ground(212, 259) // lava 209-211 (the arena)
	b.Ceiling()
	b.Fill(30, 13, 32, 14, 'L')
	b.Fill(55, 13, 57, 14, 'L')
	b.Fill(92, 13, 94, 14, 'L')
	b.Fill(127, 13, 129, 14, 'L')
	b.Fill(172, 13, 174, 14, 'L')
	b.Fill(209, 13, 211, 14, 'L')
	b.Fill(236, 13, 239, 14, 'L') // the arena pool under the bridge
	b.Set(3, 12, 'M')

	b.Set(31, 13, 'o') // the pools boil at every gate
	b.Set(93, 13, 'o')
	b.Set(173, 13, 'o')

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

	// MAZE ONE (the low way) and its pacing walker.
	b.Maze(60, 86, false)
	b.Set(70, 12, 'K')

	// MAZE TWO (the high way), then the dive between mazes: the plant
	// pipe at 124 sinks into the flooded cut, and the room's exit
	// surfaces at the pipe inside maze three (past its wall, under
	// its tiers — the room is 8-4's own template, a warp room).
	b.Maze(99, 122, true)
	b.Set(108, 12, 'W')
	b.Pipe(124, 2)
	b.Plant(124, 2)

	// MAZE THREE (low) and MAZE FOUR (high): the ladder's last rungs.
	b.Maze(133, 164, false)
	b.Set(146, 12, 'K')
	b.Pipe(142, 2) // the water detour's surfacing pipe, inside the zone
	b.Maze(178, 202, true)
	b.Set(188, 12, 'W')

	// The final hall: the flower, the bars, the last walker.
	b.Set(216, 9, 'f')
	pillar(222)
	b.Set(220, 12, 'K')

	// The boss arena: the short stair, the bridge over the pool, and
	// the REAL Bowser — BowserDisguise stays unset (killBowser: the
	// suit falls, no corpse) — the axe behind him, the princess beyond.
	b.StairsUp(230, 4)
	for x := 236; x <= 239; x++ {
		b.Set(x, 13, 'b') // planks flush to the ledge at 240
	}
	b.Set(237, 12, 'Z') // the real Bowser, on the bridge
	b.Set(242, 12, 'x') // the axe
	b.Set(250, 12, 'p') // the princess: the quest's true end
	l := mustLevel("8-4", b)
	l.Time = 300
	room := level32WaterRoom()
	l.Warps = []Warp{{X: 124, Top: GroundTop - 2, Dest: room, DestX: 2, DestTop: GroundTop - 2}}
	return l
}

// level32WaterRoom is 8-4's flooded detour: the original's mid-maze
// underwater pipe, a room here — two bloopers, the coin drift, and
// the exit pipe surfacing inside maze three, past its wall.
func level32WaterRoom() *Level {
	b := NewBuilder(40, LevelHeight)
	b.Theme(ThemeUnderwater)
	b.Ground(0, 39)
	b.Ceiling()
	for y := 1; y < GroundTop; y++ { // brick walls close the pocket
		b.Set(0, y, 'B')
		b.Set(39, y, 'B')
	}
	b.Pipe(2, 2)  // arrival
	b.Pipe(36, 2) // the exit pipe
	b.Set(6, 9, 'q')
	b.Set(15, 9, 'q')
	b.Coins(9, 8, 9, 10, 11, 12)
	b.Coins(6, 17, 18, 19)
	b.Coins(9, 24, 25, 26, 27)
	b.Set(30, 9, '?')
	l := mustLevel("8-4 water", b)
	l.Underwater = true
	l.Warps = []Warp{{X: 36, Top: GroundTop - 2, Dest: nil, DestX: 142, DestTop: GroundTop - 2}}
	return l
}
