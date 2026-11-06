package kakuro

import (
	"math/bits"
	"math/rand/v2"
)

// spec is how a level's puzzles are built.
type spec struct {
	n      int // grid size, clue row and column included
	maxRun int
	// white cells as a percentage of the (n-1)² cells below and right of
	// the clue row and column
	whiteLo, whiteHi int
	// bias weights digits 1-9 when filling a grid. Extreme digits make
	// sums with few combinations (3 in two cells is only 1 + 2), so easier
	// levels lean on them harder.
	bias [9]int
	// budget is how many digit changes the climb tries on one fill.
	budget int
}

var specs = [...]spec{
	Easy:   {n: 7, maxRun: 5, whiteLo: 58, whiteHi: 72, bias: [9]int{8, 5, 3, 2, 1, 2, 3, 5, 8}, budget: 300},
	Medium: {n: 9, maxRun: 6, whiteLo: 58, whiteHi: 70, bias: [9]int{5, 4, 3, 2, 2, 2, 3, 4, 5}, budget: 400},
	Hard:   {n: 11, maxRun: 7, whiteLo: 58, whiteHi: 68, bias: [9]int{3, 3, 2, 2, 2, 2, 2, 3, 3}, budget: 800},
	Expert: {n: 13, maxRun: 8, whiteLo: 58, whiteHi: 68, bias: [9]int{2, 2, 2, 2, 2, 2, 2, 2, 2}, budget: 1000},
}

// fillsPerLayout is how many fills the generator climbs on one layout
// before drawing a new one.
const fillsPerLayout = 3

// Generate returns a puzzle at level whose clues have exactly one solution
// and whose logical solve needs the level's hardest techniques and nothing
// beyond them. The same seed and level always give the same puzzle.
//
// It draws a 180°-symmetric layout, fills it with random digits (distinct
// within each run, leaning on extreme digits at easier levels) and derives
// the clues. Then it climbs: while the logical solver, limited to the
// level's techniques, gets stuck, it changes the digit of a random stuck
// cell (which changes the clues of its two runs) and keeps the change if
// the solver gets no further from done. A complete logical solve implies a
// unique solution, so the climb ends on a unique puzzle within the level's
// ceiling. It is kept if the techniques below the level's floor can't
// finish it, the exact solver confirms uniqueness, and the full grade lands
// on the level; otherwise the climb continues from a fresh fill or layout.
func Generate(seed uint64, level Level) Puzzle {
	if level < Easy || level > Expert {
		panic("kakuro: Generate needs Easy, Medium, Hard or Expert, got " + level.String())
	}
	g := &gen{r: newRNG(seed, level), sp: &specs[level]}
	set := upTo(ceilings[level])
	var below techSet
	if f := floors[level]; f > 0 {
		below = upTo(f - 1)
	}
	for {
		b := g.layout()
		for f := 0; f < fillsPerLayout; f++ {
			if !g.fill(b) {
				break
			}
			if !g.climb(b, set, below) {
				continue
			}
			if n, _ := countSolutions(b, 2); n != 1 {
				continue
			}
			rt := g.sv.rate(b, upTo(lastTechnique))
			if !rt.Solved || rt.Level != level {
				continue
			}
			sol := make([]uint8, b.n*b.n)
			copy(sol, g.sol[:])
			return Puzzle{
				N:          b.n,
				Cells:      b.cells(),
				Solution:   sol,
				Seed:       seed,
				Level:      level,
				Hardest:    rt.Hardest.String(),
				Techniques: rt.Techniques(),
			}
		}
	}
}

type gen struct {
	r   *rng
	sp  *spec
	sol [maxCells]uint8
	sv  solver // solves with the level's techniques
	low solver // checks the puzzle needs them
}

// layout draws a valid layout: 180°-symmetric about the centre of the
// white area, no single-cell runs, runs at most maxRun long, the white
// cells connected, every row and column below and right of the clues used,
// and the white share within the level's range.
func (g *gen) layout() *board {
	for {
		if b := g.tryLayout(); b != nil {
			return b
		}
	}
}

