package kakuro

import (
	"fmt"
	"slices"
	"strings"
)

// NextStep returns the easiest logical step from the player's position.
//
// entries holds the player's digits, one per cell (0 = empty; anything on
// a black cell is ignored). candidates holds the player's pencil marks (bit
// d-1 = digit d); a cell with no marks (0) is treated as having every digit
// AutoCandidates allows, and marks are always intersected with that set.
// Either slice may be nil.
//
// If an entry disagrees with the solution, or a cell's marks exclude its
// solution digit, the step is a Mistake naming those cells. If no technique
// applies, the step is a Reveal of one cell. ok is false when the board is
// already solved, or p is malformed or, lacking a Solution, doesn't have
// exactly one.
//
// Elimination steps don't change the board. A caller that lets players skip
// pencil marks has to keep the eliminations itself (e.g. materialise the
// cell's AutoCandidates minus the eliminated digit), or the same hint comes
// back next time.
func NextStep(p Puzzle, entries []uint8, candidates []uint16) (Step, bool) {
	b, err := puzzleBoard(p)
	if err != nil {
		return Step{}, false
	}
	sol := p.Solution
	if len(sol) != len(p.Cells) || !b.fits(sol, true) {
		n, s := countSolutions(b, 2)
		if n != 1 {
			return Step{}, false
		}
		sol = s[:len(p.Cells)]
	}
	board := make([]uint8, len(p.Cells))
	var wrong []int
	solved := true
	for _, c := range b.whites[:b.nw] {
		if int(c) < len(entries) && entries[c] != 0 {
			board[c] = entries[c]
			if board[c] != sol[c] {
				wrong = append(wrong, int(c))
			}
		} else {
			solved = false
		}
	}
	if len(wrong) > 0 {
		return wrongEntries(b, board, wrong), true
	}
	if solved {
		return Step{}, false
	}

	var s state
	s.init(b, board)
	var badNotes []int
	for _, c := range b.whites[:b.nw] {
		if s.val[c] != 0 {
			continue
		}
		if int(c) < len(candidates) && candidates[c] != 0 {
			s.cand[c] &= candidates[c]
		}
		if s.cand[c]&bit(sol[c]) == 0 {
			badNotes = append(badNotes, int(c))
		}
	}
	if len(badNotes) > 0 {
		st := Step{Technique: Mistake.String(), Cells: badNotes}
		if len(badNotes) == 1 {
			st.Title = "Your notes rule out the answer here"
			st.Text = fmt.Sprintf("Your notes in %s leave out the number that belongs there. Check them again.", cellsPhrase(b.n, badNotes))
		} else {
			st.Title = "Your notes rule out some answers"
			st.Text = fmt.Sprintf("Your notes in %s each leave out the number that belongs there. Check them again.", cellsPhrase(b.n, badNotes))
		}
		return st, true
	}

	var m move
	if s.nextIn(&m, upTo(lastTechnique)) {
		return describe(&s, &m), true
	}
	return reveal(&s, sol), true
}

func wrongEntries(b *board, board []uint8, wrong []int) Step {
	st := Step{Technique: Mistake.String(), Cells: wrong}
	if len(wrong) > 1 {
		st.Title = "Some numbers are wrong"
		st.Text = fmt.Sprintf("%d of your numbers are wrong: %s.", len(wrong), cellsPhrase(b.n, wrong))
		return st
	}
	c := wrong[0]
	v := int(board[c])
	st.Digits = []int{v}
	for _, r := range b.runOf[c] {
		rn := &b.runs[r]
		for _, x := range rn.cells[:rn.n] {
			if int(x) != c && int(board[x]) == v {
				st.Title = fmt.Sprintf("Two %ds in one run", v)
				st.Text = fmt.Sprintf("%s can't be %s. %s, already has one in %s.",
					upper(cellPhrase(b.n, c)), aDigit(v), upper(runName(b, int(r))), inRun(b, int(r), []int{int(x)}))
				return st
			}
		}
	}
	for _, r := range b.runOf[c] {
		rn := &b.runs[r]
		sum, full := 0, true
		for _, x := range rn.cells[:rn.n] {
			sum += int(board[x])
			full = full && board[x] != 0
		}
		if full && sum != int(rn.sum) {
			st.Title = "This run doesn't add up"
			st.Text = fmt.Sprintf("%s, adds up to %d, but its clue is %d.", upper(runName(b, int(r))), sum, rn.sum)
			return st
		}
	}
	st.Title = fmt.Sprintf("That %d isn't right", v)
	st.Text = fmt.Sprintf("The %d in %s is wrong. Nothing clashes yet, but it leads to a dead end.", v, cellPhrase(b.n, c))
	return st
}

