package engine

// World 5 (the ladder continues: one world per release). Its signature
// is the bullet-bill blaster's debut (bullet.go) — 5-1 fields the
// cannons on the open road, 5-2 dives through an underwater detour
// (the cellar pattern run in reverse: a ground body whose mid-pipe
// swaps to a swimming room), 5-3 is the elevator flight, and 5-4's
// castle takes the princess now that the quest runs past 4-4.

// level17 is 5-1: the blasters' debut — cannon pairs bracket the road
// between the enemy waves, one high pair guards the coin shelf, and
// the paratroopa pairs keep the sky busy.
func level17() *Level {
	b := NewBuilder(190, LevelHeight)
	b.Ground(0, 43)
	b.Ground(48, 95)   // pit 44-47
	b.Ground(100, 189) // pit 96-99
	b.Set(3, 12, 'M')

	// The opening wave under the first cannon's arc.
	b.Set(10, 12, 'G')
	b.Set(12, 12, 'G')
	b.Set(16, 9, '?')
	b.Set(18, 9, 'U')
	b.Set(20, 9, '?')
	b.Fill(26, 12, 27, 12, 'N') // the first blaster
	b.Set(32, 12, 'W')
	b.Set(35, 12, 'W')
	b.Set(38, 12, 'K')

	// The pipe pair before the pit.
	b.Pipe(42, 2)
	b.Plant(42, 2)
	b.Coins(9, 50, 51, 52)
	b.Fill(56, 12, 57, 12, 'N') // the road blaster
	b.Set(62, 12, 'K')
	b.Set(65, 12, 'G')
	b.Set(68, 12, 'G')

	// The high pair: a raised brick shelf carries the cannons over the
	// coin run below — the safe line is under their muzzles' start.
	b.Fill(74, 9, 79, 9, 'B')
	b.Fill(75, 8, 76, 8, 'N')
	b.Coins(11, 74, 75, 76, 77, 78)
	b.Set(82, 12, 'R')

	b.Pipe(86, 3)
	b.Plant(86, 3)
	b.Set(90, 12, 'W')
	b.Set(93, 12, 'W')

	// The far bank: the starman brick behind the cannon.
	b.Fill(104, 12, 105, 12, 'N')
	b.Set(110, 9, 'S')
	b.Set(114, 12, 'K')
	b.Set(117, 12, 'G')
	b.Coins(9, 120, 121, 122)
	b.Set(124, 5, '1') // the hidden 1-UP over the coin row

	// The closing stretch: the last cannon before the staircase.
	b.Fill(130, 12, 131, 12, 'N')
	b.Set(136, 12, 'W')
	b.Set(139, 12, 'W')
	b.Set(142, 12, 'R')
	b.Coins(9, 146, 147, 148)
	b.StairsUp(154, 8)
	b.Flag(170)
	l := mustLevel("5-1", b)
	l.Time = 400
	return l
}

// level18 is 5-2: the cave with the flooded middle — the surface body
// runs to a plant pipe that sinks into the underwater detour (bloober
// country), whose exit pipe surfaces the run past the pit, on the road
// to the flag. The cellar pattern, run in reverse.
func level18() *Level {
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
	b.Set(34, 12, 'G')
	b.Set(38, 12, 'W')

	// THE DIVE: the plant pipe at 42 sinks into the flooded cut.
	b.Pipe(42, 2)
	b.Plant(42, 2)

	// The surfacing road: coins greet the swimmer back on land, then
	// the cave's last stretch to the flag.
	b.Coins(10, 50, 51, 52, 53)
	b.Set(58, 12, 'K')
	b.Fill(62, 9, 66, 9, 'B')
	b.Set(64, 9, 'U')
	b.Coins(8, 62, 63, 64, 65, 66)
	b.Set(72, 12, 'G')
	b.Set(74, 12, 'W')
	b.Set(78, 12, 'K')
	b.Set(84, 9, '?')
	b.Set(86, 9, 'f') // the fire flower for the road
	b.Coins(9, 90, 91, 92)
	b.Set(96, 12, 'G')
	b.Set(98, 12, 'G')
	b.Pipe(104, 2)
	b.Set(110, 12, 'K')
	b.Set(113, 12, 'W')
	b.Coins(9, 118, 119, 120)
	b.StairsUp(126, 8)
	b.Flag(142)
	l := mustLevel("5-2", b)
	l.Time = 400
	room := level18WaterRoom()
	l.Warps = []Warp{{X: 42, Top: GroundTop - 2, Dest: room, DestX: 2, DestTop: GroundTop - 2}}
	return l
}