func (g *gen) tryLayout() *board {
	n, sp := g.sp.n, g.sp
	var white [maxCells]bool
	for r := 1; r < n; r++ {
		for c := 1; c < n; c++ {
			white[r*n+c] = true
		}
	}
	interior := (n - 1) * (n - 1)
	count := interior
	lo := interior * sp.whiteLo / 100
	target := interior * (sp.whiteLo + g.r.intn(sp.whiteHi-sp.whiteLo+1)) / 100
	for tries := 0; tries < 300; tries++ {
		h, v := runLens(&white, n)
		long := false
		for _, c := range interiorCells(n) {
			if white[c] && max(h[c], v[c]) > uint8(sp.maxRun) {
				long = true
				break
			}
		}
		if !long && count <= target {
			b, err := newBoard(n, white[:n*n])
			if err != nil {
				return nil
			}
			return b
		}
		// Black out a cell picked with weight growing with its runs'
		// lengths, so long runs break up first; only cells on too-long runs
		// while any are left.
		var weights [maxCells]int
		total := 0
		for _, c := range interiorCells(n) {
			if !white[c] || long && max(h[c], v[c]) <= uint8(sp.maxRun) {
				continue
			}
			w := int(h[c]) + int(v[c])
			weights[c] = w * w
			total += w * w
		}
		if total == 0 {
			return nil
		}
		x := g.r.intn(total)
		c := 0
		for ; x >= weights[c]; c++ {
			x -= weights[c]
		}
		trial := white
		trial[c], trial[mirror(n, c)] = false, false
		fixSingles(&trial, n)
		k := 0
		for _, i := range interiorCells(n) {
			if trial[i] {
				k++
			}
		}
		if k < lo || !connected(&trial, n, k) || !linesUsed(&trial, n) {
			continue
		}
		white, count = trial, k
	}
	return nil
}

var interiorCache [maxN + 1][]int

func init() {
	for n := 3; n <= maxN; n++ {
		for r := 1; r < n; r++ {
			for c := 1; c < n; c++ {
				interiorCache[n] = append(interiorCache[n], r*n+c)
			}
		}
	}
}

// interiorCells lists the cells below the clue row and right of the clue
// column.
func interiorCells(n int) []int { return interiorCache[n] }

// mirror is the cell's partner under a half turn of the white area.
func mirror(n, c int) int { return (n-c/n)*n + n - c%n }

// runLens returns the length of the across and down run through each white
// cell.
func runLens(w *[maxCells]bool, n int) (h, v [maxCells]uint8) {
	for r := 1; r < n; r++ {
		for c := 1; c < n; {
			if !w[r*n+c] {
				c++
				continue
			}
			e := c
			for e < n && w[r*n+e] {
				e++
			}
			for x := c; x < e; x++ {
				h[r*n+x] = uint8(e - c)
			}
			c = e
		}
	}
	for c := 1; c < n; c++ {
		for r := 1; r < n; {
			if !w[r*n+c] {
				r++
				continue
			}
			e := r
			for e < n && w[e*n+c] {
				e++
			}
			for x := r; x < e; x++ {
				v[x*n+c] = uint8(e - r)
			}
			r = e
		}
	}
	return h, v
}

// fixSingles blacks out (with their mirrors) white cells without a white
// neighbour across or down, until none are left.
func fixSingles(w *[maxCells]bool, n int) {
	for changed := true; changed; {
		changed = false
		for _, i := range interiorCells(n) {
			if !w[i] {
				continue
			}
			c := i % n
			across := w[i-1] || c+1 < n && w[i+1]
			down := w[i-n] || i+n < n*n && w[i+n]
			if !across || !down {
				w[i], w[mirror(n, i)] = false, false
				changed = true
			}
		}
	}
}