// reveal gives away the open cell with the fewest candidates.
func reveal(s *state, sol []uint8) Step {
	best, bestN := -1, 10
	for _, c := range s.b.whites[:s.b.nw] {
		if n := popcount(s.cand[c]); s.val[c] == 0 && n < bestN {
			best, bestN = int(c), n
		}
	}
	d := int(sol[best])
	return Step{
		Technique: Reveal.String(),
		Cells:     []int{best},
		Digits:    []int{d},
		Placement: &Cand{best, d},
		Title:     "Here's one for free",
		Text:      fmt.Sprintf("None of my tricks work here, so here's a free one: %s is %s.", cellPhrase(s.b.n, best), aDigit(d)),
	}
}

// describe turns a move found in s into a Step with hint text.
func describe(s *state, m *move) Step {
	b := s.b
	n := b.n
	st := Step{Technique: m.tech.String()}
	r := int(m.run)
	if r >= 0 {
		st.Cells = runCells(b, r)
	}
	if m.place >= 0 {
		st.Placement = &Cand{int(m.place), int(m.digit)}
		st.Digits = []int{int(m.digit)}
	}
	var elimCells []int
	var elimAll uint16
	if m.place < 0 {
		rn := &b.runs[r]
		for i, e := range m.elim[:rn.n] {
			if e == 0 {
				continue
			}
			c := int(rn.cells[i])
			elimCells = append(elimCells, c)
			elimAll |= e
			for _, d := range digitsOf(e) {
				st.Eliminations = append(st.Eliminations, Cand{c, d})
			}
		}
	}
	d := int(m.digit)
	at := ""
	if m.place >= 0 {
		at = cellPhrase(n, int(m.place))
	}

	switch m.tech {
	case LastCell:
		rn := &b.runs[r]
		st.Title = "One cell left in this run"
		st.Text = fmt.Sprintf("%s, adds up to %d. Its other cells already make %d, so %s is %d.",
			upper(runName(b, r)), rn.sum, int(rn.sum)-d, at, d)
	case UniqueCombination, CrossRun:
		a, dn := r, int(m.other)
		st.Cells = slices.Concat(runCells(b, a), runCells(b, dn))
		slices.Sort(st.Cells)
		st.Cells = slices.Compact(st.Cells)
		if m.tech == UniqueCombination {
			st.Title = "Two fixed runs cross here"
			st.Text = fmt.Sprintf("%s is where two runs with only one combination each cross. Across, %s. Down, %s. The only number in both is %d.",
				upper(at), runCombos(s, a), runCombos(s, dn), d)
		} else {
			st.Title = "Only one number fits both runs"
			st.Text = fmt.Sprintf("%s has to fit both its runs. Across, %s. Down, %s. The only number both allow is %d.",
				upper(at), runCombos(s, a), runCombos(s, dn), d)
		}
	case RequiredDigit:
		_, _, k := s.arith(r)
		st.Title = fmt.Sprintf("This run needs %s", aDigit(d))
		st.Text = fmt.Sprintf("In %s, %s. %s %s, and %s is the only one of its cells that can be %d.",
			runName(b, r), runCombos(s, r), pick(k == 1, "It needs", "Every one of them needs"), aDigit(d), inRun(b, r, []int{int(m.place)}), d)
	case NakedSingle:
		st.Cells = []int{int(m.place)}
		st.Title = "Only one number left"
		st.Text = fmt.Sprintf("%s can only be %s. Every other number has been ruled out.", upper(at), aDigit(d))
	case CombinationExclusion:
		st.Title, st.Text = describeExclusion(s, m, elimCells)
	case TwoCellSum:
		rn := &b.runs[r]
		x, y := elimCells[0], int(m.other)
		sum := int(s.rem[r])
		var mirror []int
		for _, e := range digitsOf(s.cand[y]) {
			if f := sum - e; f >= 1 && f <= 9 && f != e {
				mirror = append(mirror, f)
			}
		}
		slices.Sort(mirror)
		st.Cells = []int{y, x}
		slices.Sort(st.Cells)
		lead := fmt.Sprintf("The last two open cells of %s,", runName(b, r))
		if s.open[r] == rn.n {
			lead = fmt.Sprintf("The two cells of %s,", runName(b, r))
		}
		st.Title = fmt.Sprintf("These two cells add up to %d", sum)
		st.Text = fmt.Sprintf("%s must add up to %d. %s can only be %s, so %s can only be %s. Cross %s off %s.",
			lead, sum, upper(cellPhrase(n, y)), setPhrase(s.cand[y], "or"), cellPhrase(n, x), orList(mirror),
			setPhrase(elimAll, "and"), cellPhrase(n, x))
	case NakedPair, NakedTriple:
		cells := make([]int, m.nc)
		for i := range cells {
			cells[i] = int(m.cells[i])
		}
		st.Cells, st.Digits = cells, digitsOf(m.digits)
		ds := setPhrase(m.digits, "and")
		if m.tech == NakedPair {
			st.Title = "These two cells share " + ds
			st.Text = fmt.Sprintf("In %s, %s can only be %s. One is the %d, the other the %d.",
				runName(b, r), inRun(b, r, cells), setPhrase(m.digits, "or"), st.Digits[0], st.Digits[1])
		} else {
			st.Title = "These three cells share " + ds
			st.Text = fmt.Sprintf("In %s, %s can only hold %s between them, so those three numbers go there.",
				runName(b, r), inRun(b, r, cells), ds)
		}
		st.Text += fmt.Sprintf(" Cross %s off the rest of the run: %s.", ds, inRun(b, r, elimCells))
	case HiddenPair, HiddenTriple:
		cells := make([]int, m.nc)
		for i := range cells {
			cells[i] = int(m.cells[i])
		}
		st.Cells, st.Digits = cells, digitsOf(m.digits)
		ds := setPhrase(m.digits, "and")
		k := countWord(len(cells))
		st.Title = fmt.Sprintf("%s only fit in %s cells", ds, k)
		st.Text = fmt.Sprintf("%s, must hold %s: every combination that still fits its cells has them. They can only go in %s, so those %s cells are %s. Cross every other number off them.",
			upper(runName(b, r)), ds, inRun(b, r, cells), k, ds)
	case SumArithmetic:
		x := elimCells[0]
		var others []string
		for _, c := range runCells(b, r) {
			if s.val[c] == 0 && c != x {
				others = append(others, fmt.Sprintf("%s: %s", inRun(b, r, []int{c}), setPhrase(s.cand[c], "or")))
			}
		}
		var needs []int
		for _, e := range digitsOf(elimAll) {
			needs = append(needs, int(s.rem[r])-e)
		}
		who := "the other open cells"
		if len(others) <= 4 {
			who += " (" + strings.Join(others, "; ") + ")"
		}
		st.Title = fmt.Sprintf("%s can't be %s", upper(cellPhrase(n, x)), setPhrase(elimAll, "or"))
		st.Text = fmt.Sprintf("The %s open cells of %s, must add up to %d with different numbers. If %s were %s, %s would need to make %s, and they can't. Cross %s off it.",
			countWord(int(s.open[r])), runName(b, r), s.rem[r], cellPhrase(n, x), setPhrase(elimAll, "or"), who, orList(needs), setPhrase(elimAll, "and"))
	}
	return st
}

