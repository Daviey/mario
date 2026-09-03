package engine

// World 7: the para gauntlet with the springboard shelf (7-1), the
// cave with the second flooded middle (7-2 — 5-2's dive pattern, a
// bigger bloom), the lift chain (7-3) and THE maze castle (7-4) — the
// looping-corridor debut, with 4-4 retrofitted behind it.

// level25 is 7-1: the para gauntlet — hopping pairs the whole road,
// the springboard shelf over the tall brick wall, plant pipes and the
// starman brick for the closing rush.
func level25() *Level {
	b := NewBuilder(200, LevelHeight)
	b.Ground(0, 45)
	b.Ground(50, 97)   // pit 46-49
	b.Ground(102, 199) // pit 98-101
	b.Set(3, 12, 'M')

	// The opening wave under the question row.
	b.Set(10, 12, 'W')
	b.Set(13, 12, 'W')
	b.Set(16, 9, '?')
	b.Set(18, 9, 'U')
	b.Set(20, 9, '?')
	b.Set(26, 12, 'G')
	b.Set(28, 12, 'G')

	// THE SPRINGBOARD SHELF, 2-1's exact spacing: spring, hidden
	// block two along, tall wall three past it — the bounce clears it
	// or the hidden step does (the planner's route).
	b.Springboard(36, 12)
	b.Fill(37, 10, 38, 10, 'B') // 2-1's brick step under the hidden block
	b.Set(38, 7, 'H')           // the hidden step: bump, stand, hop the wall top
	b.Fill(41, 6, 41, 12, 'B')
	b.Fill(44, 5, 48, 5, 'B')
	b.Coins(4, 45, 46, 47)
	b.Set(33, 12, 'K')

	b.Pipe(52, 3)
	b.Plant(52, 3)
	b.Set(58, 12, 'W')
	b.Set(61, 12, 'W')
	b.Coins(9, 64, 65, 66)
	b.Set(70, 12, 'K')
	b.Set(72, 12, 'G')
	b.Pipe(78, 2)
	b.Plant(78, 2)
	b.Set(84, 12, 'W')
	b.Set(87, 12, 'W')

	// The far bank: the multi-coin row and the hidden 1-UP.
	b.Fill(106, 9, 110, 9, 'B')
	b.Set(108, 9, 'C')
	b.Set(112, 5, '1')
	b.Set(116, 12, 'G')
	b.Set(118, 12, 'G')
	b.Coins(9, 122, 123, 124)
	b.Pipe(128, 3)
	b.Plant(128, 3)
	b.Set(134, 12, 'K')
	b.Set(137, 12, 'W')
	b.Set(140, 12, 'W')
	b.Set(144, 9, 'S') // the starman brick
	b.Coins(9, 148, 149, 150)
	b.StairsUp(158, 8)
	b.Flag(178)
	l := mustLevel("7-1", b)
	l.Time = 400
	return l
}

// level26 is 7-2: the cave with the second flooded middle — a bigger
// bloom than 5-2's (three bloopers, the coin drift), the dive pipe
// mid-body and the exit pipe to the overworld flag stretch.
func level26() *Level {
	b := NewBuilder(190, LevelHeight)
	b.Theme(ThemeUnderground)
	b.Ground(0, 43)
	b.Ground(48, 189) // pit 44-47
	b.Ceiling()
	b.Set(3, 12, 'M')

	// The opening: brick row, koopa pair, coins.
	b.Set(10, 9, 'B')
	b.Set(11, 9, '?')
	b.Set(12, 9, 'U')
	b.Set(13, 9, 'B')
	b.Set(16, 12, 'K')
	b.Set(18, 12, 'K')
	b.Coins(9, 22, 23, 24)
	b.Set(28, 9, 'C') // the lone multi-coin brick
	b.Set(32, 12, 'G')
	b.Set(34, 12, 'W')
	b.Set(38, 12, 'G')

	// THE DIVE: the plant pipe at 42 sinks into the flooded cut.
	b.Pipe(42, 2)
	b.Plant(42, 2)

	// The surfacing road: coins, then the cave's last stretch.
	b.Coins(10, 50, 51, 52, 53)
	b.Set(58, 12, 'K')
	b.Fill(62, 9, 66, 9, 'B')
	b.Set(64, 9, 'U')
	b.Coins(8, 62, 63, 64, 65, 66)
	b.Set(72, 12, 'W')
	b.Set(74, 12, 'W')
	b.Set(78, 12, 'K')
	b.Set(84, 9, '?')
	b.Set(86, 9, 'f')
	b.Coins(9, 90, 91, 92)
	b.Set(96, 12, 'G')
	b.Set(98, 12, 'G')
	b.Pipe(104, 2)
	b.Plant(104, 2)
	b.Set(110, 12, 'K')
	b.Set(113, 12, 'W')
	b.Coins(9, 118, 119, 120)
	b.StairsUp(126, 8)
	b.Flag(142)
	l := mustLevel("7-2", b)
	l.Time = 400
	room := level26WaterRoom()
	l.Warps = []Warp{{X: 42, Top: GroundTop - 2, Dest: room, DestX: 2, DestTop: GroundTop - 2}}
	return l
}

// level26WaterRoom is 7-2's flooded middle: three bloopers and the
// long coin drift, its exit pipe surfacing back in the cave.
func level26WaterRoom() *Level {
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
	b.Set(13, 9, 'q')
	b.Set(20, 9, 'q')
	b.Coins(9, 7, 8, 9, 10, 11, 12)
	b.Coins(6, 16, 17, 18)
	b.Coins(9, 23, 24, 25, 26, 27)
	b.Set(30, 9, '?')
	l := mustLevel("7-2 water", b)
	l.Underwater = true
	l.Warps = []Warp{{X: 36, Top: GroundTop - 2, Dest: nil, DestX: 50, DestTop: GroundTop - 3}}
	return l
}

