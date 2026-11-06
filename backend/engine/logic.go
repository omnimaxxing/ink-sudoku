package engine

import "math/bits"

// state is a board plus pencil marks, as seen by the human-style solver.
type state struct {
	val    Grid
	cand   [81]uint16
	empty  int
	broken bool // an empty cell or a missing digit has nowhere left to go
}

func newState(g Grid) (state, bool) {
	if !g.Valid() {
		return state{}, false
	}
	s := state{val: g, cand: AutoCandidates(g)}
	for c, v := range g {
		if v == 0 {
			s.empty++
			if s.cand[c] == 0 {
				s.broken = true
			}
		}
	}
	return s, true
}

func (s *state) place(c int, d uint8) {
	b := CandidateBit(d)
	s.val[c] = d
	s.cand[c] = 0
	s.empty--
	for _, p := range peers[c] {
		if s.cand[p]&b != 0 {
			s.cand[p] &^= b
			if s.cand[p] == 0 {
				s.broken = true
			}
		}
	}
}

func (s *state) apply(m *move) {
	if m.place >= 0 {
		s.place(int(m.place), m.digit)
		return
	}
	for c, e := range m.elim {
		if e != 0 {
			s.cand[c] &^= e
			if s.cand[c] == 0 && s.val[c] == 0 {
				s.broken = true
			}
		}
	}
}

// move is a deduction in compact form; describe turns it into a Step.
// Finders fill it without allocating so grading stays cheap.
type move struct {
	tech   Technique
	place  int8   // cell to fill, or -1 for an elimination-only move
	digit  uint8  // placed digit; the digit of a locked set, fish or chain; z of a wing
	digits uint16 // subset digits; the candidates of a wing's pivot or pair
	houses [6]uint8
	nh     uint8
	// cells are the pattern cells. A wing's pivot comes first; a W-wing's
	// pair comes before its link cells; skyscrapers, kites and colouring
	// chains list their cells in chain order, end to end.
	cells [9]uint8
	nc    uint8
	elim  [81]uint16
}

func (m *move) reset(t Technique) { *m = move{tech: t, place: -1} }
func (m *move) addHouse(h int)    { m.houses[m.nh] = uint8(h); m.nh++ }
func (m *move) addCell(c int)     { m.cells[m.nc] = uint8(c); m.nc++ }

// finders is the technique ladder, easiest first, indexed by Technique.
var finders = [NumTechniques]func(*state, *move) bool{
	HiddenSingle:    (*state).hiddenSingle,
	NakedSingle:     (*state).nakedSingle,
	Pointing:        (*state).pointing,
	Claiming:        (*state).claiming,
	NakedPair:       func(s *state, m *move) bool { return s.nakedSubset(m, 2, NakedPair) },
	HiddenPair:      func(s *state, m *move) bool { return s.hiddenSubset(m, 2, HiddenPair) },
	NakedTriple:     func(s *state, m *move) bool { return s.nakedSubset(m, 3, NakedTriple) },
	HiddenTriple:    func(s *state, m *move) bool { return s.hiddenSubset(m, 3, HiddenTriple) },
	NakedQuad:       func(s *state, m *move) bool { return s.nakedSubset(m, 4, NakedQuad) },
	HiddenQuad:      func(s *state, m *move) bool { return s.hiddenSubset(m, 4, HiddenQuad) },
	XWing:           func(s *state, m *move) bool { return s.fish(m, 2, XWing) },
	Swordfish:       func(s *state, m *move) bool { return s.fish(m, 3, Swordfish) },
	SimpleColouring: (*state).simpleColouring,
	Skyscraper:      (*state).skyscraper,
	TwoStringKite:   (*state).twoStringKite,
	XYWing:          (*state).xyWing,
	WWing:           (*state).wWing,
	XYZWing:         (*state).xyzWing,
}

// next finds the easiest move using techniques up to ceiling.
func (s *state) next(m *move, ceiling Technique) bool {
	return s.nextIn(m, upTo(ceiling))
}

// nextIn finds the easiest move using the techniques in set.
func (s *state) nextIn(m *move, set techSet) bool {
	for t := HiddenSingle; int(t) < NumTechniques; t++ {
		if s.broken {
			return false
		}
		if set&(1<<t) != 0 && finders[t](s, m) {
			return true
		}
	}
	return false
}

// techSet is a set of techniques, bit t for Technique t.
type techSet uint32

