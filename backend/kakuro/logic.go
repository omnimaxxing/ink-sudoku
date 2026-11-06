package kakuro

import "math/bits"

// move is a deduction in compact form; describe turns it into a Step.
// Finders fill it without allocating so grading stays cheap.
type move struct {
	tech   Technique
	place  int16  // cell to fill, or -1 for an elimination-only move
	digit  uint8  // placed digit
	digits uint16 // subset digits; for CombinationExclusion, the surviving combinations' union
	run    int16  // the run the reasoning is about (the across run of a crossing single), or -1
	other  int16  // the down run of a crossing single; the cell a TwoCellSum mirrors
	cells  [9]uint8
	nc     uint8
	elim   [9]uint16 // eliminations, by position in the run's cells
}

func (m *move) reset(t Technique) { *m = move{tech: t, place: -1, run: -1, other: -1} }
func (m *move) addCell(c int)     { m.cells[m.nc] = uint8(c); m.nc++ }

// techSet is a set of techniques, bit t for Technique t.
type techSet uint32

// upTo is the set of techniques up to and including t.
func upTo(t Technique) techSet { return 1<<(t+1) - 1 }

// finders is the technique ladder, easiest first, indexed by Technique.
var finders = [NumTechniques]func(*state, *move) bool{
	LastCell:             (*state).lastCell,
	UniqueCombination:    func(s *state, m *move) bool { return s.crossSingle(m, UniqueCombination) },
	CrossRun:             func(s *state, m *move) bool { return s.crossSingle(m, CrossRun) },
	RequiredDigit:        (*state).requiredDigit,
	NakedSingle:          (*state).nakedSingle,
	CombinationExclusion: (*state).combinationExclusion,
	TwoCellSum:           (*state).twoCellSum,
	NakedPair:            func(s *state, m *move) bool { return s.nakedSubset(m, 2, NakedPair) },
	HiddenPair:           func(s *state, m *move) bool { return s.hiddenSubset(m, 2, HiddenPair) },
	NakedTriple:          func(s *state, m *move) bool { return s.nakedSubset(m, 3, NakedTriple) },
	HiddenTriple:         func(s *state, m *move) bool { return s.hiddenSubset(m, 3, HiddenTriple) },
	SumArithmetic:        (*state).sumArithmetic,
}

// nextIn finds the easiest move using the techniques in set.
func (s *state) nextIn(m *move, set techSet) bool {
	for t := Technique(0); int(t) < NumTechniques; t++ {
		if s.broken {
			return false
		}
		if set&(1<<t) != 0 && finders[t](s, m) {
			return true
		}
	}
	return false
}

func (s *state) apply(m *move) {
	if m.place >= 0 {
		s.place(int(m.place), m.digit)
		return
	}
	rn := &s.b.runs[m.run]
	for i, e := range m.elim[:rn.n] {
		if e == 0 {
			continue
		}
		c := rn.cells[i]
		s.cand[c] &^= e
		s.touch(c)
		if s.cand[c] == 0 && s.val[c] == 0 {
			s.broken = true
		}
	}
}

// solver is scratch space for the logical solver. The finders are called
// through a table, so a state and move on the stack would escape to the
// heap on every solve; the generator reuses one solver instead.
type solver struct {
	s    state
	m    move
	memo memo
}

func (sv *solver) start(b *board) {
	sv.s.memo = &sv.memo
	sv.s.init(b, nil)
}

// singleSet is every technique that fills a cell from a single candidate.
const singleSet = 1<<LastCell | 1<<UniqueCombination | 1<<CrossRun | 1<<NakedSingle

// solves reports whether the techniques in set finish b, which is what
// rate's Solved says, only faster: it fills every single (and, if allowed,
// every required digit) a sweep finds before trying anything harder.
// Fills never block one another, so both reach the same position before
// anything harder is needed. A set without NakedSingle has no eliminations
// either (sets are prefixes of the ladder), so every single it meets comes
// from the baseline and is one of the crossing singles.
func (sv *solver) solves(b *board, set techSet) bool {
	s, m := &sv.s, &sv.m
	sv.start(b)
	harder := set &^ (singleSet | 1<<RequiredDigit)
	for s.empty > 0 && !s.broken {
		if s.placeForced(set&(1<<RequiredDigit) != 0) {
			continue
		}
		if s.broken || !s.nextIn(m, harder) {
			return false
		}
		s.apply(m)
	}
	return !s.broken
}

