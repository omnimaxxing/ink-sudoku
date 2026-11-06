package engine

import (
	"fmt"
	"math/bits"
	"slices"
	"strings"
)

// Cand is one candidate digit in one cell.
type Cand struct {
	Cell  int   `json:"cell"`
	Digit uint8 `json:"digit"`
}

// Step is one logical deduction, ready to show as a hint.
type Step struct {
	Technique Technique `json:"technique"`
	// Houses the reasoning is about, e.g. the box of a hidden single, or the
	// base lines then the cover lines of a fish.
	Houses []House `json:"houses,omitempty"`
	// Cells that form the pattern. For a Mistake, the offending cells.
	Cells  []int     `json:"cells,omitempty"`
	Digits DigitList `json:"digits,omitempty"`
	// Placement is set for singles and Reveal.
	Placement    *Cand  `json:"placement,omitempty"`
	Eliminations []Cand `json:"eliminations,omitempty"`
	// Title is the idea in plain words ("Only one place for the 7"); Text
	// explains it in a few short sentences, naming cells "row 4, column 7".
	Title string `json:"title"`
	Text  string `json:"text"`
}

// NextStep returns the easiest logical step from the player's position.
//
// entries holds the player's digits (values in given cells are ignored).
// candidates holds the player's pencil marks as CandidateBit masks; a cell
// with no marks (0) is treated as having every candidate that simple
// elimination allows, and marks are always intersected with that set.
//
// If an entry disagrees with the solution, or a cell's marks exclude its
// solution digit, the step is a Mistake naming those cells. If no technique
// applies, the step is a Reveal of one cell. ok is false when the givens
// don't have exactly one solution or the board is already solved.
//
// Elimination steps don't change the board. A caller that lets players skip
// pencil marks has to keep the eliminations itself (e.g. materialise the
// cell's AutoCandidates minus the eliminated digit), or the same hint comes
// back next time.
func NextStep(givens, entries Grid, candidates [81]uint16) (Step, bool) {
	sol, ok := uniqueSolution(givens)
	if !ok {
		return Step{}, false
	}
	board := givens
	var wrong []int
	for c, v := range entries {
		if givens[c] != 0 || v == 0 {
			continue
		}
		board[c] = v
		if v != sol[c] {
			wrong = append(wrong, c)
		}
	}
	if len(wrong) > 0 {
		return wrongEntries(board, wrong), true
	}
	if board == sol {
		return Step{}, false
	}

	s := state{val: board, cand: AutoCandidates(board)}
	var badNotes []int
	for c := range s.cand {
		if board[c] != 0 {
			continue
		}
		s.empty++
		if candidates[c] != 0 {
			s.cand[c] &= candidates[c]
		}
		if s.cand[c]&CandidateBit(sol[c]) == 0 {
			badNotes = append(badNotes, c)
		}
	}
	if len(badNotes) > 0 {
		st := Step{Technique: Mistake, Cells: badNotes}
		if len(badNotes) == 1 {
			st.Title = "Your notes rule out the answer here"
			st.Text = fmt.Sprintf("Your notes in %s leave out the number that belongs there. Check them again.", cellsPhrase(badNotes))
		} else {
			st.Title = "Your notes rule out some answers"
			st.Text = fmt.Sprintf("Your notes in %s each leave out the number that belongs there. Check them again.", cellsPhrase(badNotes))
		}
		return st, true
	}

	var m move
	if s.next(&m, lastTechnique) {
		return describe(&s, &m), true
	}
	return reveal(&s, sol), true
}

func uniqueSolution(g Grid) (Grid, bool) {
	s, ok := newBacktracker(g)
	if !ok {
		return Grid{}, false
	}
	s.limit = 2
	s.search()
	return s.first, s.count == 1
}