// upTo is the set of techniques up to and including t.
func upTo(t Technique) techSet { return 1<<(t+1) - 1 }

// Hidden singles are searched box-first: cross-hatching a box is the
// first thing most people learn.
var hiddenOrder = [27]uint8{
	18, 19, 20, 21, 22, 23, 24, 25, 26,
	0, 1, 2, 3, 4, 5, 6, 7, 8,
	9, 10, 11, 12, 13, 14, 15, 16, 17,
}

// houseCounts returns the digits with at least one and at least two
// candidates in house h, plus the digits already placed there.
func (s *state) houseCounts(h int) (once, twice, placed uint16) {
	for _, c := range houseCells[h] {
		k := s.cand[c]
		twice |= once & k
		once |= k
		if v := s.val[c]; v != 0 {
			placed |= CandidateBit(v)
		}
	}
	return
}

func (s *state) hiddenSingle(m *move) bool {
	for _, h := range hiddenOrder {
		once, twice, placed := s.houseCounts(int(h))
		if once|placed != allDigits {
			s.broken = true
			return false
		}
		x := once &^ twice
		if x == 0 {
			continue
		}
		b := x & -x
		for _, c := range houseCells[h] {
			if s.cand[c]&b != 0 {
				m.reset(HiddenSingle)
				m.place, m.digit = int8(c), lowDigit(b)
				m.addHouse(int(h))
				m.addCell(int(c))
				return true
			}
		}
	}
	return false
}

func (s *state) nakedSingle(m *move) bool {
	for c, k := range s.cand {
		if k != 0 && k&(k-1) == 0 {
			m.reset(NakedSingle)
			m.place, m.digit = int8(c), lowDigit(k)
			m.addCell(c)
			return true
		}
	}
	return false
}

// placeSingles places every naked and hidden single it comes across in one
// sweep and reports whether it placed any. It marks the state broken if a
// digit has nowhere to go.
func (s *state) placeSingles() bool {
	placed := false
	for c := range s.cand {
		// Read the cell now: earlier placements in this sweep change it.
		if k := s.cand[c]; k != 0 && k&(k-1) == 0 {
			s.place(c, lowDigit(k))
			placed = true
		}
	}
	for h := 0; h < 27 && !s.broken; h++ {
		once, twice, have := s.houseCounts(h)
		if once|have != allDigits {
			s.broken = true
			break
		}
		for x := once &^ twice; x != 0; x &= x - 1 {
			b := x & -x
			c := -1
			for _, cc := range houseCells[h] {
				if s.cand[cc]&b != 0 {
					c = int(cc)
					break
				}
			}
			if c < 0 {
				s.broken = true // its only spot went to another digit
				break
			}
			s.place(c, lowDigit(b))
			placed = true
		}
	}
	return placed
}

// singlesAvailable counts the distinct cells that a naked or hidden single
// could fill right now.
func (s *state) singlesAvailable() int {
	var set cellSet
	for c, k := range s.cand {
		if k != 0 && k&(k-1) == 0 {
			set.add(c)
		}
	}
	for h := 0; h < 27; h++ {
		once, twice, _ := s.houseCounts(h)
		for x := once &^ twice; x != 0; x &= x - 1 {
			b := x & -x
			for _, c := range houseCells[h] {
				if s.cand[c]&b != 0 {
					set.add(int(c))
					break
				}
			}
		}
	}
	return set.count()
}

func (s *state) pointing(m *move) bool {
	for b := 0; b < 9; b++ {
		h := 18 + b
		var rows, cols [9]uint16 // per digit
		for _, c := range houseCells[h] {
			for k := s.cand[c]; k != 0; k &= k - 1 {
				d := bits.TrailingZeros16(k)
				rows[d] |= 1 << cellRow[c]
				cols[d] |= 1 << cellCol[c]
			}
		}
		for d := uint8(1); d <= 9; d++ {
			r, c := rows[d-1], cols[d-1]
			if r == 0 || r&(r-1) != 0 && c&(c-1) != 0 {
				continue
			}
			in := s.cellsWith(h, CandidateBit(d))
			if r&(r-1) == 0 && s.locked(m, Pointing, d, h, bits.TrailingZeros16(r), in) {
				return true
			}
			if c&(c-1) == 0 && s.locked(m, Pointing, d, h, 9+bits.TrailingZeros16(c), in) {
				return true
			}
		}
	}
	return false
}