// placeForced fills every cell with one candidate and, if required is set,
// every required digit with one home. It reports whether it filled any.
func (s *state) placeForced(required bool) bool {
	placed := false
	for _, c := range s.b.whites[:s.b.nw] {
		if k := s.cand[c]; s.val[c] == 0 && single(k) {
			s.place(int(c), lowDigit(k))
			placed = true
		}
	}
	if required {
		for r := 0; r < s.b.nr && !s.broken; r++ {
			if s.open[r] < 2 || s.skip(RequiredDigit, r) {
				continue
			}
			if c, d := s.requiredHome(r); c >= 0 {
				s.place(c, d)
				placed = true
			} else {
				s.mark(RequiredDigit, r)
			}
		}
	}
	return placed
}

// stuck measures how far from solved the last solves left the board: the
// candidates beyond one per open cell.
func (sv *solver) stuck() int {
	s := &sv.s
	n := 0
	for _, c := range s.b.whites[:s.b.nw] {
		if s.val[c] == 0 {
			n += bits.OnesCount16(s.cand[c]) - 1
		}
	}
	return n
}

// rate runs the logical solver with the techniques in set, one easiest
// step at a time, and rates the solve.
func (sv *solver) rate(b *board, set techSet) Rating {
	var r Rating
	s, m := &sv.s, &sv.m
	sv.start(b)
	for s.empty > 0 {
		if !s.nextIn(m, set) {
			return r
		}
		s.apply(m)
		r.Steps++
		r.Counts[m.tech]++
		r.Hardest = max(r.Hardest, m.tech)
	}
	if s.broken {
		return r
	}
	r.Solved = true
	r.Level = levelOf(r.Hardest)
	return r
}

func (s *state) lastCell(m *move) bool {
	for r := 0; r < s.b.nr; r++ {
		if s.open[r] != 1 {
			continue
		}
		rn := &s.b.runs[r]
		for _, c := range rn.cells[:rn.n] {
			if s.val[c] != 0 {
				continue
			}
			k := s.cand[c]
			if k == 0 {
				s.broken = true
				return false
			}
			if single(k) {
				m.reset(LastCell)
				m.place, m.digit, m.run = int16(c), lowDigit(k), int16(r)
				return true
			}
		}
	}
	return false
}

// crossSingle finds a cell whose runs' combinations leave one digit:
// UniqueCombination when each run has a single combination, CrossRun
// otherwise. A single that notes or eliminations made is a NakedSingle.
func (s *state) crossSingle(m *move, t Technique) bool {
	for _, c := range s.b.whites[:s.b.nw] {
		k := s.cand[c]
		if s.val[c] != 0 || !single(k) {
			continue
		}
		a, d := int(s.b.runOf[c][0]), int(s.b.runOf[c][1])
		ua, _, na := s.arith(a)
		ud, _, nd := s.arith(d)
		if ua&ud != k || (na == 1 && nd == 1) != (t == UniqueCombination) {
			continue
		}
		m.reset(t)
		m.place, m.digit, m.run, m.other = int16(c), lowDigit(k), int16(a), int16(d)
		return true
	}
	return false
}

func (s *state) requiredDigit(m *move) bool {
	for r := 0; r < s.b.nr; r++ {
		if s.open[r] < 2 || s.skip(RequiredDigit, r) {
			continue
		}
		if c, d := s.requiredHome(r); c >= 0 {
			m.reset(RequiredDigit)
			m.place, m.digit, m.run = int16(c), d, int16(r)
			return true
		}
		if s.broken {
			return false
		}
		s.mark(RequiredDigit, r)
	}
	return false
}

func (s *state) nakedSingle(m *move) bool {
	for _, c := range s.b.whites[:s.b.nw] {
		if k := s.cand[c]; s.val[c] == 0 && single(k) {
			m.reset(NakedSingle)
			m.place, m.digit = int16(c), lowDigit(k)
			return true
		}
	}
	return false
}

