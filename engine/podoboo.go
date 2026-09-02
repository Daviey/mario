package engine

// Podoboo — the lava leaper of the castles (S7). A pure hazard: it
// rests under the lava surface, launches on a hash-phased period,
// arcs under plain (unclamped) gravity and falls back in. Nothing
// kills it — touching one kills the player outright, star power
// included — and fireballs pass straight through. Score 0 by
// definition: there is nothing to score.

// Podoboo tuning (per 60 Hz tick).
const (
	PodobooW        = 0.8
	PodobooH        = 0.8
	PodobooJumpVel  = -0.45 // launch impulse (~4.8-tile apex under Gravity)
	PodobooRestDrop = 1.2   // rest depth below BaseY while hidden in the lava
	PodobooHideAt   = 1.0   // deeper than this below BaseY counts as hidden
)

// Podoboo is a periodic lava fountain: Phase/Period fix its launch
// cadence (pure functions of the spawn column via bowserHash and the
// game tick — no RNG, no counters), BaseY is the lava surface.
type Podoboo struct {
	Pos, Vel Vec
	W, H     float64
	Phase    int
	Period   int
	BaseY    float64
	Gone     bool
}

// newPodoboo builds the leaper for a lava pool: x is the pool's centre
// column, surfaceY the lava surface row. The body starts hidden below
// the surface, waiting for its phase window.
func newPodoboo(x float64, surfaceY int) *Podoboo {
	period := 150 + int(bowserHash(int(x), 7)%60)
	p := &Podoboo{
		W:      PodobooW,
		H:      PodobooH,
		Period: period,
		Phase:  int(bowserHash(int(x), 13)) % period,
		BaseY:  float64(surfaceY) + 0.5,
	}
	p.Pos = Vec{x + (1-PodobooW)/2, p.BaseY + PodobooRestDrop}
	return p
}

// updatePodoboos advances every podoboo one tick: launch on the phase
// window, free flight, rest below the surface until the next window.
// Player contact while risen is an unconditional kill — the one hazard
// star power does not touch.
func (g *Game) updatePodoboos() {
	p := g.Player
	for _, pd := range g.Podoboos {
		if pd.Gone {
			continue
		}
		if (g.Tick+pd.Phase)%pd.Period == 0 {
			pd.Vel.Y = PodobooJumpVel
		}
		if pd.Vel.Y == 0 {
			continue // resting under the surface
		}
		pd.Vel.Y += Gravity // unclamped: free flight up and down
		pd.Pos.Y += pd.Vel.Y
		if pd.Vel.Y > 0 && pd.Pos.Y > pd.BaseY+PodobooHideAt {
			// Back under the surface: rest until the next window.
			pd.Pos.Y = pd.BaseY + PodobooRestDrop
			pd.Vel.Y = 0
			continue
		}
		if pd.Pos.Y < pd.BaseY+PodobooHideAt &&
			overlap(p.Pos.X, p.Pos.Y, p.W, p.H, pd.Pos.X, pd.Pos.Y, pd.W, pd.H) {
			g.kill() // star does NOT protect against lava
		}
	}
}