func wrongEntries(board Grid, wrong []int) Step {
	st := Step{Technique: Mistake, Cells: wrong}
	if len(wrong) > 1 {
		st.Title = "Some numbers are wrong"
		st.Text = fmt.Sprintf("%d of your numbers are wrong: %s.", len(wrong), cellsPhrase(wrong))
		return st
	}
	c := wrong[0]
	v := board[c]
	st.Digits = []uint8{v}
	for _, p := range peers[c] {
		if board[p] != v {
			continue
		}
		var h House
		switch {
		case cellRow[p] == cellRow[c]:
			h = House(cellRow[c])
		case cellCol[p] == cellCol[c]:
			h = House(9 + cellCol[c])
		default:
			h = House(18 + cellBox[c])
		}
		st.Houses = []House{h}
		st.Title = upper(fmt.Sprintf("%s already has %s", h, aDigit(v)))
		st.Text = fmt.Sprintf("%s can't be %s. %s already has one %s %s.",
			upper(cellPhrase(c)), aDigit(v), upper(h.String()), pick(h >= 18, "at", "in"), cellsIn(h, []int{int(p)}))
		return st
	}
	st.Title = fmt.Sprintf("That %d isn't right", v)
	st.Text = fmt.Sprintf("The %d in %s is wrong. Nothing clashes yet, but it leads to a dead end.", v, cellPhrase(c))
	return st
}

// reveal gives away the empty cell with the fewest candidates.
func reveal(s *state, sol Grid) Step {
	best, bestN := -1, 10
	for c, k := range s.cand {
		if s.val[c] == 0 && bits.OnesCount16(k) < bestN {
			best, bestN = c, bits.OnesCount16(k)
		}
	}
	return Step{
		Technique: Reveal,
		Cells:     []int{best},
		Digits:    []uint8{sol[best]},
		Placement: &Cand{best, sol[best]},
		Title:     "Here's one for free",
		Text:      fmt.Sprintf("None of my tricks work here, so here's a free one: %s is %s.", cellPhrase(best), aDigit(sol[best])),
	}
}