// connected reports whether the k white cells form one region.
func connected(w *[maxCells]bool, n, k int) bool {
	var seen [maxCells]bool
	var queue [maxCells]int
	head, tail := 0, 0
	for _, i := range interiorCells(n) {
		if w[i] {
			queue[tail], seen[i] = i, true
			tail++
			break
		}
	}
	for head < tail {
		i := queue[head]
		head++
		for _, j := range [4]int{i - 1, i + 1, i - n, i + n} {
			if j >= 0 && j < n*n && w[j] && !seen[j] && (j/n == i/n || j%n == i%n) {
				seen[j] = true
				queue[tail] = j
				tail++
			}
		}
	}
	return tail == k
}

// linesUsed reports whether every row and column past the clues has a
// white cell.
func linesUsed(w *[maxCells]bool, n int) bool {
	for a := 1; a < n; a++ {
		row, col := false, false
		for b := 1; b < n; b++ {
			row = row || w[a*n+b]
			col = col || w[b*n+a]
		}
		if !row || !col {
			return false
		}
	}
	return true
}

// fill puts random digits in b's white cells, distinct within each run.
func (g *gen) fill(b *board) bool {
	g.sol = [maxCells]uint8{}
	var used [maxRuns]uint16
	nodes := 0
	return g.fillFrom(b, &used, b.nw, &nodes)
}

func (g *gen) fillFrom(b *board, used *[maxRuns]uint16, left int, nodes *int) bool {
	if left == 0 {
		return true
	}
	if *nodes++; *nodes > 20*b.nw {
		return false
	}
	best, bestN := -1, 10
	var bestM uint16
	for _, c := range b.whites[:b.nw] {
		if g.sol[c] != 0 {
			continue
		}
		ro := b.runOf[c]
		m := allDigits &^ (used[ro[0]] | used[ro[1]])
		if k := bits.OnesCount16(m); k < bestN {
			best, bestN, bestM = int(c), k, m
			if k == 0 {
				return false
			}
		}
	}
	ro := b.runOf[best]
	for m := bestM; m != 0; {
		d := g.pick(m)
		m &^= bit(d)
		g.sol[best] = d
		used[ro[0]] |= bit(d)
		used[ro[1]] |= bit(d)
		if g.fillFrom(b, used, left-1, nodes) {
			return true
		}
		used[ro[0]] &^= bit(d)
		used[ro[1]] &^= bit(d)
		g.sol[best] = 0
	}
	return false
}

// pick draws a digit from mask, weighted by the level's bias.
func (g *gen) pick(mask uint16) uint8 {
	total := 0
	for m := mask; m != 0; m &= m - 1 {
		total += g.sp.bias[bits.TrailingZeros16(m)]
	}
	x := g.r.intn(total)
	for m := mask; ; m &= m - 1 {
		d := bits.TrailingZeros16(m)
		if x -= g.sp.bias[d]; x < 0 {
			return uint8(d + 1)
		}
	}
}

// setSums derives every clue from the fill.
func (g *gen) setSums(b *board) {
	for i := 0; i < b.nr; i++ {
		rn := &b.runs[i]
		s := 0
		for _, c := range rn.cells[:rn.n] {
			s += int(g.sol[c])
		}
		rn.sum = uint8(s)
	}
}

// setCell changes one digit of the fill and the clues of its runs.
func (g *gen) setCell(b *board, c int, d uint8) {
	for _, r := range b.runOf[c] {
		b.runs[r].sum += d - g.sol[c]
	}
	g.sol[c] = d
}