// level18WaterRoom is 5-2's flooded middle: an underwater pocket with
// bloopers on patrol and a coin drift, its exit pipe surfacing the
// run back in the cave at the far-bank road (2-2's swim machinery).
func level18WaterRoom() *Level {
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
	b.Set(14, 9, 'q')
	b.Set(22, 9, 'q')
	b.Coins(9, 8, 9, 10, 11)
	b.Coins(6, 16, 17, 18)
	b.Coins(9, 24, 25, 26, 27)
	b.Set(30, 9, '?')
	l := mustLevel("5-2 water", b)
	l.Underwater = true
	l.Warps = []Warp{{X: 36, Top: GroundTop - 2, Dest: nil, DestX: 50, DestTop: GroundTop - 3}}
	return l
}

// level19 is 5-3: the elevator flight — vertical lifts climb the
// island stack, horizontal boards bridge the gaps, and the red
// paratroopas patrol the shafts.
func level19() *Level {
	b := NewBuilder(200, LevelHeight)
	b.Theme(ThemeSky)
	b.Ground(0, 19)
	b.Ground(24, 51)   // pit 20-23 (lift)
	b.Ground(56, 83)   // pit 52-55 (elevator pair)
	b.Ground(88, 115)  // pit 84-87 (lift)
	b.Ground(120, 147) // pit 116-119 (flimsy)
	b.Ground(152, 199) // pit 148-151 (elevator)
	b.Set(3, 12, 'M')

	// The opening island: the power-up ledge and the first red pair.
	b.Set(8, 12, 'R')
	b.Fill(12, 9, 15, 9, 'B')
	b.Set(13, 9, 'U')
	b.Set(18, 12, 'G')

	// The horizontal board over the first gap, coins above its sweep.
	b.Lift(21, 10, 3, LiftHoriz, 4)
	b.Coins(7, 20, 21, 22, 23)

	// The mid island: koopa under the brick row.
	b.Set(28, 12, 'K')
	b.Fill(32, 9, 36, 9, 'B')
	b.Coins(8, 32, 33, 34, 35, 36)
	b.Set(40, 7, 'r') // the shaft patrol begins
	b.Set(44, 12, 'R')
	b.Coins(9, 48, 49, 50)

	// THE ELEVATORS: vertical lifts climb the gap, coins up the shaft.
	b.Lift(53, 10, 3, LiftVert, 5)
	b.Coins(6, 52, 53, 54, 55)
	b.Set(60, 12, 'K')
	b.Fill(64, 9, 68, 9, 'B')
	b.Set(66, 9, 'S') // the starman brick for the crossing
	b.Set(74, 7, 'r')
	b.Set(78, 12, 'R')
	b.Coins(9, 80, 81, 82)

	// The board back, then the flimsy lift to the high coin run.
	b.Lift(85, 10, 3, LiftHoriz, 4)
	b.Coins(7, 84, 85, 86, 87)
	b.Set(92, 12, 'G')
	b.Lift(117, 9, 3, LiftFlimsy, 0)
	b.Fill(122, 6, 130, 6, 'B')
	b.Coins(5, 123, 124, 125, 126, 127, 128, 129)
	b.Set(136, 12, 'R')
	b.Set(140, 12, 'K')
	b.Coins(9, 144, 145, 146)

	// The last elevator rides down to the home stretch.
	b.Lift(149, 9, 3, LiftVert, 5)
	b.Set(158, 12, 'R')
	b.Set(162, 12, 'G')
	b.Coins(9, 166, 167, 168)
	b.StairsUp(178, 8)
	b.Flag(194)
	l := mustLevel("5-3", b)
	l.Time = 300
	return l
}

// level20 is the world-5 castle: the fire-bar hall doubles down —
// pillar pairs and lava between every stretch — before the fake Bowser
// (a koopa again) on his bridge and the axe. Toad waits behind the
// arena: the princess is in another castle (6-4, since world 6 exists).
func level20() *Level {
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

	// The long hall: pillar pairs, coins over the crossings.
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
	pillar(94)
	pillar(99)
	b.Set(104, 12, 'W')
	b.Coins(9, 108, 109, 110)
	b.Set(112, 12, 'K')
	pillar(123)
	pillar(128)
	b.Set(132, 9, 'f') // the fire flower for the boss
	b.Set(134, 12, 'G')
	b.Set(138, 12, 'W')
	b.Set(142, 12, 'K')

	// The boss arena: a short stair up, the bridge over the pool, the
	// fake Bowser (a koopa) and the axe behind him.
	b.StairsUp(150, 4)
	b.Fill(154, 13, 161, 14, 'L') // the lava pool under the bridge
	for x := 154; x <= 161; x++ {
		b.Set(x, 13, 'b') // planks flush to the ledge at 162
	}
	b.Set(158, 12, 'Z') // fake Bowser (koopa), on the bridge
	b.Set(165, 12, 'x') // the axe
	b.Set(174, 12, 't') // the toad — the princess is in another castle (6-4)
	l := mustLevel("5-4", b)
	l.Time = 300
	l.BowserDisguise = KindKoopa
	return l
}