func (s *state) claiming(m *move) bool {
	for line := 0; line < 18; line++ {
		var boxes [9]uint16 // per digit
		for _, c := range houseCells[line] {
			for k := s.cand[c]; k != 0; k &= k - 1 {
				boxes[bits.TrailingZeros16(k)] |= 1 << cellBox[c]
			}
		}
		for d := uint8(1); d <= 9; d++ {
			if b := boxes[d-1]; b != 0 && b&(b-1) == 0 &&
				s.locked(m, Claiming, d, line, 18+bits.TrailingZeros16(b), s.cellsWith(line, CandidateBit(d))) {
				return true
			}
		}
	}
	return false
}

// cellsWith returns the cells of house h that have bit.
func (s *state) cellsWith(h int, bit uint16) cellSet {
	var in cellSet
	for _, c := range houseCells[h] {
		if s.cand[c]&bit != 0 {
			in.add(int(c))
		}
	}
	return in
}

// locked handles both kinds of locked candidates: every d in house from
// lies in the cells in, which are all inside house to, so d goes from the
// rest of to.
func (s *state) locked(m *move, t Technique, d uint8, from, to int, in cellSet) bool {
	bit := CandidateBit(d)
	found := false
	for _, c := range houseCells[to] {
		if s.cand[c]&bit != 0 && !in.has(int(c)) {
			if !found {
				m.reset(t)
				found = true
			}
			m.elim[c] = bit
		}
	}
	if !found {
		return false
	}
	m.digit = d
	m.addHouse(from)
	m.addHouse(to)
	in.each(m.addCell)
	return true
}

func (s *state) nakedSubset(m *move, n int, t Technique) bool {
	for h := 0; h < 27; h++ {
		var idx [9]uint8
		var masks [9]uint16
		k, open := 0, 0
		for _, c := range houseCells[h] {
			pc := bits.OnesCount16(s.cand[c])
			if pc > 0 {
				open++
			}
			if pc >= 2 && pc <= n {
				idx[k], masks[k] = c, s.cand[c]
				k++
			}
		}
		if open <= n {
			continue // nothing left over to cross off
		}
		found := subsets(masks[:k], n, func(sel, union uint16) bool {
			if bits.OnesCount16(union) != n {
				return false
			}
			var in cellSet
			for x := sel; x != 0; x &= x - 1 {
				i := bits.TrailingZeros16(x)
				in.add(int(idx[i]))
			}
			any := false
			for _, c := range houseCells[h] {
				if e := s.cand[c] & union; e != 0 && !in.has(int(c)) {
					if !any {
						m.reset(t)
						any = true
					}
					m.elim[c] = e
				}
			}
			if !any {
				return false
			}
			m.digits = union
			m.addHouse(h)
			for x := sel; x != 0; x &= x - 1 {
				i := bits.TrailingZeros16(x)
				m.addCell(int(idx[i]))
			}
			return true
		})
		if found {
			return true
		}
	}
	return false
}

func (s *state) hiddenSubset(m *move, n int, t Technique) bool {
	for h := 0; h < 27; h++ {
		var pos [9]uint16 // per digit: positions within the house
		open := 0
		for i, c := range houseCells[h] {
			if s.cand[c] != 0 {
				open++
			}
			for k := s.cand[c]; k != 0; k &= k - 1 {
				pos[bits.TrailingZeros16(k)] |= 1 << i
			}
		}
		if open <= n {
			continue // the subset would fill the house: nothing else to cross off
		}
		var ds [9]uint8
		var masks [9]uint16
		k := 0
		for d := 0; d < 9; d++ {
			if pc := bits.OnesCount16(pos[d]); pc >= 2 && pc <= n {
				ds[k], masks[k] = uint8(d), pos[d]
				k++
			}
		}
		found := subsets(masks[:k], n, func(sel, where uint16) bool {
			var digits uint16
			for x := sel; x != 0; x &= x - 1 {
				i := bits.TrailingZeros16(x)
				digits |= 1 << ds[i]
			}
			if bits.OnesCount16(where) != n {
				return false
			}
			any := false
			for i, c := range houseCells[h] {
				if where&(1<<i) == 0 {
					continue
				}
				if e := s.cand[c] &^ digits; e != 0 {
					if !any {
						m.reset(t)
						any = true
					}
					m.elim[c] = e
				}
			}
			if !any {
				return false
			}
			m.digits = digits
			m.addHouse(h)
			for i, c := range houseCells[h] {
				if where&(1<<i) != 0 {
					m.addCell(int(c))
				}
			}
			return true
		})
		if found {
			return true
		}
	}
	return false
}