// describe turns a move found in s into a Step with hint text.
func describe(s *state, m *move) Step {
	st := Step{Technique: m.tech}
	hs := make([]House, m.nh)
	for i := range hs {
		hs[i] = House(m.houses[i])
	}
	if len(hs) > 0 {
		st.Houses = hs
	}
	st.Cells = make([]int, m.nc)
	for i := range st.Cells {
		st.Cells[i] = int(m.cells[i])
	}
	if m.digits != 0 {
		st.Digits = digitsOf(m.digits)
	} else {
		st.Digits = []uint8{m.digit}
	}
	if m.place >= 0 {
		st.Placement = &Cand{int(m.place), m.digit}
	}
	var elimCells []int
	for c, e := range m.elim {
		if e == 0 {
			continue
		}
		elimCells = append(elimCells, c)
		for _, d := range digitsOf(e) {
			st.Eliminations = append(st.Eliminations, Cand{c, d})
		}
	}
	d := m.digit
	cells := st.Cells
	ds := digitPhrase(st.Digits, "and")

	switch m.tech {
	case HiddenSingle:
		c, h := int(m.place), hs[0]
		if emptyIn(s, h) == 1 {
			st.Title = upper(h.String()) + " is missing just one number"
			st.Text = fmt.Sprintf("%s has one empty cell left: %s. The only number missing is %d.", upper(h.String()), cellsIn(h, []int{c}), d)
		} else {
			st.Title = fmt.Sprintf("Only one place for the %d", d)
			st.Text = fmt.Sprintf("In %s, the %d can only go in %s. Every other spot is ruled out.", h, d, cellsIn(h, []int{c}))
		}
	case NakedSingle:
		st.Title = "Only one number fits here"
		st.Text = fmt.Sprintf("%s can only be %s. Every other number is ruled out.", upper(cellPhrase(int(m.place))), aDigit(d))
	case Pointing, Claiming:
		from, to := hs[0], hs[1]
		st.Title = fmt.Sprintf("%s's %d must be in %s", upper(from.String()), d, to)
		st.Text = fmt.Sprintf("In %s, the %d can only go in %s. So %s gets its %d from %s. Cross %d off the rest of %s: %s.",
			from, d, to, to, d, from, d, to, cellsIn(to, elimCells))
	case NakedPair, NakedTriple, NakedQuad:
		h := hs[0]
		if m.tech == NakedPair {
			st.Title = "These two cells share " + ds
			st.Text = fmt.Sprintf("%s can only be %s. One is the %d, the other the %d.",
				upper(cellsPhrase(cells)), digitPhrase(st.Digits, "or"), st.Digits[0], st.Digits[1])
		} else {
			n := countWord(len(cells))
			st.Title = fmt.Sprintf("These %s cells share %s", n, ds)
			st.Text = fmt.Sprintf("%s can only hold %s between them. So those %s numbers go there.", upper(cellsPhrase(cells)), ds, n)
		}
		st.Text += fmt.Sprintf(" Cross %s off the rest of %s: %s.", ds, h, cellsIn(h, elimCells))
	case HiddenPair, HiddenTriple, HiddenQuad:
		h := hs[0]
		n := countWord(len(cells))
		st.Title = fmt.Sprintf("%s only fit in %s cells", ds, n)
		st.Text = fmt.Sprintf("In %s, the %s can only go in %s. So those %s cells hold %s. Cross every other number off them.",
			h, ds, cellsIn(h, cells), n, ds)
	case XWing, Swordfish:
		n := len(hs) / 2
		base, cover := housePhrase(hs[:n]), housePhrase(hs[n:])
		st.Title = fmt.Sprintf("The %ds in %s line up", d, base)
		lines, crosses := pick(hs[0] < 9, "rows", "columns"), pick(hs[0] < 9, "columns", "rows")
		st.Text = fmt.Sprintf("In %s, the %d can only go in %s. Those %s %s use up the %ds of %s %s. Cross %d off the rest of %s.",
			base, d, cover, pick(n == 2, "two", "three"), lines, d, pick(n == 2, "both", "all three"), crosses, d, cover)
	case XYWing:
		p, a, b := int(m.cells[0]), int(m.cells[1]), int(m.cells[2])
		ka, kb := s.cand[a], s.cand[b]
		x, y := lowDigit(ka&s.cand[p]), lowDigit(kb&s.cand[p])
		if x > y {
			x, y, a, b = y, x, b, a
		}
		st.Digits = []uint8{x, y, d}
		st.Title = fmt.Sprintf("One of two cells must be %s", aDigit(d))
		st.Text = fmt.Sprintf("%s is %d or %d. If it's %d, %s is %s. If it's %d, %s is %s. "+
			"Either way, one of them is %s. Cross %d off %s, which %s a row, column or box with both.",
			upper(cellPhrase(p)), x, y, x, cellPhrase(a), aDigit(d), y, cellPhrase(b), aDigit(d),
			aDigit(d), d, cellsPhrase(elimCells), pick(len(elimCells) == 1, "shares", "share"))
	case Skyscraper:
		line1, line2, cross := hs[0], hs[1], hs[2]
		top1, base1, base2, top2 := cells[0], cells[1], cells[2], cells[3]
		st.Title = fmt.Sprintf("The %ds in %s almost line up", d, housePhrase(hs[:2]))
		st.Text = fmt.Sprintf("In %s, the %d can only go in %s. In %s, it can only go in %s. "+
			"%s can't have %ds in both %s, so %s or %s uses its other spot: %s or %s. "+
			"Cross %d off %s, which %s a row, column or box with both.",
			line1, d, cellsIn(line1, []int{top1, base1}), line2, cellsIn(line2, []int{top2, base2}),
			upper(cross.String()), d, cellsIn(cross, []int{base1, base2}), line1, line2, cellPhrase(top1), cellPhrase(top2),
			d, cellsPhrase(elimCells), pick(len(elimCells) == 1, "shares", "share"))
	case TwoStringKite:
		row, col, box := hs[0], hs[1], hs[2]
		rowEnd, rowBox, colBox, colEnd := cells[0], cells[1], cells[2], cells[3]
		st.Title = fmt.Sprintf("The %ds in %s and %s meet in %s", d, row, col, box)
		st.Text = fmt.Sprintf("In %s, the %d can only go in %s. In %s, it can only go in %s. "+
			"%s can't have %ds at both %s and %s, so %s or %s uses its other spot: %s or %s. "+
			"Cross %d off %s, which %s a row, column or box with both.",
			row, d, cellsIn(row, []int{rowEnd, rowBox}), col, cellsIn(col, []int{colBox, colEnd}),
			upper(box.String()), d, cellPhrase(rowBox), cellPhrase(colBox), row, col, cellPhrase(rowEnd), cellPhrase(colEnd),
			d, cellsPhrase(elimCells), pick(len(elimCells) == 1, "shares", "share"))
	case XYZWing:
		p, a, b := cells[0], cells[1], cells[2]
		z := CandidateBit(d)
		x, y := lowDigit(s.cand[a]&^z), lowDigit(s.cand[b]&^z)
		if x > y {
			x, y, a, b = y, x, b, a
		}
		st.Digits = []uint8{x, y, d}
		st.Title = fmt.Sprintf("One of three cells must be %s", aDigit(d))
		st.Text = fmt.Sprintf("%s is %s. If it's %d, %s is %s; if it's %d, %s is %s; and if it's %d, it's the %d itself. "+
			"Either way, one of these three cells is %s. Cross %d off %s, which %s a row, column or box with all three.",
			upper(cellPhrase(p)), digitPhrase(digitsOf(m.digits), "or"), x, cellPhrase(a), aDigit(d), y, cellPhrase(b), aDigit(d), d, d,
			aDigit(d), d, cellsPhrase(elimCells), pick(len(elimCells) == 1, "shares", "share"))
	case WWing:
		a, b, p, q := cells[0], cells[1], cells[2], cells[3]
		h := hs[0]
		x := lowDigit(m.digits &^ CandidateBit(d))
		st.Digits = []uint8{x, d}
		st.Title = fmt.Sprintf("Two %d-or-%d cells, linked by the %ds", min(x, d), max(x, d), x)
		st.Text = fmt.Sprintf("%s can each only be %d or %d. In %s, the %d can only go in %s, and each of those spots shares a row, column or box with one of the two cells. "+
			"If neither cell were %s, both would be %ds, and %s would have no spot left for its %d. So one of them is %s. "+
			"Cross %d off %s, which %s a row, column or box with both.",
			upper(cellsPhrase([]int{a, b})), min(x, d), max(x, d), h, x, cellsIn(h, []int{p, q}),
			aDigit(d), x, h, x, aDigit(d),
			d, cellsPhrase(elimCells), pick(len(elimCells) == 1, "shares", "share"))
	case SimpleColouring:
		bit := CandidateBit(d)
		links := make([]string, len(cells)-1)
		st.Houses = make([]House, len(cells)-1)
		for i := range links {
			h := linkHouse(s, cells[i], cells[i+1], bit)
			st.Houses[i] = h
			links[i] = fmt.Sprintf("in %s (%s)", h, linkSpots(h, cells[i], cells[i+1]))
		}
		first, last := cells[0], cells[len(cells)-1]
		st.Title = fmt.Sprintf("Follow the chain of %ds", d)
		st.Text = fmt.Sprintf("The %d has only two spots %s. Those pairs link up into a chain, and each pair holds one %d, so every other spot along it is %s. ",
			d, joinList(links, "and"), d, aDigit(d))
		if len(cells)%2 == 0 {
			st.Text += fmt.Sprintf("One of its two ends is %s: %s or %s. Cross %d off %s, which %s a row, column or box with both.",
				aDigit(d), cellPhrase(first), cellPhrase(last), d, cellsPhrase(elimCells), pick(len(elimCells) == 1, "shares", "share"))
		} else {
			st.Text += fmt.Sprintf("If %s were %s, %s would be one too, but they share %s. "+
				"So the %ds are in the other spots. Cross %d off %s.",
				cellPhrase(first), aDigit(d), cellPhrase(last), sharedHouse(first, last), d, d, cellsPhrase(elimCells))
		}
	}
	return st
}