// describeExclusion explains a CombinationExclusion move.
func describeExclusion(s *state, m *move, elimCells []int) (title, text string) {
	b, r := s.b, int(m.run)
	var all, fit [maxCombos]uint16
	na, nf := s.fitCombos(r, &all, &fit)
	var sb strings.Builder
	if na <= 4 {
		fmt.Fprintf(&sb, "In %s, %s.", runName(b, r), runCombos(s, r))
		shown := 0
		for _, c := range all[:na] {
			if slices.Contains(fit[:nf], c) {
				continue
			}
			if shown == 2 {
				sb.WriteString(" Nor do the others.")
				break
			}
			sb.WriteString(" " + whyNot(s, r, c) + ".")
			shown++
		}
		fmt.Fprintf(&sb, " That leaves %s.", combosPhrase(fit[:nf]))
	} else {
		fmt.Fprintf(&sb, "%s, %s, which has %s combinations, but only %s still fit its cells.",
			upper(runName(b, r)), runSum(s, r), countWord(na), pick(nf <= 4, combosPhrase(fit[:nf]), fmt.Sprintf("%d of them", nf)))
	}
	if m.place >= 0 {
		d := int(m.digit)
		title = fmt.Sprintf("This run needs %s", aDigit(d))
		fmt.Fprintf(&sb, " %s %s, and %s is the only cell that can take it.",
			pick(nf == 1, "It needs", "Each of them needs"), aDigit(d), inRun(b, r, []int{int(m.place)}))
		return title, sb.String()
	}
	title = "Some combinations don't fit"
	if nf == 1 {
		title = "Only one combination fits"
	}
	fmt.Fprintf(&sb, " %s", crossOff(s, m, elimCells))
	return title, sb.String()
}

