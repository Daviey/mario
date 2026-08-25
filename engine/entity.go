package engine

// Vec is a 2D vector (world units; one tile == 1.0). Y grows downward.
type Vec struct{ X, Y float64 }

// Entity sizes in tiles. SMB proportions: small player is 1x1 tile,
// super is 1x2, so sprites have a square footprint on screen.
const (
	SmallW, SmallH       = 0.8, 1.0
	SuperW, SuperH       = 0.8, 2.0
	GoombaW, GoombaH     = 0.9, 0.9
	KoopaW, KoopaH       = 0.9, 1.3
	MushroomW, MushroomH = 0.9, 0.9
	CoinSize             = 0.6
	MushroomEmergeStep   = 0.85 / MushroomEmergeTicks
)

// Physics tuning (per 60 Hz tick).
const (
	Gravity         = 0.021
	MaxFall         = 0.32
	JumpVel         = -0.43 // full jump clears ~4.4 tiles
	JumpCut         = -0.15 // velocity clamp when the jump key is released early
	WalkAccel       = 0.006
	RunAccel        = 0.009
	MaxWalk         = 0.085
	MaxRun          = 0.145
	Friction        = 0.010
	AirFactor       = 0.85
	CoyoteTicks     = 5
	JumpBufferTicks = 5

	EnemyWalk           = 0.030
	ShellSpeed          = 0.22
	MushroomWalk        = 0.035
	MushroomEmergeTicks = 26
	StompBounce         = -0.28
	InvincibleTicks     = 120
)

// Scoring.
const (
	CoinScore        = 200
	StompScore       = 100
	BrickScore       = 50
	MushroomScore    = 1000
	TimeBonusPerUnit = 10
)

// Player is the controllable character.
type Player struct {
	Pos, Vel   Vec
	W, H       float64
	Facing     int // 1 right, -1 left
	Grounded   bool
	Super      bool
	Invincible int // post-hit invincibility ticks

	groundTimer int // ticks since last grounded (coyote time)
	jumpBuffer  int // ticks a jump press remains queued
	jumping     bool
}

func newPlayer(start Vec, super bool) *Player {
	p := &Player{Facing: 1}
	if super {
		p.Super = true
		p.W, p.H = SuperW, SuperH
	} else {
		p.W, p.H = SmallW, SmallH
	}
	// Keep the feet where the spawn row's floor is regardless of size.
	p.Pos = Vec{start.X, start.Y - (p.H - SmallH)}
	return p
}

// grow makes a small player super, keeping the feet planted.
func (p *Player) grow() {
	if p.Super {
		return
	}
	p.Pos.Y -= SuperH - p.H
	p.W, p.H = SuperW, SuperH
	p.Super = true
}

// shrink makes a super player small, keeping the feet planted.
func (p *Player) shrink() {
	p.Pos.Y += p.H - SmallH
	p.W, p.H = SmallW, SmallH
	p.Super = false
}

// EnemyKind discriminates enemy species.
type EnemyKind uint8

const (
	KindGoomba EnemyKind = iota
	KindKoopa
)

// EnemyState is the lifecycle state of an enemy.
type EnemyState uint8

const (
	EnemyWalking EnemyState = iota
	EnemySquashed
	EnemyShell
	EnemyShellMoving
	EnemyFlipped
)

// Enemy is a goomba or koopa (koopas become shells when stomped).
type Enemy struct {
	Pos, Vel Vec
	W, H     float64
	Kind     EnemyKind
	State    EnemyState
	Dir      int // walk direction: 1 or -1
	Timer    int // squashed animation countdown
	Gone     bool
}

func newGoomba(p Vec) *Enemy {
	return &Enemy{Pos: p, W: GoombaW, H: GoombaH, Kind: KindGoomba, State: EnemyWalking, Dir: -1}
}

func newKoopa(p Vec) *Enemy {
	return &Enemy{Pos: p, W: KoopaW, H: KoopaH, Kind: KindKoopa, State: EnemyWalking, Dir: -1}
}

// CoinItem is a collectible coin floating in the world.
type CoinItem struct {
	Pos  Vec // top-left of the 0.6x0.6 coin box
	Gone bool
}

// Mushroom is a power-up that emerges from a block and then walks.
type Mushroom struct {
	Pos, Vel Vec
	Dir      int
	Emerge   int // ticks remaining of the rise-out-of-block phase
	Gone     bool
}

// ParticleKind discriminates visual particles.
type ParticleKind uint8

const (
	ParticleCoin ParticleKind = iota
	ParticleDebris
	ParticleSparkle
)

// Particle is a purely visual effect (coin pop, brick debris, sparkle).
type Particle struct {
	Pos, Vel Vec
	Life     int
	Kind     ParticleKind
}

// overlap reports whether two AABBs intersect.
func overlap(ax, ay, aw, ah, bx, by, bw, bh float64) bool {
	return ax < bx+bw && bx < ax+aw && ay < by+bh && by < ay+ah
}