// fish finds X-wings (n=2) and swordfish (n=3): n base lines whose
// candidates for d all fall in n cross lines, so d goes from the rest of
// those cross lines. Rows are tried as base lines before columns.
func (s *state) fish(m *move, n int, t Technique) bool {
	spots := s.lineSpots()
	for d := uint8(1); d <= 9; d++ {
		bit := CandidateBit(d)
		for orient := 0; orient < 2; orient++ {
			var lines [9]int
			var masks [9]uint16
			k := 0
			for i := 0; i < 9; i++ {
				mask := spots[orient][d-1][i]
				if pc := bits.OnesCount16(mask); pc >= 2 && pc <= n {
					lines[k], masks[k] = i, mask
					k++
				}
			}
			found := subsets(masks[:k], n, func(sel, cover uint16) bool {
				var base uint16
				for x := sel; x != 0; x &= x - 1 {
					base |= 1 << lines[bits.TrailingZeros16(x)]
				}
				if bits.OnesCount16(cover) != n {
					return false
				}
				any := false
				for x := cover; x != 0; x &= x - 1 {
					ch := (1-orient)*9 + bits.TrailingZeros16(x)
					// Within a cross line, position j is base-line index j.
					for j, c := range houseCells[ch] {
						if base&(1<<j) == 0 && s.cand[c]&bit != 0 {
							if !any {
								m.reset(t)
								any = true
							}
							m.elim[c] = bit
						}
					}
				}
				if !any {
					return false
				}
				m.digit = d
				for x := sel; x != 0; x &= x - 1 {
					i := bits.TrailingZeros16(x)
					m.addHouse(orient*9 + lines[i])
				}
				for x := cover; x != 0; x &= x - 1 {
					m.addHouse((1-orient)*9 + bits.TrailingZeros16(x))
				}
				for x := sel; x != 0; x &= x - 1 {
					i := bits.TrailingZeros16(x)
					for j, c := range houseCells[orient*9+lines[i]] {
						if masks[i]&(1<<j) != 0 {
							m.addCell(int(c))
						}
					}
				}
				return true
			})
			if found {
				return true
			}
		}
	}
	return false
}

// xyWing finds a pivot {x,y} that sees pincers {x,z} and {y,z}. One pincer
// must be z, so z goes from every cell that sees both pincers.
func (s *state) xyWing(m *move) bool {
	for p, pk := range s.cand {
		if bits.OnesCount16(pk) != 2 {
			continue
		}
		for i, a := range peers[p] {
			ak := s.cand[a]
			if bits.OnesCount16(ak) != 2 || bits.OnesCount16(ak&pk) != 1 {
				continue
			}
			z := ak &^ pk
			want := pk&^ak | z
			for _, b := range peers[p][i+1:] {
				if s.cand[b] != want {
					continue
				}
				any := false
				peerSet[a].and(peerSet[b]).each(func(c int) {
					if s.cand[c]&z != 0 {
						if !any {
							m.reset(XYWing)
							any = true
						}
						m.elim[c] = z
					}
				})
				if !any {
					continue
				}
				m.digit = lowDigit(z)
				m.digits = pk | z
				m.addCell(p)
				m.addCell(int(a))
				m.addCell(int(b))
				return true
			}
		}
	}
	return false
}

// lineSpots returns, per orientation (rows, then columns), digit and line,
// the positions within the line whose cells have that digit.
func (s *state) lineSpots() (spots [2][9][9]uint16) {
	for c, k := range s.cand {
		r, col := cellRow[c], cellCol[c]
		for ; k != 0; k &= k - 1 {
			d := bits.TrailingZeros16(k)
			spots[0][d][r] |= 1 << col
			spots[1][d][col] |= 1 << r
		}
	}
	return
}

// lineMask returns the positions within house h whose cells have bit.
func (s *state) lineMask(h int, bit uint16) uint16 {
	var mask uint16
	for j, c := range houseCells[h] {
		if s.cand[c]&bit != 0 {
			mask |= 1 << j
		}
	}
	return mask
}