// whyNot says why combination c doesn't fit run r's cells.
func whyNot(s *state, r int, c uint16) string {
	b := s.b
	var any uint16
	var open []int
	for _, x := range runCells(b, r) {
		if s.val[x] == 0 {
			any |= s.cand[x]
			open = append(open, x)
		}
	}
	if miss := c &^ any; miss != 0 {
		d := int(lowDigit(miss))
		return fmt.Sprintf("%s needs %s, and none of its cells can be %d", comboPhrase(c), aDigit(d), d)
	}
	for _, x := range open {
		if s.cand[x]&c == 0 {
			return fmt.Sprintf("%s doesn't fit %s, which can only be %s", comboPhrase(c), cellPhrase(b.n, x), setPhrase(s.cand[x], "or"))
		}
	}
	return comboPhrase(c) + " doesn't fit"
}

// crossOff lists a move's eliminations: "Cross 2 and 5 off row 3, columns
// 4 and 5." or, when the cells lose different digits, cell by cell.
func crossOff(s *state, m *move, cells []int) string {
	b := s.b
	rn := &b.runs[m.run]
	same := true
	var first uint16
	var parts []string
	for i, e := range m.elim[:rn.n] {
		if e == 0 {
			continue
		}
		if first == 0 {
			first = e
		}
		same = same && e == first
		parts = append(parts, fmt.Sprintf("%s from %s", setPhrase(e, "and"), cellPhrase(b.n, int(rn.cells[i]))))
	}
	if same {
		return fmt.Sprintf("Cross %s off %s.", setPhrase(first, "and"), cellsPhrase(b.n, cells))
	}
	return "Cross off " + strings.Join(parts, "; ") + "."
}

// runSum states what run r's open cells must add up to: "adds up to 17",
// or "must make 9 in its two open cells, with 1 and 7 already in".
func runSum(s *state, r int) string {
	rn := &s.b.runs[r]
	if s.open[r] == rn.n {
		return fmt.Sprintf("adds up to %d", rn.sum)
	}
	return fmt.Sprintf("must make %d in its %s open cells, with %s already in", s.rem[r], countWord(int(s.open[r])), setPhrase(s.used[r], "and"))
}

