package engine

import "testing"

// TestWarpZoneWorldFourPipe pins the 1-2 warp zone's restored third
// pipe: world 4's mouth (leftmost, the original's 4-3-2 order) jumps
// the run to 4-1 — level index 12, not a clamp.
func TestWarpZoneWorldFourPipe(t *testing.T) {
	l := level2()
	room := l.Warps[0].Dest // the roof warp zone; its pipes carry the JumpTos
	var w *Warp
	for i := range room.Warps {
		if room.Warps[i].JumpTo == 12 {
			w = &room.Warps[i]
		}
	}
	if w == nil {
		t.Fatal("1-2 warp zone has no world-4 pipe (JumpTo 12)")
	}
	// The full run's level list: JumpTo indexes DefaultLevels, so a
	// one-level fixture would clamp the skip to itself.
	g := NewGame(DefaultLevels(), 40, LevelHeight)
	g.State = StatePlaying
	g.performWarp(w)
	if got := g.LevelName(); got != "4-1" {
		t.Fatalf("world-4 pipe landed on %q, want 4-1", got)
	}
}

// TestEndingLadder pins the quest's ending ladder now that world 4
// exists: 3-4 fields the toad's "another castle" beat and carries on,
// and the princess — StateWin's trigger — waits in 4-4, the last level.
func TestEndingLadder(t *testing.T) {
	levels := DefaultLevels()
	if got := levels[11].Name; got != "3-4" {
		t.Fatalf("level 11 = %q, want 3-4", got)
	}
	if levels[11].Retainer != 1 {
		t.Errorf("3-4 retainer = %d, want 1 (the toad — another castle)", levels[11].Retainer)
	}
	if got := levels[15].Name; got != "4-4" {
		t.Fatalf("level 15 = %q, want 4-4", got)
	}
	if levels[15].Retainer != 1 {
		t.Errorf("4-4 retainer = %d, want 1 (the toad — another castle, since world 5 exists)", levels[15].Retainer)
	}
	if got := levels[19].Name; got != "5-4" {
		t.Fatalf("level 19 = %q, want 5-4", got)
	}
	if levels[19].Retainer != 2 {
		t.Errorf("5-4 retainer = %d, want 2 (the princess ends the quest)", levels[19].Retainer)
	}
}

// TestWorldFourVineHeavenIsItsOwnRoom pins the room-caching contract
// the 4-2 heaven relies on: two vine rooms must be distinct templates,
// or a run through 1-1's heaven would leave 4-2's picked clean (rooms
// cache live state per template pointer).
func TestWorldFourVineHeavenIsItsOwnRoom(t *testing.T) {
	l1, l14 := level1(), level14()
	if l1.VineRoom == nil || l14.VineRoom == nil {
		t.Fatal("both 1-1 and 4-2 must carry a vine room")
	}
	if l1.VineRoom == l14.VineRoom {
		t.Fatal("1-1 and 4-2 share one vine-room template — coin state would bleed between them")
	}
	if l14.VineRoom.DropExitX != 100 {
		t.Errorf("4-2 heaven DropExitX = %d, want 100 (past the cave's pit)", l14.VineRoom.DropExitX)
	}
}
