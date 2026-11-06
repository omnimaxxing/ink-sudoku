package engine

import "math/bits"

// Solve returns a solution of g (the first one found if there are several).
// It reports false if g has conflicting digits or no solution.
func Solve(g Grid) (Grid, bool) {
	s, ok := newBacktracker(g)
	if !ok {
		return Grid{}, false
	}
	s.limit = 1
	s.search()
	return s.first, s.count > 0
}

// CountSolutions counts the solutions of g, stopping once limit is reached.
// CountSolutions(g, 2) == 1 is the uniqueness check.
func CountSolutions(g Grid, limit int) int {
	s, ok := newBacktracker(g)
	if !ok || limit <= 0 {
		return 0
	}
	s.limit = limit
	s.search()
	return s.count
}

// backtracker is a bitmask depth-first solver that always branches on the
// empty cell with the fewest candidates (MRV).
type backtracker struct {
	g     Grid
	used  [27]uint16 // digits present per house
	limit int
	count int
	first Grid
	rnd   *rng // when set, candidates are tried in random order
}

func newBacktracker(g Grid) (*backtracker, bool) {
	if !g.Valid() {
		return nil, false
	}
	s := &backtracker{g: g}
	for c, v := range g {
		if v != 0 {
			s.toggle(c, v)
		}
	}
	return s, true
}

func (s *backtracker) toggle(c int, d uint8) {
	b := CandidateBit(d)
	s.used[cellRow[c]] ^= b
	s.used[9+cellCol[c]] ^= b
	s.used[18+cellBox[c]] ^= b
}

func (s *backtracker) free(c int) uint16 {
	return allDigits &^ (s.used[cellRow[c]] | s.used[9+cellCol[c]] | s.used[18+cellBox[c]])
}

func (s *backtracker) search() {
	best, bestN := -1, 10
	var bestM uint16
	var free [81]uint16
	for c := 0; c < 81; c++ {
		if s.g[c] != 0 {
			continue
		}
		m := s.free(c)
		free[c] = m
		if n := bits.OnesCount16(m); n < bestN {
			best, bestN, bestM = c, n, m
			if n <= 1 {
				break
			}
		}
	}
	if best < 0 {
		s.count++
		if s.count == 1 {
			s.first = s.g
		}
		return
	}
	if bestN == 0 {
		return
	}
	if bestN > 1 {
		// A digit with one place left in a house is forced; one with none
		// left is a dead end. Either beats branching on the MRV cell.
		for h := 0; h < 27 && bestN > 1; h++ {
			var once, twice uint16
			for _, c := range houseCells[h] {
				twice |= once & free[c]
				once |= free[c]
			}
			need := allDigits &^ s.used[h]
			if need&^once != 0 {
				return
			}
			if x := need & once &^ twice; x != 0 {
				b := x & -x
				for _, c := range houseCells[h] {
					if free[c]&b != 0 {
						best, bestN, bestM = int(c), 1, b
						break
					}
				}
			}
		}
	}

	var order [9]uint8
	n := 0
	for m := bestM; m != 0; m &= m - 1 {
		order[n] = uint8(bits.TrailingZeros16(m)) + 1
		n++
	}
	if s.rnd != nil {
		s.rnd.shuffle(order[:n])
	}
	for _, d := range order[:n] {
		s.g[best] = d
		s.toggle(best, d)
		s.search()
		s.toggle(best, d)
		s.g[best] = 0
		if s.count >= s.limit {
			return
		}
	}
}

// randomSolution returns a random complete grid drawn from r.
func randomSolution(r *rng) Grid {
	s := &backtracker{limit: 1, rnd: r}
	s.search()
	return s.first
}
