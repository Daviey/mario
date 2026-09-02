package engine

import "math"

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
	BowserW, BowserH     = 1.9, 1.9 // the boss: a two-tile wall of shell
	BossFireW, BossFireH = 0.9, 0.5
	CoinSize             = 0.6
	MushroomEmergeStep   = 0.85 / MushroomEmergeTicks
	FlowerEmergeStep     = 0.85 / FlowerEmergeTicks
)

// Physics tuning (per 60 Hz tick).
const (
	Gravity = 0.021
	MaxFall = 0.32
	JumpVel = -0.43 // full jump clears ~4.4 tiles
	JumpCut = -0.15 // velocity clamp when the jump key is released early
	// Level-authoring clearances, both hanging off the full jump: the
	// JumpVel apex is ~4.4 tiles (a running jump covers ~5), so a pit
	// wider than MaxPitWidth is unjumpable and a shelf higher than
	// MaxShelfRise above the floor is unmountable. The generator and
	// the level tests both clamp to these.
	MaxPitWidth     = 4 // widest pit a full-speed running jump still clears
	MaxShelfRise    = 4 // tallest shelf a jump mounts from the floor
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
	EnemyFrameLen       = 0.16  // tiles travelled per enemy walk-cycle frame
	ParaHopVel          = -0.34 // flying-koopa hop impulse (~2.7-tile apex)
	ParaHopEvery        = 48    // grounded ticks between flying-koopa hops
	ParaRedFlyVel       = 0.03  // red paratroopa vertical flight pace
	ParaRedRange        = 3.0   // red paratroopa bob extremes around BaseY

	// Interaction feel: the contact tolerances that decide stomp vs.
	// hurt, kick direction and landing feedback.
	StompBodyFrac      = 0.7   // feet above this fraction of an enemy's body counts as a stomp
	HiddenGrazeOverlap = 0.15  // hidden blocks trigger only past this much tile overlap; corner grazes pass through
	KickDeadzone       = 0.1   // centre distance within which a kick keeps the shell's last direction
	HardLandFall       = 0.12  // landing fall speed that plays the squash pose and twin dust puffs
	BumpFlipTol        = 0.2   // how far an enemy's feet may sit off the block top to still ride a bump
	FlipVel            = -0.28 // pop-up impulse when an enemy is knocked on its back
	DeathBounceVel     = -0.38 // death-arc launch impulse at the freeze-frame beat

	// Pose pacing: how long the purely visual beats hold. These set
	// pose countdowns, corpse holds and bump animations; physics reads
	// none of them.
	StretchPoseTicks = 6  // jump-stretch pose after liftoff
	SquashPoseTicks  = 6  // hard-landing squash pose
	BumpAnimTicks    = 8  // block bump animation length
	SquashHoldTicks  = 30 // squashed-enemy corpse hold before it pops

	FireBarBallGap  = 0.55  // tiles between fire-bar balls
	FireBarBallSize = 0.45  // collision box of one ball
	FireBarLen      = 6     // balls per bar
	FireBarSpeed    = 0.026 // radians per tick (~4s per revolution)
	FireBarHubClear = 0.45  // hub centre to the first ball's centre; the ball clears the hub

	StarTicks  = 600 // star power: ~10s of kill-on-touch
	StarScore  = 1000
	StarWalk   = 0.055
	StarBounce = -0.22

	FlagSlideSpeed  = 0.05     // tiles per tick down the pole
	CastleWalkSpeed = 0.07     // auto-walk to the castle door
	CastleHopVel    = -0.22    // hop off the pole base towards the castle door
	CastleFlagRise  = 1.0 / 60 // victory-flag rise per tick: full hoist in one second
)

// Scoring.
const (
	CoinScore        = 200
	StompScore       = 100
	BrickScore       = 50
	MushroomScore    = 1000
	FlowerScore      = 1000
	TimeBonusPerUnit = 10
	CheepScore       = 200 // cheep cheep: stomp (leaping) or fireball
	BlooberScore     = 200 // bloober: fireball or star only
)