// seeBoth starts a move of technique t that crosses bit off every cell
// outside skip that sees both a and b. It reports false, leaving m alone,
// if there is nothing to cross off.
func (s *state) seeBoth(m *move, t Technique, bit uint16, a, b int, skip cellSet) bool {
	any := false
	peerSet[a].and(peerSet[b]).andNot(skip).each(func(c int) {
		if s.cand[c]&bit != 0 {
			if !any {
				m.reset(t)
				any = true
			}
			m.elim[c] = bit
		}
	})
	return any
}

// skyscraper finds two parallel lines that each have exactly two spots for
// d, one of them on a shared cross line. Both lines can't use the shared
// cross line, so one of the other two spots (the tops) is d, and d goes
// from every cell that sees both tops. Rows are tried before columns.
func (s *state) skyscraper(m *move) bool {
	for d := uint8(1); d <= 9; d++ {
		bit := CandidateBit(d)
		for orient := 0; orient < 2; orient++ {
			var lines [9]int
			var masks [9]uint16
			k := 0
			for i := 0; i < 9; i++ {
				if mask := s.lineMask(orient*9+i, bit); bits.OnesCount16(mask) == 2 {
					lines[k], masks[k] = orient*9+i, mask
					k++
				}
			}
			for i := 0; i < k; i++ {
				for j := i + 1; j < k; j++ {
					shared := masks[i] & masks[j]
					if bits.OnesCount16(shared) != 1 {
						continue // no common cross line, or an X-wing
					}
					base := bits.TrailingZeros16(shared)
					ti, tj := bits.TrailingZeros16(masks[i]&^shared), bits.TrailingZeros16(masks[j]&^shared)
					top1, top2 := int(houseCells[lines[i]][ti]), int(houseCells[lines[j]][tj])
					if !s.seeBoth(m, Skyscraper, bit, top1, top2, cellSet{}) {
						continue
					}
					m.digit = d
					m.addHouse(lines[i])
					m.addHouse(lines[j])
					m.addHouse((1-orient)*9 + base)
					m.addCell(top1)
					m.addCell(int(houseCells[lines[i]][base]))
					m.addCell(int(houseCells[lines[j]][base]))
					m.addCell(top2)
					return true
				}
			}
		}
	}
	return false
}

// twoStringKite finds a row and a column that each have exactly two spots
// for d, one spot of each in the same box (different cells). Those two
// can't both be d, so one of the far ends is, and d goes from every cell
// that sees both ends.
func (s *state) twoStringKite(m *move) bool {
	for d := uint8(1); d <= 9; d++ {
		bit := CandidateBit(d)
		var colMasks [9]uint16
		for c := 0; c < 9; c++ {
			colMasks[c] = s.lineMask(9+c, bit)
		}
		for r := 0; r < 9; r++ {
			rm := s.lineMask(r, bit)
			if bits.OnesCount16(rm) != 2 {
				continue
			}
			rowCells := [2]int{r*9 + bits.TrailingZeros16(rm), r*9 + 15 - bits.LeadingZeros16(rm)}
			for c := 0; c < 9; c++ {
				cm := colMasks[c]
				if bits.OnesCount16(cm) != 2 {
					continue
				}
				colCells := [2]int{bits.TrailingZeros16(cm)*9 + c, (15-bits.LeadingZeros16(cm))*9 + c}
				for ri := 0; ri < 2; ri++ {
					for ci := 0; ci < 2; ci++ {
						rb, ra := rowCells[ri], rowCells[1-ri]
						cb, ce := colCells[ci], colCells[1-ci]
						box := cellBox[rb]
						if rb == cb || cellBox[cb] != box || cellBox[ra] == box || cellBox[ce] == box || ra == ce {
							continue
						}
						if !s.seeBoth(m, TwoStringKite, bit, ra, ce, cellSet{}) {
							continue
						}
						m.digit = d
						m.addHouse(r)
						m.addHouse(9 + c)
						m.addHouse(18 + int(box))
						m.addCell(ra)
						m.addCell(rb)
						m.addCell(cb)
						m.addCell(ce)
						return true
					}
				}
			}
		}
	}
	return false
}