func (s *state) combinationExclusion(m *move) bool {
	var all, fit [maxCombos]uint16
	for r := 0; r < s.b.nr; r++ {
		if s.open[r] < 2 || s.skip(CombinationExclusion, r) {
			continue
		}
		na, nf := s.fitCombos(r, &all, &fit)
		if nf == na {
			s.mark(CombinationExclusion, r)
			continue
		}
		if nf == 0 {
			s.broken = true
			return false
		}
		var union uint16
		inter := allDigits
		for _, d := range fit[:nf] {
			union |= d
			inter &= d
		}
		rn := &s.b.runs[r]
		found := false
		for i, c := range rn.cells[:rn.n] {
			if e := s.cand[c] &^ union; s.val[c] == 0 && e != 0 {
				if !found {
					m.reset(CombinationExclusion)
					found = true
				}
				m.elim[i] = e
			}
		}
		if found {
			m.run, m.digits = int16(r), union
			return true
		}
		_, before, _ := s.arith(r)
		for x := inter &^ before; x != 0; x &= x - 1 {
			b := x & -x
			home, count := -1, 0
			for _, c := range rn.cells[:rn.n] {
				if s.val[c] == 0 && s.cand[c]&b != 0 {
					home = int(c)
					count++
				}
			}
			if count == 1 {
				m.reset(CombinationExclusion)
				m.place, m.digit, m.run, m.digits = int16(home), lowDigit(b), int16(r), union
				return true
			}
		}
		s.mark(CombinationExclusion, r)
	}
	return false
}

func (s *state) twoCellSum(m *move) bool {
	for r := 0; r < s.b.nr; r++ {
		if s.open[r] != 2 || s.skip(TwoCellSum, r) {
			continue
		}
		rn := &s.b.runs[r]
		var pos [2]int
		k := 0
		for i, c := range rn.cells[:rn.n] {
			if s.val[c] == 0 {
				pos[k] = i
				k++
			}
		}
		sum := int(s.rem[r])
		for j := 0; j < 2; j++ {
			x, y := rn.cells[pos[j]], rn.cells[pos[1-j]]
			var ok uint16
			for kx := s.cand[x]; kx != 0; kx &= kx - 1 {
				d := bits.TrailingZeros16(kx) + 1
				if e := sum - d; e >= 1 && e <= 9 && e != d && s.cand[y]&bit(uint8(e)) != 0 {
					ok |= bit(uint8(d))
				}
			}
			if e := s.cand[x] &^ ok; e != 0 {
				m.reset(TwoCellSum)
				m.run, m.other = int16(r), int16(y)
				m.elim[pos[j]] = e
				return true
			}
		}
		s.mark(TwoCellSum, r)
	}
	return false
}

// runPositions lists the positions (in run r's cells) of its open cells.
func (s *state) runPositions(r int, buf *[9]uint8) []uint8 {
	rn := &s.b.runs[r]
	out := buf[:0]
	for i, c := range rn.cells[:rn.n] {
		if s.val[c] == 0 {
			out = append(out, uint8(i))
		}
	}
	return out
}

// nextCombo advances idx, an ascending choice of len(idx) indices below k,
// to the next one in lexicographic order.
func nextCombo(idx []int, k int) bool {
	n := len(idx)
	for i := n - 1; i >= 0; i-- {
		if idx[i] < k-n+i {
			idx[i]++
			for j := i + 1; j < n; j++ {
				idx[j] = idx[j-1] + 1
			}
			return true
		}
	}
	return false
}

func (s *state) nakedSubset(m *move, n int, t Technique) bool {
	var buf [9]uint8
	for r := 0; r < s.b.nr; r++ {
		if int(s.open[r]) <= n || s.skip(t, r) {
			continue
		}
		rn := &s.b.runs[r]
		pos := s.runPositions(r, &buf)
		var small [9]uint8 // positions with 2..n candidates
		k := 0
		for _, p := range pos {
			if pc := bits.OnesCount16(s.cand[rn.cells[p]]); pc >= 2 && pc <= n {
				small[k] = p
				k++
			}
		}
		if k < n {
			s.mark(t, r)
			continue
		}
		idx := [3]int{0, 1, 2}
		for {
			var union uint16
			var in uint16 // positions in the subset
			for _, i := range idx[:n] {
				union |= s.cand[rn.cells[small[i]]]
				in |= 1 << small[i]
			}
			if bits.OnesCount16(union) == n {
				found := false
				for _, p := range pos {
					if e := s.cand[rn.cells[p]] & union; in&(1<<p) == 0 && e != 0 {
						if !found {
							m.reset(t)
							found = true
						}
						m.elim[p] = e
					}
				}
				if found {
					m.run, m.digits = int16(r), union
					for _, i := range idx[:n] {
						m.addCell(int(rn.cells[small[i]]))
					}
					return true
				}
			}
			if !nextCombo(idx[:n], k) {
				break
			}
		}
		s.mark(t, r)
	}
	return false
}

