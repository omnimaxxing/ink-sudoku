package engine

import (
	"math/bits"
	"math/rand/v2"
)

// Puzzle is a generated puzzle with its solution and grade.
type Puzzle struct {
	Givens     Grid
	Solution   Grid
	Seed       uint64
	Level      Level
	Grade      Rating
	Techniques []Technique // techniques the solve uses, easiest first
	Attempts   int         // full grids tried before one graded at Level
}

// Generate makes a puzzle at the given level (Easy..Expert). The same seed
// and level always give the same puzzle. Clues are removed in
// 180°-symmetric pairs.
//
// A pair is only removed if the logical solver, limited to the hardest
// technique the level allows (and, for Easy, never dropping below
// easyMinChoices available singles), can still finish the puzzle. A complete
// logical solve implies a unique solution, so this is stricter than a
// uniqueness check and keeps every carve inside the level's ceiling. The
// result is then graded with all techniques and kept only if it lands on
// the requested level; otherwise a fresh grid is tried.
//
// An Expert puzzle first draws one of expertFamilies from the seed. Its
// carve stops at the family's hardest technique, and it is kept only if the
// grade's hardest step is in the family, so every family turns up about
// equally often.
func Generate(seed uint64, level Level) Puzzle {
	if level > Expert {
		panic("engine: Generate needs Easy, Medium, Hard or Expert, got " + level.String())
	}
	r := newRNG(seed, level)
	floor, ceiling := floors[level], ceilings[level]
	if level == Expert {
		f := expertFamilies[r.intn(len(expertFamilies))]
		floor, ceiling = f[0], f[1]
	}
	sv := new(solver)
	for attempt := 1; ; attempt++ {
		sol := randomSolution(r)
		g := carve(sol, r, level, ceiling)
		if floor > 0 && sv.solves(g, upTo(floor-1)) {
			continue // too easy; the same answer as grading, sooner
		}
		rt := Grade(g)
		if rt.Level != level || rt.Hardest < floor || rt.Hardest > ceiling || CountSolutions(g, 2) != 1 {
			continue
		}
		return Puzzle{
			Givens:     g,
			Solution:   sol,
			Seed:       seed,
			Level:      level,
			Grade:      rt,
			Techniques: rt.Techniques(),
			Attempts:   attempt,
		}
	}
}

// floors is the easiest technique a level's hardest step can be, where
// that alone decides (Easy and Medium also depend on narrowness).
var floors = [...]Technique{Easy: HiddenSingle, Medium: HiddenSingle, Hard: NakedPair, Expert: XWing}

// ceilings is the hardest technique allowed while carving each level.
// Expert carves stop at the top of their family instead.
var ceilings = [...]Technique{
	Easy:   NakedSingle,
	Medium: Claiming,
	Hard:   HiddenQuad,
	Expert: lastTechnique,
}

// expertFamilies are the technique families an Expert puzzle is built
// around, each the first and last technique of its run of the ladder: fish,
// colouring, single-digit patterns and wings.
//
// Fish cost the most to generate: a carve that may only use fish beyond the
// Hard techniques rarely ends up needing one (about 1 in 200 on sudokugen
// runs, against 1 in 10 to 40 for the others), but fresh grids are cheap.
var expertFamilies = [...][2]Technique{
	{XWing, Swordfish},
	{SimpleColouring, SimpleColouring},
	{Skyscraper, TwoStringKite},
	{XYWing, XYZWing},
}

// carve removes symmetric clue pairs from sol in random order, keeping each
// removal only while the puzzle stays solvable with techniques up to
// ceiling.
func carve(sol Grid, r *rng, level Level, ceiling Technique) Grid {
	minChoices := -1
	if level == Easy {
		minChoices = easyMinChoices
	}
	var order [41]uint8 // cell i pairs with 80-i; 40 is the centre
	for i := range order {
		order[i] = uint8(i)
	}
	r.shuffle(order[:])
	g := sol
	sv := new(solver)
	for _, i := range order {
		a, b := int(i), 80-int(i)
		g[a], g[b] = 0, 0
		var ok bool
		if minChoices >= 0 {
			rt, _ := sv.rate(g, upTo(ceiling), minChoices)
			ok = rt.Solved
		} else {
			ok = sv.solves(g, upTo(ceiling))
		}
		if !ok {
			g[a], g[b] = sol[a], sol[b]
		}
	}
	return g
}

// rng draws from PCG directly, with its own bounded-int and shuffle, so
// puzzles for a seed don't change if the stdlib's helpers ever do.
type rng struct{ p *rand.PCG }

func newRNG(seed uint64, level Level) *rng {
	return &rng{rand.NewPCG(seed, 0x9e3779b97f4a7c15^uint64(level))}
}

// intn returns a uniform int in [0, n) (Lemire's method).
func (r *rng) intn(n int) int {
	bound := uint64(n)
	hi, lo := bits.Mul64(r.p.Uint64(), bound)
	if lo < bound {
		thresh := -bound % bound
		for lo < thresh {
			hi, lo = bits.Mul64(r.p.Uint64(), bound)
		}
	}
	return int(hi)
}

func (r *rng) shuffle(s []uint8) {
	for i := len(s) - 1; i > 0; i-- {
		j := r.intn(i + 1)
		s[i], s[j] = s[j], s[i]
	}
}