// linkHouse returns the house in which a and b are the only two spots for
// bit.
func linkHouse(s *state, a, b int, bit uint16) House {
	for _, h := range [3]int{int(cellRow[a]), 9 + int(cellCol[a]), 18 + int(cellBox[a])} {
		if p, q := s.pairIn(h, bit); p >= 0 && (p == a && q == b || p == b && q == a) {
			return House(h)
		}
	}
	return sharedHouse(a, b)
}

// linkSpots names a and b within house h in chain order: "columns 7 and
// 2" in a row, full names in a box.
func linkSpots(h House, a, b int) string {
	switch {
	case h < 9:
		return fmt.Sprintf("columns %d and %d", a%9+1, b%9+1)
	case h < 18:
		return fmt.Sprintf("rows %d and %d", a/9+1, b/9+1)
	}
	return cellPhrase(a) + " and " + cellPhrase(b)
}

// sharedHouse returns a house holding both a and b: their row, else their
// column, else their box.
func sharedHouse(a, b int) House {
	switch {
	case cellRow[a] == cellRow[b]:
		return House(cellRow[a])
	case cellCol[a] == cellCol[b]:
		return House(9 + cellCol[a])
	}
	return House(18 + cellBox[a])
}

// countWord spells out the size of a subset.
func countWord(n int) string {
	return [...]string{"", "one", "two", "three", "four"}[n]
}

