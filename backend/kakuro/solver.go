package kakuro

import "math/bits"

// CountSolutions counts the solutions of p's clues (p.Solution is ignored),
// stopping once limit is reached; CountSolutions(p, 2) == 1 is the
// uniqueness check. A malformed puzzle has none.
func CountSolutions(p Puzzle, limit int) int {
	b, err := puzzleBoard(p)
	if err != nil || limit <= 0 {
		return 0
	}
	n, _ := countSolutions(b, limit)
	return n
}

// Solve returns a solution of p's clues, the first found if there are
// several, and false if there is none.
func Solve(p Puzzle) ([]uint8, bool) {
	b, err := puzzleBoard(p)
	if err != nil {
		return nil, false
	}
	n, sol := countSolutions(b, 1)
	if n == 0 {
		return nil, false
	}
	return sol[:b.n*b.n], true
}

func countSolutions(b *board, limit int) (int, [maxCells]uint8) {
	x := searcher{limit: limit}
	var s state
	s.init(b, nil)
	x.search(&s)
	return x.count, x.first
}

// searcher is a depth-first solver. Each node fills every forced cell (one
// candidate left, or the only home of a digit all of a run's combinations
// need), then branches on the open cell with the fewest candidates.
type searcher struct {
	limit, count int
	first        [maxCells]uint8
}

func (x *searcher) search(s *state) {
	if !s.propagate() {
		return
	}
	if s.empty == 0 {
		x.count++
		if x.count == 1 {
			x.first = s.val
		}
		return
	}
	best, bestN := -1, 10
	for _, c := range s.b.whites[:s.b.nw] {
		if s.val[c] != 0 {
			continue
		}
		if n := bits.OnesCount16(s.cand[c]); n < bestN {
			best, bestN = int(c), n
			if n == 2 {
				break
			}
		}
	}
	var child state
	for m := s.cand[best]; m != 0; m &= m - 1 {
		child = *s
		child.place(best, lowDigit(m))
		x.search(&child)
		if x.count >= x.limit {
			return
		}
	}
}

// propagate fills forced cells until none are left and reports whether the
// board is still consistent.
func (s *state) propagate() bool {
	for !s.broken {
		progress := false
		for _, c := range s.b.whites[:s.b.nw] {
			if s.val[c] != 0 {
				continue
			}
			k := s.cand[c]
			if k == 0 {
				s.broken = true
				return false
			}
			if single(k) {
				s.place(int(c), lowDigit(k))
				progress = true
			}
		}
		for r := 0; r < s.b.nr && !s.broken; r++ {
			if s.open[r] < 2 {
				continue
			}
			if c, d := s.requiredHome(r); c >= 0 {
				s.place(c, d)
				progress = true
			}
		}
		if !progress {
			break
		}
	}
	return !s.broken
}

// requiredHome finds a digit every remaining combination of run r needs
// that only one open cell can take, and returns that cell. It marks the
// state broken if such a digit has no cell at all.
func (s *state) requiredHome(r int) (int, uint8) {
	_, inter, n := s.arith(r)
	if n == 0 {
		s.broken = true
		return -1, 0
	}
	rn := &s.b.runs[r]
	for x := inter; x != 0; x &= x - 1 {
		b := x & -x
		home, count := -1, 0
		for _, c := range rn.cells[:rn.n] {
			if s.val[c] == 0 && s.cand[c]&b != 0 {
				home = int(c)
				count++
			}
		}
		if count == 0 {
			s.broken = true
			return -1, 0
		}
		if count == 1 {
			return home, lowDigit(b)
		}
	}
	return -1, 0
}