// stompLadder is the consecutive-airborne-stomp score ladder; once past
// the end, every further stomp in the same chain is a 1-UP.
var stompLadder = [...]int{100, 200, 400, 500, 800, 1000, 2000, 4000, 5000, 8000}

// PowerLevel is the player's power-up state.
type PowerLevel uint8

// Power levels, weakest first; the order matters — PowerSuper and above
// stand two tiles tall.
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
	Star       int     // star-power ticks: invincible, kills on touch
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

// Enemy species. KindPara hops as it walks and demotes to KindKoopa when
// stomped. KindBuzzy (the beetle) wears a fireproof shell: fireballs die
// on it with no effect, a stomp makes the shell like a koopa's.
const (
	KindGoomba EnemyKind = iota
	KindKoopa
	KindPara  // flying koopa: hops while walking; a stomp demotes it to koopa
	KindBuzzy // buzzy beetle: fire-immune; a stomp makes the shell
)

// EnemyState is the lifecycle state of an enemy.
type EnemyState uint8

// Enemy lifecycle: walking alive, squashed (goomba death pose, counts
// down to Gone), idle shell, player-slid shell, flipped onto its back
// (already dead, falling out of the world).
const (
	EnemyWalking EnemyState = iota
	EnemySquashed
	EnemyShell
	EnemyShellMoving
	EnemyFlipped
)