// xyzWing finds a pivot {x,y,z} that sees pincers {x,z} and {y,z}. One of
// the three cells must be z, so z goes from every cell that sees all three.
func (s *state) xyzWing(m *move) bool {
	for p, pk := range s.cand {
		if bits.OnesCount16(pk) != 3 {
			continue
		}
		for i, a := range peers[p] {
			ak := s.cand[a]
			if bits.OnesCount16(ak) != 2 || ak&^pk != 0 {
				continue
			}
			for _, b := range peers[p][i+1:] {
				bk := s.cand[b]
				if bits.OnesCount16(bk) != 2 || bk&^pk != 0 || bk == ak {
					continue
				}
				z := ak & bk
				any := false
				peerSet[p].and(peerSet[a]).and(peerSet[b]).each(func(c int) {
					if s.cand[c]&z != 0 {
						if !any {
							m.reset(XYZWing)
							any = true
						}
						m.elim[c] = z
					}
				})
				if !any {
					continue
				}
				m.digit = lowDigit(z)
				m.digits = pk
				m.addCell(p)
				m.addCell(int(a))
				m.addCell(int(b))
				return true
			}
		}
	}
	return false
}

// wWing finds two cells that can each only be x or y and don't see each
// other, plus a house whose only two spots for x see one cell each. If
// neither cell were y, both would be x and that house would have no x, so
// y goes from every cell that sees both.
func (s *state) wWing(m *move) bool {
	var pairs [81]uint8
	n := 0
	for c, k := range s.cand {
		if bits.OnesCount16(k) == 2 {
			pairs[n] = uint8(c)
			n++
		}
	}
	for i := 0; i < n; i++ {
		a := int(pairs[i])
		k := s.cand[a]
		for _, bc := range pairs[i+1 : n] {
			b := int(bc)
			if s.cand[b] != k || peerSet[a].has(b) {
				continue
			}
			both := peerSet[a].and(peerSet[b])
			for xs := k; xs != 0; xs &= xs - 1 {
				x := xs & -xs
				y := k &^ x
				hasY := false
				both.each(func(c int) { hasY = hasY || s.cand[c]&y != 0 })
				if !hasY {
					continue
				}
				for h := 0; h < 27; h++ {
					p, q := s.pairIn(h, x)
					if p < 0 {
						continue
					}
					if !peerSet[a].has(p) || !peerSet[b].has(q) {
						p, q = q, p
						if !peerSet[a].has(p) || !peerSet[b].has(q) {
							continue
						}
					}
					s.seeBoth(m, WWing, y, a, b, cellSet{})
					m.digit = lowDigit(y)
					m.digits = k
					m.addHouse(h)
					m.addCell(a)
					m.addCell(b)
					m.addCell(p)
					m.addCell(q)
					return true
				}
			}
		}
	}
	return false
}

// pairIn returns the two cells of house h that have bit, or -1, -1 unless
// there are exactly two.
func (s *state) pairIn(h int, bit uint16) (int, int) {
	p, q := -1, -1
	for _, c := range houseCells[h] {
		if s.cand[c]&bit == 0 {
			continue
		}
		switch {
		case p < 0:
			p = int(c)
		case q < 0:
			q = int(c)
		default:
			return -1, -1
		}
	}
	if q < 0 {
		return -1, -1
	}
	return p, q
}

// maxChain is the most cells a colouring chain may have: long chains are
// hard to follow and wouldn't fit in a move.
const maxChain = len(move{}.cells)

// simpleColouring follows chains of houses where d has exactly two spots:
// along a chain the d's alternate. If both ends of an odd chain see a cell,
// one end is d and that cell isn't (a colour trap). If an even chain's ends
// share a house, they can't both be d, so neither is, nor is every other
// cell along it (a colour wrap). The shortest chain over all digits wins,
// so the hint stays short.
func (s *state) simpleColouring(m *move) bool {
	var best colourChain
	for d := uint8(1); d <= 9; d++ {
		s.colourDigit(d, &best)
	}
	if best.n == 0 {
		return false
	}
	bit := CandidateBit(best.digit)
	if best.n%2 == 1 {
		m.reset(SimpleColouring)
		for i := 0; i < best.n; i += 2 {
			m.elim[best.cells[i]] = bit
		}
	} else {
		s.seeBoth(m, SimpleColouring, bit, int(best.cells[0]), int(best.cells[best.n-1]), best.cluster)
	}
	m.digit = best.digit
	for _, c := range best.cells[:best.n] {
		m.addCell(int(c))
	}
	return true
}

// colourChain is the best colouring chain found so far: n cells (0 for
// none) for digit, end to end, in a cluster of linked cells.
type colourChain struct {
	digit   uint8
	n       int
	cells   [maxChain]uint8
	cluster cellSet
}

