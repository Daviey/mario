package engine

import "fmt"

// The daily challenge: a level generated deterministically from a date
// seed, so every player worldwide plays the same layout on the same day.
// The generator is a tiny xorshift PRNG driving a segment grammar over the
// same Builder the hand-built levels use — no wall clock, no math/rand,
// fully reproducible from the seed alone.

type dailyRng struct {
	s uint64
}

func (r *dailyRng) next() uint64 {
	x := r.s
	x ^= x >> 12
	x ^= x << 25
	x ^= x >> 27
	r.s = x
	return x * 0x2545F4914F6CDD1D
}

// intn returns a deterministic int in [0, n). Deliberately naive modulo:
// for the small n values used here the bias is ~2^-58, and the segment
// weights below are tuned around this exact mapping.
func (r *dailyRng) intn(n int) int { return int(r.next() % uint64(n)) }

// DailySeed folds a calendar date into a generator seed.
func DailySeed(year, month, day int) uint64 {
	v := uint64(year*10000 + month*100 + day)
	return v*2654435761 ^ 0x9E3779B97F4A7C15
}

// DailyLevelFor builds the challenge level for a calendar date.
func DailyLevelFor(year, month, day int) *Level {
	l := DailyLevel(DailySeed(year, month, day))
	l.Name = fmt.Sprintf("%02d-%02d", month, day)
	return l
}

// DailyLevel generates the challenge level for a raw seed. Solvability is
// guaranteed structurally, using the same clearances the hand-built levels
// rely on: pits at most 4 tiles (a full-speed running jump covers ~5),
// pipes at most 3 tall, mid-level stair sets at most 4 steps.
func DailyLevel(seed uint64) *Level {
	r := &dailyRng{s: seed | 1}
	const w = 176
	b := NewBuilder(w, LevelHeight)
	if r.intn(3) == 0 {
		b.Theme(ThemeUnderground)
		b.Fill(0, 0, w-1, 0, 'B')
	}

	b.Set(3, 12, 'M')
	b.Set(8, 9, '?')
	b.Ground(0, 11)

	groundEnd := 12
	x := 12
	const finalReserve = 26
	firePlaced := false

	segment := func() int {
		// Weighted segment picker; returns the segment kind.
		switch v := r.intn(100); {
		case v < 16:
			return 0 // pit
		case v < 32:
			return 1 // pipe (maybe with a plant)
		case v < 52:
			return 2 // block row + coins
		case v < 64:
			return 3 // enemy cluster
		case v < 76:
			return 4 // stair pair
		case v < 88:
			return 5 // high platform + coins
		default:
			return 6 // breather
		}
	}

	for x < w-finalReserve {
		need := w - finalReserve - x
		switch segment() {
		case 0: // pit: 2-4 wide, flanked by flat ground
			pw := 2 + r.intn(3)
			if need < pw+8 {
				continue
			}
			for tx := groundEnd; tx < x; tx++ {
				b.Ground(tx, tx)
			}
			groundEnd = x + pw // the gap
			x += pw + 3
		case 1: // pipe 1-3 tall; plant 40%, only with breathing room
			h := 1 + r.intn(3)
			if need < h+6 {
				continue
			}
			for tx := groundEnd; tx <= x+1; tx++ {
				b.Ground(tx, tx)
			}
			groundEnd = x + 2
			b.Pipe(x, h)
			if r.intn(5) < 2 {
				b.Plant(x, h)
			}
			x += 2 + 2 + r.intn(3)
		case 2: // block row at jump height with coins above
			n := 3 + r.intn(5)
			if need < n+4 {
				continue
			}
			for tx := groundEnd; tx < x+n+1; tx++ {
				b.Ground(tx, tx)
			}
			groundEnd = x + n + 1
			row := 9 - r.intn(2)
			for i := range n {
				ch := byte('B')
				switch {
				case i == n/2 && !firePlaced && r.intn(4) == 0:
					ch = 'f'
					firePlaced = true
				case i == n/2 && r.intn(3) == 0:
					ch = 'U'
				case i != 0 && i != n-1 && r.intn(4) == 0:
					ch = '?'
				}
				b.Set(x+i, row, ch)
			}
			if r.intn(2) == 0 {
				xs := make([]int, 0, n)
				for i := range n {
					xs = append(xs, x+i)
				}
				b.Coins(row-1, xs...)
			}
			if r.intn(3) == 0 {
				b.Set(x+n/2, 12, 'G')
			}
			x += n + 3
		case 3: // enemy cluster on flat ground
			n := 1 + r.intn(3)
			if need < n+4 {
				continue
			}
			for tx := groundEnd; tx < x+n+2; tx++ {
				b.Ground(tx, tx)
			}
			groundEnd = x + n + 2
			for i := range n {
				if r.intn(3) == 0 {
					b.Set(x+2*i, 12, 'K')
				} else {
					b.Set(x+2*i, 12, 'G')
				}
			}
			x += n + 3
		case 4: // stair pair (up then down), max 4 steps mid-level
			h := 3 + r.intn(2)
			if need < 2*h+4 {
				continue
			}
			for tx := groundEnd; tx < x+2*h+2; tx++ {
				b.Ground(tx, tx)
			}
			groundEnd = x + 2*h + 2
			b.StairsUp(x, h)
			b.StairsDown(x+h, h)
			x += 2*h + 3
		case 5: // floating brick platform with coins
			n := 3 + r.intn(3)
			if need < n+4 {
				continue
			}
			for tx := groundEnd; tx < x+n+1; tx++ {
				b.Ground(tx, tx)
			}
			groundEnd = x + n + 1
			row := 10 - r.intn(3) // 10, 9 or 8
			b.Fill(x, row, x+n-1, row, 'B')
			xs := make([]int, 0, n)
			for i := range n {
				xs = append(xs, x+i)
			}
			b.Coins(row-2, xs...)
			x += n + 3
		default: // breather: plain ground
			n := 3 + r.intn(3)
			for tx := groundEnd; tx < x+n; tx++ {
				b.Ground(tx, tx)
			}
			groundEnd = x + n
			x += n
		}
	}

	// Finale: flat run-up, the classic 8-step staircase, flag and castle.
	for tx := groundEnd; tx < w; tx++ {
		b.Ground(tx, tx)
	}
	b.StairsUp(w-22, 8)
	b.Flag(w - 8)
	return mustLevel("DAILY", b)
}