// runCombos describes run r's remaining combinations: "17 in two cells
// can only be 8 + 9", "8 in three cells is 1 + 2 + 5 or 1 + 3 + 4", "the
// other 9 in two cells (with 1 already in) is 2 + 7, 3 + 6 or 4 + 5".
func runCombos(s *state, r int) string {
	rn := &s.b.runs[r]
	k, rem := int(s.open[r]), int(s.rem[r])
	what := fmt.Sprintf("%d in %s cells", rem, countWord(k))
	if k < int(rn.n) {
		what = fmt.Sprintf("the other %d in %s cells (with %s already in)", rem, countWord(k), setPhrase(s.used[r], "and"))
	}
	var list []uint16
	var union uint16
	if rem >= 0 && rem <= 45 {
		for _, c := range combos[k][rem] {
			if c&s.used[r] == 0 {
				list = append(list, c)
				union |= c
			}
		}
	}
	switch {
	case len(list) == 1:
		return what + " can only be " + comboPhrase(list[0])
	case len(list) <= 4:
		return what + " is " + combosPhrase(list)
	case union == allDigits:
		return what + " can use any digit"
	}
	return what + " can only use " + setPhrase(union, "and")
}

// inRun names cells of run r relative to it: "columns 3 and 5" in an across
// run, "row 4" in a down run.
func inRun(b *board, r int, cells []int) string {
	if b.runs[r].down {
		return numbered("row", cells, func(c int) int { return c / b.n })
	}
	return numbered("column", cells, func(c int) int { return c % b.n })
}

func orList(ds []int) string { return digitPhrase(ds, "or") }

// Check returns the white cells in conflict, ascending: every cell holding a
// digit that appears twice in one of its runs, and every cell of a full run
// whose digits don't add up to its clue. entries has one digit per cell
// (0 = empty); anything on black cells is ignored.
func Check(p Puzzle, entries []uint8) []int {
	b, err := puzzleBoard(p)
	if err != nil {
		return nil
	}
	var bad [maxCells]bool
	for i := 0; i < b.nr; i++ {
		rn := &b.runs[i]
		var count [10]int
		sum, full := 0, true
		for _, c := range rn.cells[:rn.n] {
			v := entry(entries, int(c))
			count[v]++
			sum += int(v)
			full = full && v != 0
		}
		for _, c := range rn.cells[:rn.n] {
			if v := entry(entries, int(c)); v != 0 && count[v] > 1 || full && sum != int(rn.sum) {
				bad[c] = true
			}
		}
	}
	var out []int
	for _, c := range b.whites[:b.nw] {
		if bad[c] {
			out = append(out, int(c))
		}
	}
	return out
}

// entry reads entries[c] as a digit, 0 if missing or out of range.
func entry(entries []uint8, c int) uint8 {
	if c < len(entries) && entries[c] <= 9 {
		return entries[c]
	}
	return 0
}

// AutoCandidates returns, for each empty white cell, the digits that some
// combination of its across run and some combination of its down run still
// allow, given the entries already in those runs (bit d-1 = digit d).
// Filled and black cells get 0, as does a cell whose runs can no longer be
// completed.
func AutoCandidates(p Puzzle, entries []uint8) []uint16 {
	out := make([]uint16, len(p.Cells))
	b, err := puzzleBoard(p)
	if err != nil {
		return out
	}
	var allow [maxRuns]uint16
	for i := 0; i < b.nr; i++ {
		rn := &b.runs[i]
		var used uint16
		open, rem := 0, int(rn.sum)
		for _, c := range rn.cells[:rn.n] {
			if v := entry(entries, int(c)); v != 0 {
				used |= bit(v)
				rem -= int(v)
			} else {
				open++
			}
		}
		if rem < 0 || rem > 45 {
			continue
		}
		for _, d := range combos[open][rem] {
			if d&used == 0 {
				allow[i] |= d
			}
		}
	}
	for _, c := range b.whites[:b.nw] {
		if entry(entries, int(c)) == 0 {
			ro := b.runOf[c]
			out[c] = allow[ro[0]] & allow[ro[1]]
		}
	}
	return out
}