// climb changes digits of the fill until its clues are solved by the
// techniques in set but not by those in below (if any), or it has used up
// the level's budget of solves.
//
// Each step changes a random stuck cell to a random digit and keeps the
// change unless more candidates are left stuck. Changes that make the
// puzzle too easy are never kept, so the climb keeps searching around the
// edge of solvability for a puzzle that needs the level's techniques. When
// patience steps in a row bring no progress, typically on a small knot of
// cells that can swap digits without changing any sum, it tries every digit
// in a sample of the stuck cells and takes the best change, even if that is
// a step back.
func (g *gen) climb(b *board, set, below techSet) bool {
	g.setSums(b)
	ok, score := g.eval(b, set, below)
	if ok {
		return true
	}
	var cells [maxCells]uint8
	nc := g.climbCells(&cells, score)
	if nc == 0 {
		return false
	}
	type change struct {
		c int
		d uint8
	}
	var best [sweepCells * 9]change
	stale, sweptAt := 0, -1
	for evals := 0; evals < g.sp.budget; {
		if stale >= patience || nc <= smallKnot && score != sweptAt {
			kick := stale >= patience
			stale = 0
			sample := cells
			k := min(nc, sweepCells)
			for i := 0; i < k; i++ {
				j := i + g.r.intn(nc-i)
				sample[i], sample[j] = sample[j], sample[i]
			}
			bestScore, nb := tooEasy+1, 0
			for _, x := range sample[:k] {
				c := int(x)
				old := g.sol[c]
				for m := g.free(b, c); m != 0; m &= m - 1 {
					g.setCell(b, c, lowDigit(m))
					evals++
					ok, s := g.eval(b, set, below)
					if ok {
						return true
					}
					if s < bestScore {
						bestScore, nb = s, 0
					}
					if s == bestScore {
						best[nb] = change{c, lowDigit(m)}
						nb++
					}
				}
				g.setCell(b, c, old)
			}
			if nb == 0 || bestScore >= score && !kick {
				sweptAt = score
				continue
			}
			ch := best[g.r.intn(nb)]
			g.setCell(b, ch.c, ch.d)
			_, score = g.eval(b, set, below)
			nc = g.climbCells(&cells, score)
			continue
		}
		c := int(cells[g.r.intn(nc)])
		free := g.free(b, c)
		if free == 0 {
			stale++
			continue
		}
		old := g.sol[c]
		g.setCell(b, c, g.pick(free))
		evals++
		ok, s := g.eval(b, set, below)
		if ok {
			return true
		}
		if s < score {
			stale = 0
		} else {
			stale++
		}
		if s <= score {
			score = s
			nc = g.climbCells(&cells, score)
		} else {
			g.setCell(b, c, old)
		}
	}
	return false
}

const (
	patience   = 40
	smallKnot  = 6
	sweepCells = 12
	tooEasy    = 1 << 20 // the score of a puzzle the techniques below the floor solve
)

// eval solves the fill's clues with the techniques in set and returns
// whether it is a puzzle for the level, or else how far from one it is.
func (g *gen) eval(b *board, set, below techSet) (bool, int) {
	if !g.sv.solves(b, set) {
		return false, g.sv.stuck()
	}
	if below != 0 && g.low.solves(b, below) {
		return false, tooEasy
	}
	return true, 0
}

// free returns the digits cell c could change to without repeating a digit
// in its runs.
func (g *gen) free(b *board, c int) uint16 {
	var used uint16
	for _, r := range b.runOf[c] {
		rn := &b.runs[r]
		for _, x := range rn.cells[:rn.n] {
			used |= bit(g.sol[x])
		}
	}
	return allDigits &^ used
}

// climbCells lists the cells the climb changes: the open cells the last
// solve left, or every white cell if the puzzle was too easy.
func (g *gen) climbCells(buf *[maxCells]uint8, score int) int {
	s := &g.sv.s
	n := 0
	for _, c := range s.b.whites[:s.b.nw] {
		if s.val[c] == 0 || score == tooEasy {
			buf[n] = c
			n++
		}
	}
	return n
}

// rng draws from PCG directly, with its own bounded-int, so puzzles for a
// seed don't change if the stdlib's helpers ever do.
type rng struct{ p *rand.PCG }

func newRNG(seed uint64, level Level) *rng {
	return &rng{rand.NewPCG(seed, 0x6b616b75726f^uint64(level))}
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
