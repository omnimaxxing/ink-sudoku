package kakuro

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// position is a puzzle part way through: white cells in its grid are
// written "d" (entered), "d?" (open, solution d, no notes) or "d:xyz"
// (open, solution d, notes x, y, z).
type position struct {
	p       Puzzle
	entries []uint8
	notes   []uint16
}

func parsePosition(t *testing.T, s string) position {
	t.Helper()
	var plain strings.Builder
	type mark struct {
		open  bool
		notes uint16
	}
	var marks []mark
	for _, line := range strings.Split(s, "\n") {
		for _, tok := range strings.Fields(line) {
			m := mark{}
			if len(tok) > 1 && tok[0] >= '1' && tok[0] <= '9' && (tok[1] == '?' || tok[1] == ':') {
				m.open = true
				for _, r := range tok[2:] {
					m.notes |= bit(uint8(r - '0'))
				}
				tok = tok[:1]
			}
			marks = append(marks, m)
			plain.WriteString(tok + " ")
		}
		plain.WriteString("\n")
	}
	p := mustParse(t, plain.String())
	ps := position{p: p, entries: make([]uint8, len(p.Cells)), notes: make([]uint16, len(p.Cells))}
	for c, m := range marks {
		if !p.Cells[c].Black && !m.open {
			ps.entries[c] = p.Solution[c]
		}
		ps.notes[c] = m.notes
	}
	return ps
}

// state is the position as the hint engine sees it.
func (ps position) state(t *testing.T) *state {
	t.Helper()
	b, err := puzzleBoard(ps.p)
	if err != nil {
		t.Fatal(err)
	}
	s := new(state)
	if !s.init(b, ps.entries) {
		t.Fatal("entries clash")
	}
	for c, k := range ps.notes {
		if k != 0 {
			s.cand[c] &= k
		}
		if s.val[c] == 0 && b.white[c] && s.cand[c]&bit(ps.p.Solution[c]) == 0 {
			t.Fatalf("position rules out the solution at %s", cellPhrase(b.n, c))
		}
	}
	return s
}

// Boards for the positions. tiny1 and tiny2 are two runs crossing two
// runs; boardC and boardD have loose column sums, so the row under test
// decides.
const (
	tiny1 = `
#####   4\    3\
  \3    1?    2?
  \4    3?    1?`
	tiny2 = `
#####  11\    4\
  \3    2?    1?
  \12   9?    3?`
	boardC = `
#####  23\   19\   13\   27\
  \12   %s    %s    %s    %s
  \23    9?    4?    2?    8?
  \21    5?    8?    1?    7?
  \26    7?    6?    4?    9?`
	boardD = `
#####  24\   23\   18\   25\   26\
  \17   %s    %s    %s    %s    %s
  \25    2?    3?    4?    7?    9?
  \23    9?    8?    1?    2?    3?
  \25    4?    9?    8?    3?    1?
  \26    8?    1?    2?    9?    6?`
)

func row1(board string, cells ...any) string { return fmt.Sprintf(board, cells...) }