// level27 is 7-3: the lift chain — three boards over the gaps, one
// elevator shaft, the flimsy hop to the high coin run, reds on every
// island.
func level27() *Level {
	b := NewBuilder(200, LevelHeight)
	b.Theme(ThemeSky)
	b.Ground(0, 19)
	b.Ground(24, 51)   // pit 20-23 (board)
	b.Ground(56, 83)   // pit 52-55 (elevator)
	b.Ground(88, 115)  // pit 84-87 (board)
	b.Ground(120, 147) // pit 116-119 (flimsy)
	b.Ground(152, 199) // pit 148-151 (board)
	b.Set(3, 12, 'M')

	b.Set(8, 12, 'R')
	b.Fill(12, 9, 15, 9, 'B')
	b.Set(13, 9, 'U')
	b.Set(18, 12, 'G')

	// The first board.
	b.Lift(21, 10, 3, LiftHoriz, 4)
	b.Coins(7, 20, 21, 22, 23)

	b.Set(28, 12, 'K')
	b.Fill(32, 9, 36, 9, 'B')
	b.Coins(8, 32, 33, 34, 35, 36)
	b.Set(40, 7, 'r')
	b.Set(44, 12, 'R')
	b.Coins(9, 48, 49, 50)

	// The elevator shaft.
	b.Lift(53, 10, 3, LiftVert, 5)
	b.Coins(6, 52, 53, 54, 55)
	b.Set(60, 12, 'K')
	b.Fill(64, 9, 68, 9, 'B')
	b.Set(66, 9, 'S')
	b.Set(74, 7, 'r')
	b.Set(78, 12, 'R')
	b.Coins(9, 80, 81, 82)

	// The board back and the flimsy hop.
	b.Lift(85, 10, 3, LiftHoriz, 4)
	b.Coins(7, 84, 85, 86, 87)
	b.Set(92, 12, 'G')
	b.Lift(117, 9, 3, LiftFlimsy, 0)
	b.Fill(122, 6, 130, 6, 'B')
	b.Coins(5, 123, 124, 125, 126, 127, 128, 129)
	b.Set(136, 12, 'R')
	b.Set(140, 12, 'K')
	b.Coins(9, 144, 145, 146)

	// The last board and the run-in.
	b.Lift(149, 10, 3, LiftHoriz, 4)
	b.Coins(7, 148, 149, 150, 151)
	b.Set(158, 12, 'R')
	b.Set(162, 12, 'G')
	b.Coins(9, 166, 167, 168)
	b.StairsUp(178, 8)
	b.Flag(194)
	l := mustLevel("7-3", b)
	l.Time = 300
	return l
}

// level28 is THE maze castle: three looping corridors between the fire
// bars — low way, high way, low way again (SMB 7-4's ladder of
// repeats, condensed to three) — before the fake Bowser (a goomba in
// the suit) on his bridge, the axe, and the princess.
func level28() *Level {
	b := NewBuilder(210, LevelHeight)
	b.Theme(ThemeCastle)
	b.Ground(0, 29)
	b.Ground(33, 54)   // lava 30-32
	b.Ground(58, 93)   // lava 55-57 (maze one)
	b.Ground(97, 132)  // lava 94-96 (maze two)
	b.Ground(136, 171) // lava 133-135 (maze three)
	b.Ground(175, 209) // lava 172-174
	b.Ceiling()
	b.Fill(30, 13, 32, 14, 'L')
	b.Fill(55, 13, 57, 14, 'L')
	b.Fill(94, 13, 96, 14, 'L')
	b.Fill(133, 13, 135, 14, 'L')
	b.Fill(172, 13, 174, 14, 'L')
	b.Set(3, 12, 'M')

	b.Set(31, 13, 'o') // the pools boil at the gate
	b.Set(95, 13, 'o')
	b.Set(134, 13, 'o')

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

	// THE MAZES: corridor one takes the low way, two the high, three
	// the low again — each wrong tier loops the corridor to its entry.
	// Zone walkers pace the open ground inside each corridor — never
	// on the wall cells (an entity marker would punch a hole in it).
	b.Maze(60, 90, false)
	b.Set(70, 12, 'K')
	b.Maze(99, 130, true)
	b.Set(108, 12, 'W')
	b.Maze(138, 168, false)
	b.Set(146, 12, 'K')

	// The final hall before the arena.
	b.Set(178, 9, 'f') // the fire flower for the boss
	b.Set(180, 12, 'G')
	b.Set(182, 12, 'W')
	b.Set(184, 12, 'K')

	// The boss arena: a short stair up, the bridge over the pool, the
	// fake Bowser (a goomba) and the axe behind him.
	b.StairsUp(184, 4)
	b.Fill(188, 13, 195, 14, 'L') // the lava pool under the bridge
	for x := 188; x <= 195; x++ {
		b.Set(x, 13, 'b') // planks flush to the ledge at 196
	}
	b.Set(192, 12, 'Z') // fake Bowser (goomba), on the bridge
	b.Set(199, 12, 'x') // the axe
	b.Set(205, 12, 'p') // the princess: the quest's end (while 7 is last)
	l := mustLevel("7-4", b)
	l.Time = 300
	l.BowserDisguise = KindGoomba
	return l
}