// colourDigit looks for a colouring chain on d shorter than best's and
// stores it there.
func (s *state) colourDigit(d uint8, best *colourChain) {
	bit := CandidateBit(d)
	var has cellSet
	var adj [81][3]uint8 // linked cells through row, column and box
	var deg [81]uint8
	for h := 0; h < 27; h++ {
		p, q := s.pairIn(h, bit)
		if p < 0 {
			continue
		}
		adj[p][deg[p]], adj[q][deg[q]] = uint8(q), uint8(p)
		deg[p]++
		deg[q]++
	}
	for c, k := range s.cand {
		if k&bit != 0 {
			has.add(c)
		}
	}
	var seen cellSet
	for start := 0; start < 81; start++ {
		if deg[start] == 0 || seen.has(start) {
			continue
		}
		// Colour the cluster breadth-first.
		var nodes [81]uint8
		var colour [81]uint8 // 1 or 2 for cluster cells
		var sides [3]cellSet
		n := 0
		nodes[n], colour[start] = uint8(start), 1
		n++
		seen.add(start)
		ok := true
		for i := 0; i < n; i++ {
			u := nodes[i]
			sides[colour[u]].add(int(u))
			for _, v := range adj[u][:deg[u]] {
				switch {
				case colour[v] == 0:
					colour[v] = 3 - colour[u]
					nodes[n] = v
					n++
					seen.add(int(v))
				case colour[v] == colour[u]:
					ok = false // an odd loop: the candidates are already broken
				}
			}
		}
		if !ok || n < 2 {
			continue
		}
		cluster := sides[1].or(sides[2])
		outside := has.andNot(cluster)
		for i := 0; i < n; i++ {
			u := int(nodes[i])
			dist, prev := clusterBFS(u, &adj, &deg)
			for j := 0; j < n; j++ {
				v := int(nodes[j])
				if v == u || int(dist[v]) >= maxChain {
					continue
				}
				if l := int(dist[v]) + 1; best.n != 0 && l >= best.n {
					continue
				}
				if colour[u] == colour[v] {
					// A wrap needs the two ends to share a house; count
					// each pair once.
					if v < u || !peerSet[u].has(v) {
						continue
					}
				} else if v < u || peerSet[u].and(peerSet[v]).and(outside).empty() {
					continue
				}
				// Trap or wrap: store the chain from u to v.
				best.digit, best.cluster = d, cluster
				best.n = int(dist[v]) + 1
				for k, c := best.n-1, v; k >= 0; k-- {
					best.cells[k] = uint8(c)
					c = int(prev[c])
				}
			}
		}
	}
}

// clusterBFS returns the number of links from u to every cell linked to it
// (directly or along a chain), and each one's predecessor on a shortest
// chain. Unreached cells get 127.
func clusterBFS(u int, adj *[81][3]uint8, deg *[81]uint8) (dist [81]int8, prev [81]uint8) {
	for i := range dist {
		dist[i] = 127
	}
	var queue [81]uint8
	queue[0], dist[u] = uint8(u), 0
	n := 1
	for i := 0; i < n; i++ {
		x := queue[i]
		for _, y := range adj[x][:deg[x]] {
			if dist[y] == 127 {
				dist[y], prev[y] = dist[x]+1, x
				queue[n] = y
				n++
			}
		}
	}
	return
}

// subsets calls f with each n-element subset of indexes into masks whose
// masks together have at most n bits set, as a bitmask of the indexes,
// along with that union. It goes in lexicographic order and stops when f
// returns true. A branch is dropped as soon as its union has more than n
// bits, which keeps quads cheap.
func subsets(masks []uint16, n int, f func(sel, union uint16) bool) bool {
	k := len(masks)
	if n > k || n <= 0 {
		return false
	}
	var at [9]int
	var union [10]uint16 // union[i]: the masks of at[:i]
	var sel uint16       // the bits of at[:i]
	i := 0
	for {
		for at[i] <= k-n+i && bits.OnesCount16(union[i]|masks[at[i]]) > n {
			at[i]++
		}
		if at[i] > k-n+i {
			if i == 0 {
				return false
			}
			i--
			sel &^= 1 << at[i]
			at[i]++
			continue
		}
		union[i+1] = union[i] | masks[at[i]]
		if i == n-1 {
			if f(sel|1<<at[i], union[n]) {
				return true
			}
			at[i]++
			continue
		}
		sel |= 1 << at[i]
		i++
		at[i] = at[i-1] + 1
	}
}
