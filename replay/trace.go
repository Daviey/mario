package replay

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/Daviey/mario/engine"
)

// Trace replays a recorded stream against a fresh game and writes a
// human-readable tick trace to w: every state transition with lives,
// score, position and clock, plus a cause dump at each death (overlapping
// enemies, plants, lava tiles in the frame, the fall and the clock).
//
// It exists for diagnosing leaderboard drops end to end: dump a row's
// recording with `mario -dump-replays`, then `mario -replay FILE` it and
// read where the live run and the recording disagree. A recording made on
// an older EngineVersion traces "coherently but differently" — that is
// version skew, not a determinism bug; the verifier drops those rows as
// `version` on purpose.
func Trace(levels []*engine.Level, mode, data string, w io.Writer) (Result, error) {
	return trace(levels, mode, data, w, nil)
}

// TraceWindow traces like Trace and additionally writes one line per
// playing tick whose player X falls in the half-open [x0,x1): the raw
// input bits, position, velocity and grounded flag captured BEFORE that
// tick's update, with a JUMP PRESS marker on jump-press edges.
//
// This is the per-tick view state transitions cannot show — the tool for
// "impossible jump" postmortems, where the answer is usually invisible at
// transition granularity (releasing the run key at the jump press clamps
// airspeed to walk max one tick after a textbook-looking takeoff, and the
// range dies with it).
func TraceWindow(levels []*engine.Level, mode, data string, w io.Writer, x0, x1 float64) (Result, error) {
	return trace(levels, mode, data, w, &xwin{x0: x0, x1: x1})
}

// xwin is a trace window: half-open [x0,x1) in player-X tiles.
type xwin struct{ x0, x1 float64 }

func (w xwin) in(x float64) bool { return x >= w.x0 && x < w.x1 }

// i01 renders a bool for the per-tick input columns.
func i01(b bool) int {
	if b {
		return 1
	}
	return 0
}

// trace is Trace/TraceWindow's shared core; win != nil adds the per-tick
// window log on top of the transition/death trace.
func trace(levels []*engine.Level, mode, data string, w io.Writer, win *xwin) (Result, error) {
	inputs, err := decode(data)
	if err != nil {
		return Result{}, err
	}
	g, err := newReplayGame(levels, mode)
	if err != nil {
		return Result{}, err
	}
	prev := g.State
	prevLives := g.Lives
	prevUp := false
	for i, in := range inputs {
		if win != nil && g.State == engine.StatePlaying && win.in(g.Player.Pos.X) {
			note := ""
			if in.Up && !prevUp {
				note = "  <-- JUMP PRESS"
			}
			p := g.Player
			fmt.Fprintf(w, "tick %5d lvl=%d in(L=%d R=%d U=%d D=%d RUN=%d)%s pos=(%7.3f,%7.3f) vel=(%7.3f,%7.3f) grd=%t\n",
				i, g.LevelIndex()+1, i01(in.Left), i01(in.Right), i01(in.Up), i01(in.Down), i01(in.Run),
				note, p.Pos.X, p.Pos.Y, p.Vel.X, p.Vel.Y, p.Grounded)
		}
		g.Update(in)
		if g.State != prev || g.Lives != prevLives {
			fmt.Fprintf(w, "tick %5d: %-11s -> %-11s lives=%d score=%-6d px=%7.2f py=%7.2f time=%d%s\n",
				i, prev, g.State, g.Lives, g.Score, g.Player.Pos.X, g.Player.Pos.Y, g.Time,
				traceInputNote(in))
			if g.State == engine.StateDying && prev == engine.StatePlaying {
				traceDeathCause(w, g)
			}
			prev, prevLives = g.State, g.Lives
		}
		prevUp = in.Up
	}
	res := Result{Score: g.Score, Level: g.LevelIndex() + 1, State: g.State}
	fmt.Fprintf(w, "END: score=%d level=%d state=%v lives=%d ticks=%d\n",
		res.Score, res.Level, res.State, g.Lives, len(inputs))
	return res, nil
}

// ParseWindow parses a -trace-window argument "X0:X1" (player-X tiles,
// half-open, X1 > X0).
func ParseWindow(s string) (float64, float64, error) {
	a, b, ok := strings.Cut(s, ":")
	if !ok {
		return 0, 0, fmt.Errorf("bad window %q (want X0:X1)", s)
	}
	x0, err := strconv.ParseFloat(a, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("bad window %q (want X0:X1): %v", s, err)
	}
	x1, err := strconv.ParseFloat(b, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("bad window %q (want X0:X1): %v", s, err)
	}
	if x1 <= x0 {
		return 0, 0, fmt.Errorf("bad window %q (X1 must exceed X0)", s)
	}
	return x0, x1, nil
}

// traceInputNote flags the edge-triggered inputs that reshape a trace.
func traceInputNote(in engine.Input) string {
	var notes []string
	if in.Suicide {
		notes = append(notes, " suicide")
	}
	if in.Restart {
		notes = append(notes, " restart")
	}
	if in.Pause {
		notes = append(notes, " pause")
	}
	return strings.Join(notes, "")
}

// traceDeathCause prints every lethal fact about the player's body at the
// moment of death: what overlapped it, what burns under it, whether it
// fell or timed out. updatePlaying kills in a fixed order, but the trace
// lists all findings — a diagnosis, not a re-derivation.
func traceDeathCause(w io.Writer, g *engine.Game) {
	p := g.Player
	var causes []string
	for _, e := range g.Enemies {
		if e.Gone {
			continue
		}
		if e.Pos.X < p.Pos.X+p.W && e.Pos.X+e.W > p.Pos.X &&
			e.Pos.Y < p.Pos.Y+p.H && e.Pos.Y+e.H > p.Pos.Y {
			causes = append(causes, fmt.Sprintf("enemy %v state=%v at (%.2f,%.2f)", e.Kind, e.State, e.Pos.X, e.Pos.Y))
		}
	}
	for _, pl := range g.Plants {
		if pl.Gone {
			continue
		}
		if pl.Pos.X < p.Pos.X+p.W && pl.Pos.X+engine.PlantW > p.Pos.X &&
			pl.Pos.Y < p.Pos.Y+p.H && pl.Pos.Y+engine.PlantH > p.Pos.Y {
			causes = append(causes, fmt.Sprintf("plant at (%.2f,%.2f)", pl.Pos.X, pl.Pos.Y))
		}
	}
	tx, ty := int(p.Pos.X), int(p.Pos.Y)
	for y := ty - 1; y <= ty+1; y++ {
		for x := tx - 1; x <= tx+1; x++ {
			if g.Level.At(x, y) == engine.Lava {
				causes = append(causes, fmt.Sprintf("lava tile at (%d,%d)", x, y))
			}
		}
	}
	if p.Pos.Y > float64(g.Level.Height)+1 {
		causes = append(causes, "fell below the level")
	}
	if g.Time == 0 {
		causes = append(causes, "clock ran out")
	}
	for _, c := range causes {
		fmt.Fprintf(w, "    cause: %s\n", c)
	}
	if len(causes) == 0 {
		fmt.Fprintf(w, "    cause: none found (input-driven or stomp-miss death)\n")
	}
}
