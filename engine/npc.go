package engine

// npc.go — castle NPC artifacts. The retainer cutscene itself (state
// machine, walk, text) is Worker A's in engine.go and the rendering is
// Worker C's; no runtime entity is needed for Toad or the Princess.
// The one NPC-flavored gameplay artifact that belongs to this worker
// is the fake-bowser reveal corpse, which lives here.

// spawnRevealCorpse replaces a fireball-killed impostor Bowser with
// the flipped corpse of his true form (SMB1: the "boss" of 1-4/2-4/3-4
// is a goomba / green koopa / buzzy beetle in a Bowser suit). The
// corpse plants its feet at the boss's feet, centres on his body and
// falls out of the world through the shared flipped-enemy path. The
// score (BowserScore) is paid by killBowser; this is pure reveal.
func (g *Game) spawnRevealCorpse(b *Bowser) {
	var e *Enemy
	switch b.Disguise {
	case KindKoopa:
		e = newKoopa(b.Pos)
	case KindBuzzy:
		e = newBuzzy(b.Pos)
	default:
		e = newGoomba(b.Pos)
	}
	e.Pos.X = b.Pos.X + (b.W-e.W)/2
	e.Pos.Y = b.Pos.Y + b.H - e.H
	e.State = EnemyFlipped
	e.Vel = Vec{0, FlipVel}
	g.Enemies = append(g.Enemies, e)
}