func (s *state) hiddenSubset(m *move, n int, t Technique) bool {
	var buf [9]uint8
	var all, fit [maxCombos]uint16
	for r := 0; r < s.b.nr; r++ {
		if int(s.open[r]) <= n || s.skip(t, r) {
			continue
		}
		rn := &s.b.runs[r]
		_, nf := s.fitCombos(r, &all, &fit)
		if nf == 0 {
			s.broken = true
			return false
		}
		req := allDigits
		for _, d := range fit[:nf] {
			req &= d
		}
		pos := s.runPositions(r, &buf)
		var where [9]uint16 // per digit: positions that can take it
		for _, p := range pos {
			for k := s.cand[rn.cells[p]]; k != 0; k &= k - 1 {
				where[bits.TrailingZeros16(k)] |= 1 << p
			}
		}
		var ds [9]uint8
		k := 0
		for x := req; x != 0; x &= x - 1 {
			d := bits.TrailingZeros16(x)
			if pc := bits.OnesCount16(where[d]); pc >= 2 && pc <= n {
				ds[k] = uint8(d)
				k++
			}
		}
		if k < n {
			s.mark(t, r)
			continue
		}
		idx := [3]int{0, 1, 2}
		for {
			var digits, at uint16
			for _, i := range idx[:n] {
				digits |= 1 << ds[i]
				at |= where[ds[i]]
			}
			if bits.OnesCount16(at) == n {
				found := false
				for x := at; x != 0; x &= x - 1 {
					p := bits.TrailingZeros16(x)
					if e := s.cand[rn.cells[p]] &^ digits; e != 0 {
						if !found {
							m.reset(t)
							found = true
						}
						m.elim[p] = e
					}
				}
				if found {
					m.run, m.digits = int16(r), digits
					for x := at; x != 0; x &= x - 1 {
						m.addCell(int(rn.cells[bits.TrailingZeros16(x)]))
					}
					return true
				}
			}
			if !nextCombo(idx[:n], k) {
				break
			}
		}
		s.mark(t, r)
	}
	return false
}

// maskSet is a set of digit masks, bit m of 512.
type maskSet [8]uint64

func (ms *maskSet) add(m uint16)      { ms[m>>6] |= 1 << (m & 63) }
func (ms *maskSet) has(m uint16) bool { return ms[m>>6]&(1<<(m&63)) != 0 }

// support returns, for each open cell of run r (by position), the
// candidates that some way of filling all its open cells with different
// candidates adding up to the remaining sum uses. ok is false if there is
// no such way.
func (s *state) support(r int, pos []uint8) (sup [9]uint16, ok bool) {
	rn := &s.b.runs[r]
	k := len(pos)
	// reach[i]: digit sets the first i open cells can hold.
	var reach [10]maskSet
	reach[0].add(0)
	for i, p := range pos {
		cand := s.cand[rn.cells[p]]
		for w, word := range reach[i] {
			for x := word; x != 0; x &= x - 1 {
				m := uint16(w<<6 | bits.TrailingZeros64(x))
				for d := cand &^ m; d != 0; d &= d - 1 {
					reach[i+1].add(m | d&-d)
				}
			}
		}
	}
	// done[i]: sets in reach[i] the remaining open cells can complete.
	var done [10]maskSet
	for _, d := range combos[k][max(int(s.rem[r]), 0)] {
		if int(s.rem[r]) >= 0 && d&s.used[r] == 0 && reach[k].has(d) {
			done[k].add(d)
			ok = true
		}
	}
	if !ok {
		return sup, false
	}
	for i := k - 1; i >= 0; i-- {
		cand := s.cand[rn.cells[pos[i]]]
		for w, word := range reach[i] {
			for x := word; x != 0; x &= x - 1 {
				m := uint16(w<<6 | bits.TrailingZeros64(x))
				for d := cand &^ m; d != 0; d &= d - 1 {
					if b := d & -d; done[i+1].has(m | b) {
						done[i].add(m)
						sup[i] |= b
					}
				}
			}
		}
	}
	return sup, true
}

func (s *state) sumArithmetic(m *move) bool {
	var buf [9]uint8
	for r := 0; r < s.b.nr; r++ {
		if s.open[r] < 3 || s.skip(SumArithmetic, r) {
			continue
		}
		rn := &s.b.runs[r]
		pos := s.runPositions(r, &buf)
		sup, ok := s.support(r, pos)
		if !ok {
			s.broken = true
			return false
		}
		for i, p := range pos {
			if e := s.cand[rn.cells[p]] &^ sup[i]; e != 0 {
				m.reset(SumArithmetic)
				m.run = int16(r)
				m.elim[p] = e
				return true
			}
		}
		s.mark(SumArithmetic, r)
	}
	return false
}
