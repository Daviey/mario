package render

import "github.com/Daviey/mario/engine"

// hashX is a deterministic column hash for sky decorations (no RNG state).
func hashX(x int) uint32 {
	h := uint32(x) * 2654435761
	h ^= h >> 13
	h *= 0x5BD1E995
	h ^= h >> 15
	return h
}

// CloudAt reports a cloud anchored at world column tx: its sky row.
// Clouds appear roughly every 9 columns; all of them stamp the same
// sprCloud sprite (see BushAt), so there is no width to report.
func CloudAt(tx int) (row int, ok bool) {
	h := hashX(tx)
	if h%9 != 0 {
		return 0, false
	}
	return 4 + int(h>>8)%7, true
}

// castleRect is the goal castle's tile footprint: anchored 3 tiles right
// of the flag pole, 5 wide, on rows 9..12. Cloud avoidance and the
// castle painter both derive from this one geometry so they can never
// drift apart.
func castleRect(g *engine.Game) (x0, y0, w, h int) {
	return g.Level.FlagX + 3, 9, 5, 4
}

// cloudBlocked reports whether a cloud anchored at tx on sky row row would
// touch solid tiles or the goal castle. Blocked clouds are skipped so they
// never slice behind level geometry — clouds only ever draw on open sky.
func cloudBlocked(g *engine.Game, tx, row int) bool {
	for x := tx; x < tx+3; x++ { // sprCloud spans 12px = 3 tiles
		if g.Level.At(x, row) != engine.Empty {
			return true
		}
	}
	cx, cy, cw, ch := castleRect(g)
	if row >= cy && row < cy+ch && tx+3 > cx && tx < cx+cw {
		return true
	}
	return false
}

// HillAt reports whether a hill sits at world column tx (every ~13).
func HillAt(tx int) bool { return hashX(tx)%13 == 5 }

// BushAt reports whether a bush is anchored at tx (~ every 7 columns,
// never on a hill column). All bushes draw the same 9×2 sprite, so there
// is no width to report.
func BushAt(tx int) bool {
	h := hashX(tx)
	return h%7 == 3 && !HillAt(tx)
}

// viewTilesOf is the vertical viewport in tiles, derived from the game's
// ViewH so the world fills the available terminal area without changing
// the on-screen scale of any sprite or tile.
func viewTilesOf(g *engine.Game) int {
	vh := g.ViewH
	if vh < 4 {
		vh = 4
	}
	if vh > g.Level.Height {
		vh = g.Level.Height
	}
	return vh
}

// CameraY computes the vertical camera position in tiles: the player is
// kept slightly below center, clamped to the level.
func CameraY(g *engine.Game) float64 {
	vh := viewTilesOf(g)
	p := g.Player
	c := p.Pos.Y + p.H/2 - float64(vh)/2
	if c < 0 {
		return 0
	}
	if m := float64(g.Level.Height - vh); c > m {
		return m
	}
	return c
}
