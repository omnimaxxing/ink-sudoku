package kakuro

// state is a partly filled board with a candidate mask per open cell, as
// seen by both solvers.
//
// The candidates always respect the baseline that AutoCandidates computes:
// a cell keeps only digits that some combination of its across run and
// some combination of its down run still allow, given the digits already
// in those runs. place keeps that up to date (as placing a digit in Sudoku
// crosses it off the cell's peers), so "8 + 9 is the only way to make 17"
// and "this cell has to fit both runs" are never steps of their own: they
// shape the candidates, and the singles they produce are the first rungs
// of the technique ladder.
type state struct {
	b      *board
	val    [maxCells]uint8
	cand   [maxCells]uint16
	used   [maxRuns]uint16 // digits placed per run
	rem    [maxRuns]int8   // clue minus the digits placed
	open   [maxRuns]uint8  // open cells per run
	empty  int
	broken bool // some cell or run has no way left to be filled

	// ver counts changes to each run's cells. memo, if set, holds per
	// technique the run's ver when the technique last found nothing there,
	// so repeated searches skip runs that haven't changed since.
	ver  [maxRuns]uint32
	memo *memo
}

type memo [NumTechniques][maxRuns]uint32

// skip reports whether technique t already found nothing in run r as it
// is now.
func (s *state) skip(t Technique, r int) bool { return s.memo != nil && s.memo[t][r] == s.ver[r] }

// mark records that technique t finds nothing in run r as it is now.
func (s *state) mark(t Technique, r int) {
	if s.memo != nil {
		s.memo[t][r] = s.ver[r]
	}
}

// touch notes a change to cell c.
func (s *state) touch(c uint8) {
	ro := s.b.runOf[c]
	s.ver[ro[0]]++
	s.ver[ro[1]]++
}

// init sets s to b's empty board plus entries (nil or one digit per cell,
// 0 = empty). It reports false if entries repeat a digit in a run or sit on
// black cells; a board that is merely unsolvable comes back broken.
func (s *state) init(b *board, entries []uint8) bool {
	*s = state{b: b, memo: s.memo}
	if s.memo != nil {
		*s.memo = memo{}
	}
	for i := 0; i < b.nr; i++ {
		s.rem[i] = int8(b.runs[i].sum)
		s.open[i] = b.runs[i].n
		s.ver[i] = 1
	}
	for _, c := range b.whites[:b.nw] {
		s.cand[c] = allDigits
		s.empty++
	}
	for c, v := range entries {
		if v == 0 {
			continue
		}
		if c >= b.n*b.n || !b.white[c] || v > 9 {
			return false
		}
		for _, r := range b.runOf[c] {
			if s.used[r]&bit(v) != 0 {
				return false
			}
			s.used[r] |= bit(v)
			s.rem[r] -= int8(v)
			s.open[r]--
		}
		s.val[c], s.cand[c] = v, 0
		s.empty--
	}
	for r := 0; r < b.nr; r++ {
		s.refresh(r)
	}
	return true
}

// arith returns the union and intersection of run r's remaining
// combinations, the digit sets its open cells could hold given only the
// clue and the digits placed, and how many there are.
func (s *state) arith(r int) (union, inter uint16, n int) {
	k, rem := int(s.open[r]), int(s.rem[r])
	if rem < 0 || rem > 45 {
		return 0, 0, 0
	}
	inter = allDigits
	used := s.used[r]
	for _, c := range combos[k][rem] {
		if c&used == 0 {
			union |= c
			inter &= c
			n++
		}
	}
	if n == 0 {
		inter = 0
	}
	return union, inter, n
}

// refresh trims run r's open cells to the digits its combinations allow.
func (s *state) refresh(r int) {
	u, _, n := s.arith(r)
	if n == 0 {
		s.broken = true
		return
	}
	rn := &s.b.runs[r]
	for _, c := range rn.cells[:rn.n] {
		if k := s.cand[c]; s.val[c] == 0 && k&^u != 0 {
			s.cand[c] = k & u
			s.touch(c)
			if s.cand[c] == 0 {
				s.broken = true
			}
		}
	}
}

func (s *state) place(c int, d uint8) {
	s.val[c], s.cand[c] = d, 0
	s.empty--
	s.touch(uint8(c))
	for _, r := range s.b.runOf[c] {
		s.used[r] |= bit(d)
		s.rem[r] -= int8(d)
		s.open[r]--
		s.refresh(int(r))
	}
}

// openCells lists run r's open cells.
func (s *state) openCells(r int, buf *[9]uint8) []uint8 {
	rn := &s.b.runs[r]
	out := buf[:0]
	for _, c := range rn.cells[:rn.n] {
		if s.val[c] == 0 {
			out = append(out, c)
		}
	}
	return out
}

// fitCombos stores run r's remaining combinations in all and those that
// still fit its open cells in fit: every digit has a cell that can take it,
// and every cell can take one of its digits.
func (s *state) fitCombos(r int, all, fit *[maxCombos]uint16) (na, nf int) {
	var buf [9]uint8
	cells := s.openCells(r, &buf)
	var any uint16
	for _, c := range cells {
		any |= s.cand[c]
	}
	k, rem := len(cells), int(s.rem[r])
	if rem < 0 || rem > 45 {
		return 0, 0
	}
	for _, d := range combos[k][rem] {
		if d&s.used[r] != 0 {
			continue
		}
		all[na] = d
		na++
		if d&^any != 0 {
			continue
		}
		ok := true
		for _, c := range cells {
			if s.cand[c]&d == 0 {
				ok = false
				break
			}
		}
		if ok {
			fit[nf] = d
			nf++
		}
	}
	return na, nf
}
