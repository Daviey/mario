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
	FlowerW, FlowerH     = 0.9, 0.9
	FireballW, FireballH = 0.45, 0.45
	PlantW, PlantH       = 0.7, 1.35
	CoinSize             = 0.6
	MushroomEmergeStep   = 0.85 / MushroomEmergeTicks
	FlowerEmergeStep     = 0.85 / FlowerEmergeTicks
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
	WalkFrameLen    = 0.55 // tiles travelled per walk-cycle animation frame
	JumpBufferTicks = 5

	EnemyWalk           = 0.030
	ShellSpeed          = 0.22
	MushroomWalk        = 0.035
	MushroomEmergeTicks = 26
	FlowerEmergeTicks   = 26
	FireballSpeed       = 0.20
	FireballBounce      = -0.16
	FireballGravity     = 0.014
	FireballLife        = 240 // ~4s of bouncing before sputtering out
	MaxFireballs        = 2
	PlantRiseTicks      = 26
	PlantUpTicks        = 90
	PlantHiddenTicks    = 100
	PlantMercyDist      = 2.4 // stay hidden while the player stands this close
	StompBounce         = -0.28
	InvincibleTicks     = 120
	EnemyFrameLen       = 0.16 // tiles travelled per enemy walk-cycle frame

	FlagSlideSpeed  = 0.05 // tiles per tick down the pole
	CastleWalkSpeed = 0.07 // auto-walk to the castle door
)

// Scoring.
const (
	CoinScore        = 200
	StompScore       = 100
	BrickScore       = 50
	MushroomScore    = 1000
	FlowerScore      = 1000
	TimeBonusPerUnit = 10
)

// stompLadder is the consecutive-airborne-stomp score ladder; once past
// the end, every further stomp in the same chain is a 1-UP.
var stompLadder = [...]int{100, 200, 400, 500, 800, 1000, 2000, 4000, 5000, 8000}

// PowerLevel is the player's power-up state.
type PowerLevel uint8

const (
	PowerSmall PowerLevel = iota
	PowerSuper
	PowerFire
)

// Player is the controllable character.
type Player struct {
	Pos, Vel   Vec
	W, H       float64
	Facing     int // 1 right, -1 left
	Grounded   bool
	Power      PowerLevel
	Invincible int     // post-hit invincibility ticks
	WalkDist   float64 // ground distance travelled, drives the leg cycle
	Skidding   bool    // turning against horizontal motion while grounded
	StretchT   int     // jump-stretch pose countdown (ticks)
	SquashT    int     // land-squash pose countdown (ticks)

	stompChain  int // airborne stomp combo; resets on landing
	groundTimer int // ticks since last grounded (coyote time)
	jumpBuffer  int // ticks a jump press remains queued
	jumping     bool
}

func newPlayer(start Vec, power PowerLevel) *Player {
	p := &Player{Facing: 1, Power: power}
	if power >= PowerSuper {
		p.W, p.H = SuperW, SuperH
	} else {
		p.W, p.H = SmallW, SmallH
	}
	// Keep the feet where the spawn row's floor is regardless of size.
	p.Pos = Vec{start.X, start.Y - (p.H - SmallH)}
	return p
}

// grow makes a small player super, keeping the feet planted. Fire stays fire.
func (p *Player) grow() {
	if p.Power >= PowerSuper {
		return
	}
	p.Pos.Y -= SuperH - p.H
	p.W, p.H = SuperW, SuperH
	p.Power = PowerSuper
}

// fireUp upgrades the player to fire; a small player becomes super-sized.
func (p *Player) fireUp() {
	p.grow()
	p.Power = PowerFire
}

// shrink drops the player to small, keeping the feet planted.
func (p *Player) shrink() {
	p.Pos.Y += p.H - SmallH
	p.W, p.H = SmallW, SmallH
	p.Power = PowerSmall
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
	Pos      Vec
	Vel      Vec
	W, H     float64
	Kind     EnemyKind
	State    EnemyState
	Dir      int
	Timer    int     // squashed-pose countdown
	WalkDist float64 // drives the walk cycle
	Chain    int     // consecutive kills by this sliding shell (combo ladder)
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
	Pos  Vec
	Gone bool
}

// Mushroom is a power-up that emerges from a block and then walks.
type Mushroom struct {
	Pos    Vec
	Vel    Vec
	Dir    int
	Emerge int
	Gone   bool
}

// FireFlower is the fire power-up: emerges from its block, then waits.
type FireFlower struct {
	Pos    Vec
	Emerge int
	Gone   bool
}

// Fireball is a thrown fire projectile: bounces along the ground, kills
// on contact, sputters on walls, at most MaxFireballs alive at once.
type Fireball struct {
	Pos, Vel Vec
	Life     int
	Gone     bool
}

// PlantState is the piranha-plant lifecycle.
type PlantState uint8

const (
	PlantHidden PlantState = iota
	PlantRising
	PlantUp
	PlantSinking
)

// Plant is a piranha plant living in a pipe: rises, waits, sinks and hides
// on a cycle — and mercifully stays down while the player stands near.
type Plant struct {
	Pos   Vec // top-left; Pos.Y rests at BaseY (the pipe mouth) when hidden
	BaseY float64
	State PlantState
	Timer int
	Gone  bool
}

func newPlant(spawn Vec) *Plant {
	return &Plant{Pos: Vec{spawn.X, spawn.Y}, BaseY: spawn.Y, State: PlantHidden, Timer: PlantHiddenTicks}
}

// ParticleKind discriminates visual particles.
type ParticleKind uint8

const (
	ParticleCoin ParticleKind = iota
	ParticleDebris
	ParticleSparkle
	ParticleDust
	ParticleScore // floating score popup (Val; 0 means "1UP")
)

// Particle is a purely visual effect (coin pop, brick debris, sparkle,
// score popup).
type Particle struct {
	Pos, Vel Vec
	Life     int
	Kind     ParticleKind
	Val      int // ParticleScore: the number shown, 0 = "1UP"
}

// overlap reports whether two AABBs intersect.
func overlap(ax, ay, aw, ah, bx, by, bw, bh float64) bool {
	return ax < bx+bw && bx < ax+aw && ay < by+bh && by < ay+ah
}