// Each technique on a small hand-made position: its finder must make the
// expected move, which must agree with the solution.
func TestTechniques(t *testing.T) {
	cases := []struct {
		tech  Technique
		pos   string
		place *Cand
		elim  []Cand
		text  string // optional snapshot of the hint text
	}{
		{tech: LastCell, pos: row1(boardC, "2", "1", "6", "3?"), place: &Cand{9, 3},
			text: "The across run in row 1, columns 1 to 4, adds up to 12. Its other cells already make 9, so row 1, column 4 is 3."},
		{tech: UniqueCombination, pos: tiny1, place: &Cand{4, 1},
			text: "Row 1, column 1 is where two runs with only one combination each cross. Across, 3 in two cells can only be 1 + 2. Down, 4 in two cells can only be 1 + 3. The only number in both is 1."},
		{tech: CrossRun, pos: tiny2, place: &Cand{4, 2}},
		{tech: RequiredDigit, pos: row1(boardC, "2:234", "1?", "6:3456", "3?"), place: &Cand{7, 1}},
		{tech: NakedSingle, pos: row1(boardC, "2:2", "1?", "6?", "3?"), place: &Cand{6, 2}},
		{tech: CombinationExclusion, pos: row1(boardC, "2?", "1?", "6?", "3:36"),
			elim: []Cand{{6, 4}, {6, 5}, {7, 4}, {7, 5}, {8, 4}, {8, 5}},
			text: "In the across run in row 1, columns 1 to 4, 12 in four cells is 1 + 2 + 3 + 6 or 1 + 2 + 4 + 5. 1 + 2 + 4 + 5 doesn't fit row 1, column 4, which can only be 3 or 6. That leaves 1 + 2 + 3 + 6. Cross 4 and 5 off row 1, columns 1 to 3."},
		{tech: TwoCellSum, pos: row1(boardC, "2", "1", "6?", "3:34"), elim: []Cand{{8, 3}, {8, 4}}},
		{tech: NakedPair, pos: row1(boardC, "2:12", "1:12", "6?", "3?"), elim: []Cand{{8, 1}, {8, 2}}},
		{tech: HiddenPair, pos: row1(boardC, "2:123", "1:126", "6:3456", "3:3456"), elim: []Cand{{6, 3}, {7, 6}}},
		{tech: NakedTriple, pos: row1(boardC, "2:12", "1:16", "6:26", "3?"), elim: []Cand{{9, 6}}},
		{tech: HiddenTriple, pos: row1(boardD, "1:125", "2:236", "3:134", "4:4567", "7:4567"), elim: []Cand{{7, 5}, {8, 6}, {9, 4}}},
		// Rows 3 and 4 filled, so each column has two open cells; in row 1,
		// 1 + 2 + 4 + 5 passes the combination test but can't be placed.
		{tech: SumArithmetic, pos: `
#####  23\   19\   13\   27\
  \12    2?    1?    6?    3?
  \23    9?    4?    2?    8?
  \21    5     8     1     7
  \26    7     6     4     9`, elim: []Cand{{7, 2}, {7, 3}, {7, 4}}},
	}
	for _, tc := range cases {
		ps := parsePosition(t, tc.pos)
		s := ps.state(t)
		var m move
		if !finders[tc.tech](s, &m) || m.tech != tc.tech {
			t.Errorf("%s: not found", tc.tech)
			continue
		}
		st := describe(s, &m)
		checkWording(t, st)
		if (st.Placement == nil) != (tc.place == nil) || st.Placement != nil && *st.Placement != *tc.place {
			t.Errorf("%s: placement %v, want %v", tc.tech, st.Placement, tc.place)
		}
		if !slices.Equal(st.Eliminations, tc.elim) {
			t.Errorf("%s: eliminations %v, want %v", tc.tech, st.Eliminations, tc.elim)
		}
		if pl := st.Placement; pl != nil && int(ps.p.Solution[pl.Cell]) != pl.Digit {
			t.Errorf("%s: places a wrong digit", tc.tech)
		}
		for _, e := range st.Eliminations {
			if int(ps.p.Solution[e.Cell]) == e.Digit {
				t.Errorf("%s: eliminates the solution at %s", tc.tech, cellPhrase(ps.p.N, e.Cell))
			}
		}
		if tc.text != "" && st.Text != tc.text {
			t.Errorf("%s text:\n got  %q\n want %q", tc.tech, st.Text, tc.text)
		}
	}
}

// Techniques must be sound for any clues, not just unique ones: a digit
// that is in some solution is never crossed off, and a placed digit is in
// every solution. Clues from an unclimbed random fill have many solutions,
// the fill among them, so the logic gets stuck early but must never
// contradict the fill. Climbed fills are logic-solvable, so they must also
// be unique, with the logic ending on the fill.
func TestLogicIsSound(t *testing.T) {
	for l := Easy; l <= Expert; l++ {
		g := &gen{r: newRNG(7, l), sp: &specs[l]}
		for i := 0; i < 24; i++ {
			b := g.layout()
			if !g.fill(b) {
				continue
			}
			g.setSums(b)
			climbed := i%3 == 0 && g.climb(b, upTo(lastTechnique), 0)
			var s state
			s.init(b, nil)
			var m move
			for s.empty > 0 && s.nextIn(&m, upTo(lastTechnique)) {
				if m.place >= 0 && g.sol[m.place] != m.digit {
					t.Fatalf("%s: %s places %d at %s, the fill has %d", l, m.tech, m.digit, cellPhrase(b.n, int(m.place)), g.sol[m.place])
				}
				if m.place < 0 {
					rn := &b.runs[m.run]
					for i, e := range m.elim[:rn.n] {
						if c := rn.cells[i]; e&bit(g.sol[c]) != 0 {
							t.Fatalf("%s: %s crosses %d off %s, the fill's digit", l, m.tech, g.sol[c], cellPhrase(b.n, int(c)))
						}
					}
				}
				s.apply(&m)
			}
			if s.broken {
				t.Fatalf("%s: logic broke a solvable puzzle", l)
			}
			if climbed {
				if n, _ := countSolutions(b, 2); n != 1 || s.empty != 0 || s.val != g.sol {
					t.Fatalf("%s: logic-solvable puzzle with %d solutions", l, n)
				}
			}
		}
	}
}