func emptyIn(s *state, h House) int {
	n := 0
	for _, c := range houseCells[h] {
		if s.val[c] == 0 {
			n++
		}
	}
	return n
}

// cellPhrase names a cell the way people say it: "row 4, column 7".
func cellPhrase(c int) string { return fmt.Sprintf("row %d, column %d", c/9+1, c%9+1) }

// cellsPhrase names cells compactly: "row 4, columns 2 and 7" when they
// share a row, "column 3, rows 1 and 5" when they share a column, else rows
// in turn ("row 1, column 2; row 4, columns 5 and 6; and row 9, column 1"),
// eliding long lists.
func cellsPhrase(cells []int) string {
	cs := slices.Clone(cells)
	slices.Sort(cs)
	cs = slices.Compact(cs)
	if len(cs) == 0 {
		return ""
	}
	if len(cs) > 1 && slices.IndexFunc(cs, func(c int) bool { return c%9 != cs[0]%9 }) < 0 {
		return fmt.Sprintf("column %d, %s", cs[0]%9+1, numbered("row", cs, func(c int) int { return c / 9 }))
	}
	const maxGroups = 4
	var groups []string
	for i := 0; i < len(cs); {
		j := i
		for j < len(cs) && cs[j]/9 == cs[i]/9 {
			j++
		}
		if len(groups) == maxGroups-1 && j < len(cs) {
			groups = append(groups, fmt.Sprintf("%d more cells", len(cs)-i))
			break
		}
		groups = append(groups, fmt.Sprintf("row %d, %s", cs[i]/9+1, numbered("column", cs[i:j], func(c int) int { return c % 9 })))
		i = j
	}
	switch n := len(groups); n {
	case 1:
		return groups[0]
	case 2:
		return groups[0] + " and " + groups[1]
	default:
		return strings.Join(groups[:n-1], "; ") + "; and " + groups[n-1]
	}
}

// cellsIn names cells of house h relative to it: "columns 1 and 8" in a
// row, "row 2" in a column, full names in a box.
func cellsIn(h House, cells []int) string {
	cs := slices.Clone(cells)
	slices.Sort(cs)
	switch {
	case h < 9:
		return numbered("column", cs, func(c int) int { return c % 9 })
	case h < 18:
		return numbered("row", cs, func(c int) int { return c / 9 })
	}
	return cellsPhrase(cs)
}

// numbered is "row 3" or "rows 1, 4 and 6": the 1-based index of each cell.
func numbered(noun string, cells []int, index func(int) int) string {
	s := make([]string, len(cells))
	for i, c := range cells {
		s[i] = fmt.Sprint(index(c) + 1)
	}
	if len(s) > 1 {
		noun += "s"
	}
	return noun + " " + joinList(s, "and")
}

// aDigit is the digit with its article: "a 7", "an 8".
func aDigit(d uint8) string {
	if d == 8 {
		return "an 8"
	}
	return fmt.Sprintf("a %d", d)
}

func upper(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func digitPhrase(ds []uint8, conj string) string {
	s := make([]string, len(ds))
	for i, d := range ds {
		s[i] = fmt.Sprint(d)
	}
	return joinList(s, conj)
}

// housePhrase names same-kind houses: "rows 2 and 7", "columns 1, 4 and 9".
func housePhrase(hs []House) string {
	kind, base := "rows", 0
	if hs[0] >= 9 {
		kind, base = "columns", 9
	}
	s := make([]string, len(hs))
	for i, h := range hs {
		s[i] = fmt.Sprint(int(h) - base + 1)
	}
	return kind + " " + joinList(s, "and")
}

func joinList(s []string, conj string) string {
	switch len(s) {
	case 0:
		return ""
	case 1:
		return s[0]
	}
	return strings.Join(s[:len(s)-1], ", ") + " " + conj + " " + s[len(s)-1]
}

func pick(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}