// Enemy is a goomba or koopa (koopas become shells when stomped).
type Enemy struct {
	Pos   Vec
	Vel   Vec
	W, H  float64
	Kind  EnemyKind
	State EnemyState
	Dir   int
	// Red marks the red variants: a red koopa probes ahead and turns
	// at ledges instead of walking off; a red paratroopa flies in
	// place, bobbing vertically around BaseY.
	Red   bool
	BaseY float64 // red paratroopa flight centre (the spawn Y)
	Timer int     // squashed-pose countdown; grounded paratroopas reuse it as hop charge
	// (a red paratroopa reuses it as the flight direction sign)
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

func newPara(p Vec) *Enemy {
	return &Enemy{Pos: p, W: KoopaW, H: KoopaH, Kind: KindPara, State: EnemyWalking, Dir: -1}
}

func newBuzzy(p Vec) *Enemy {
	return &Enemy{Pos: p, W: KoopaW, H: KoopaH, Kind: KindBuzzy, State: EnemyWalking, Dir: -1}
}

func newKoopaRed(p Vec) *Enemy {
	return &Enemy{Pos: p, W: KoopaW, H: KoopaH, Kind: KindKoopa, State: EnemyWalking, Dir: -1, Red: true}
}

// newParaRed is the vertical flier of the athletic levels: no gravity,
// no walking — it bobs around BaseY (Timer carries the flight sign,
// starting rising). A stomp demotes it to a red koopa (edge-turner).
func newParaRed(p Vec) *Enemy {
	return &Enemy{Pos: p, W: KoopaW, H: KoopaH, Kind: KindPara, State: EnemyWalking, Dir: -1, Red: true, BaseY: p.Y, Timer: -1}
}

// CoinItem is a collectible coin floating in the world.
type CoinItem struct {
	Pos  Vec
	Gone bool
}

// MushroomKind discriminates block power-ups that walk like a mushroom.
type MushroomKind uint8

// Block power-ups that emerge from a bumped block and then walk off.
const (
	MushSuper MushroomKind = iota // grow to super
	MushLife                      // 1-UP
	MushStar                      // star power
)

// Mushroom is a power-up that emerges from a block and then walks: the
// super mushroom, the 1-UP mushroom and the bouncing star.
type Mushroom struct {
	Pos    Vec
	Vel    Vec
	Dir    int
	Emerge int
	Kind   MushroomKind
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

// The piranha-plant cycle, starting hidden in its pipe.
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

// FireBar is a rotating castle hazard: a chain of fireballs pivoting on a
// hub. Angle advances deterministically with the game tick.
type FireBar struct {
	X, Y  float64 // hub centre in tile coords
	Speed float64 // radians per tick; negative reverses direction
}

// NewFireBar builds a bar whose hub centre is the centre of cell (x, y).
// Every other bar (by hub column) spins the other way, a touch faster —
// variety without extra level syntax.
func NewFireBar(x, y float64) FireBar {
	speed := FireBarSpeed
	if int(x)%2 == 1 {
		speed = -1.5 * FireBarSpeed
	}
	return FireBar{X: x + 0.5, Y: y + 0.5, Speed: speed}
}

// BallPos returns the centre of ball i (0-based, nearest the hub first)
// at the given tick. The radius law here — hub clearance plus one ball
// gap per ball — is what spawnThreatNear (level.go) mirrors to keep
// checkpoints out of a bar's sweep; change them together.
func (fb FireBar) BallPos(i, tick int) Vec {
	angle := fb.Speed * float64(tick)
	r := FireBarHubClear + float64(i+1)*FireBarBallGap
	return Vec{fb.X + r*math.Cos(angle), fb.Y + r*math.Sin(angle)}
}

// MaxReach returns how far a bar's sweep extends from its hub centre:
// hub clearance, one ball gap per ball, plus half a ball for the
// outermost ball's collision box — the single derivation of the sweep
// radius that spawnThreatNear (level.go) keeps checkpoints outside of.
// It is bar-independent (every bar shares FireBarLen), so the receiver
// is the law's home, not input.
func (fb FireBar) MaxReach() float64 {
	return FireBarHubClear + float64(FireBarLen)*FireBarBallGap + FireBarBallSize/2
}

// BowserState is the boss lifecycle.
type BowserState uint8

// Bowser lifecycle: on the bridge cycling hop and fire, mouth open
// (the fire telegraph), then — support gone or killed — falling with no
// collision, and finally sinking in the lava.
const (
	BowserIdle BowserState = iota
	BowserMouth
	BowserFalling
	BowserSinking
)

// Bowser is the castle boss: hops and breathes fire on the bridge, dies
// to five fireballs (or star power, or the axe) and never counts as a
// stomp. His moods roll off bowserHash (bowser.go), so the whole fight
// is a pure function of the tick stream.
type Bowser struct {
	Pos, Vel Vec
	W, H     float64
	Dir      int     // -1: faces the approaching player (left)
	HomeX    float64 // patrol clamp origin (spawn X); clamps [HomeX-BowserPatrol, HomeX]
	State    BowserState
	Timer    int  // action countdown / sink countdown
	Clock    int  // lifetime ticks: deterministic mood input
	HP       int  // starts at BowserFireHP
	Flash    int  // hit-flash countdown (render brightens)
	Flipped  bool // killed by fireballs/star: render upside down
	Gone     bool

	// Disguise is the impostor's true form (worlds 1-4/2-4/3-4 field a
	// goomba, koopa or buzzy in a Bowser suit). KindGoomba (0) means a
	// real boss; anything else reveals a flipped corpse of that species
	// when a combat kill lands (killBowser).
	Disguise EnemyKind
}

func newBowser(spawn Vec) *Bowser {
	return &Bowser{Pos: spawn, W: BowserW, H: BowserH, Dir: -1, HomeX: spawn.X, HP: BowserFireHP}
}

// dead reports whether the bowser is a corpse — killed by fireballs or
// star (Flipped), or already dropping/sunk. Corpses deal no contact
// damage and shrug off further fireballs.
func (b *Bowser) dead() bool {
	return b.Gone || b.Flipped || b.State == BowserFalling || b.State == BowserSinking
}

// BossFire is one breath of Bowser fire: flies flat or rides a sine
// wave, burns the player on touch and persists through the hit.
type BossFire struct {
	Pos, Vel Vec
	BaseY    float64 // wave centre
	Wave     bool
	Life     int
	Gone     bool
}

// ParticleKind discriminates visual particles.
type ParticleKind uint8

// Visual particle kinds; purely decorative, never gameplay state.
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
	Pos  Vec
	Vel  Vec
	Life int
	Kind ParticleKind
	Val  int // ParticleScore: the number to float (0 means "1UP")
}

// overlap reports whether two AABBs intersect.
func overlap(ax, ay, aw, ah, bx, by, bw, bh float64) bool {
	return ax < bx+bw && bx < ax+aw && ay < by+bh && by < ay+ah
}
