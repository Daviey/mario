package engine

import "math"

const skin = 0.001 // inset applied when probing tile boundaries

// solidAt reports whether the tile at a grid position blocks movement.
func (g *Game) solidAt(tx, ty int) bool {
	return g.Level.At(tx, ty).Solid()
}

// applyGravity advances a falling body's vertical velocity by g and
// clamps it to MaxFall, the terminal velocity every clamped body shares.
// (The player and flipped enemies are deliberately unclamped.)
func applyGravity(vy, g float64) float64 {
	return min(vy+g, MaxFall)
}

// moveX shifts a body horizontally by dx, stopping at the first solid tile.
// It reports whether a wall was hit and leaves the body flush against it.
func (g *Game) moveX(pos *Vec, w, h, dx float64) bool {
	if dx == 0 {
		return false
	}
	pos.X += dx
	y0 := int(math.Floor(pos.Y + skin))
	y1 := int(math.Floor(pos.Y + h - skin))
	if dx > 0 {
		tx := int(math.Floor(pos.X + w - skin))
		for ty := y0; ty <= y1; ty++ {
			if g.solidAt(tx, ty) {
				pos.X = float64(tx) - w
				return true
			}
		}
	} else {
		tx := int(math.Floor(pos.X + skin))
		for ty := y0; ty <= y1; ty++ {
			if g.solidAt(tx, ty) {
				pos.X = float64(tx) + 1
				return true
			}
		}
	}
	return false
}

// moveY shifts a body vertically by dy. When moving down it reports whether
// the body landed (and leaves it flush with the surface). When moving up it
// returns the ceiling row and the columns of the tiles hit, if any.
func (g *Game) moveY(pos *Vec, w, h, dy float64) (landed bool, ceilTy int, ceilCols []int) {
	if dy == 0 {
		return false, -1, nil
	}
	pos.Y += dy
	x0 := int(math.Floor(pos.X + skin))
	x1 := int(math.Floor(pos.X + w - skin))
	if dy > 0 {
		ty := int(math.Floor(pos.Y + h - skin))
		for tx := x0; tx <= x1; tx++ {
			if g.solidAt(tx, ty) {
				pos.Y = float64(ty) - h
				return true, -1, nil
			}
		}
		return false, -1, nil
	}
	g.ceilBuf = g.ceilBuf[:0]
	ty := int(math.Floor(pos.Y + skin))
	for tx := x0; tx <= x1; tx++ {
		if g.solidAt(tx, ty) {
			g.ceilBuf = append(g.ceilBuf, tx)
		}
	}
	if len(g.ceilBuf) > 0 {
		pos.Y = float64(ty) + 1
		return false, ty, g.ceilBuf
	}
	return false, -1, nil
}
