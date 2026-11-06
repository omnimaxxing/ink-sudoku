package engine

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

// shortName catches the old "r4c4" cell names; hints say "row 4, column 4".
var shortName = regexp.MustCompile(`r\dc\d`)

func checkWording(t *testing.T, st Step) {
	t.Helper()
	if st.Title == "" || st.Text == "" || shortName.MatchString(st.Title+" "+st.Text) {
		t.Fatalf("%s: bad wording: title %q, text %q", st.Technique, st.Title, st.Text)
	}
}

// Following hints from an empty board must reach the solution, never
// contradict it, and retrace the grader's path step for step.
func TestNextStepWalksToSolution(t *testing.T) {
	cases := []struct {
		seed  uint64
		level Level
	}{
		{100, Easy}, {101, Medium}, {102, Hard}, {103, Expert},
		// An Expert puzzle from each family: X-wing, simple colouring,
		// two-string kite, W-wing.
		{9, Expert}, {2, Expert}, {4, Expert}, {6, Expert},
	}
	for _, tc := range cases {
		level := tc.level
		p := Generate(tc.seed, level)
		hinted := map[Technique]bool{}
		var entries Grid
		cands := AutoCandidates(p.Givens)
		steps := 0
		for {
			st, ok := NextStep(p.Givens, entries, cands)
			if !ok {
				break
			}
			steps++
			if st.Technique >= Mistake {
				t.Fatalf("%s step %d: unexpected %s %q", level, steps, st.Technique, st.Text)
			}
			checkWording(t, st)
			hinted[st.Technique] = true
			if pl := st.Placement; pl != nil {
				if p.Solution[pl.Cell] != pl.Digit {
					t.Fatalf("%s: %q places a wrong digit", level, st.Text)
				}
				entries[pl.Cell] = pl.Digit
				cands[pl.Cell] = 0
				for _, q := range peers[pl.Cell] {
					cands[q] &^= CandidateBit(pl.Digit)
				}
			}
			if st.Placement == nil && len(st.Eliminations) == 0 {
				t.Fatalf("%s: %q makes no progress", level, st.Text)
			}
			for _, e := range st.Eliminations {
				if p.Solution[e.Cell] == e.Digit {
					t.Fatalf("%s: %q eliminates the solution digit at %s", level, st.Text, CellName(e.Cell))
				}
				cands[e.Cell] &^= CandidateBit(e.Digit)
			}
			if steps > 400 {
				t.Fatalf("%s: hints don't terminate", level)
			}
		}
		for i := range entries {
			if p.Givens[i] != 0 {
				entries[i] = p.Givens[i]
			}
		}
		if entries != p.Solution {
			t.Fatalf("%s: hints stopped before the solution", level)
		}
		if steps != p.Grade.Steps {
			t.Errorf("%s: %d hint steps, grader took %d", level, steps, p.Grade.Steps)
		}
		if !hinted[p.Grade.Hardest] {
			t.Errorf("%s seed %d: no hint used %s, the hardest step", level, tc.seed, p.Grade.Hardest)
		}
	}
}

func TestNextStepFlagsMistakes(t *testing.T) {
	p := Generate(5, Medium)
	c := 0
	for p.Givens[c] != 0 {
		c++
	}
	wrong := p.Solution[c]%9 + 1

	var entries Grid
	entries[c] = wrong
	st, ok := NextStep(p.Givens, entries, [81]uint16{})
	if !ok || st.Technique != Mistake || len(st.Cells) != 1 || st.Cells[0] != c {
		t.Errorf("wrong entry: got %s %v %q", st.Technique, st.Cells, st.Text)
	}
	checkWording(t, st)

	var notes [81]uint16
	notes[c] = CandidateBit(wrong)
	st, ok = NextStep(p.Givens, Grid{}, notes)
	if !ok || st.Technique != Mistake || len(st.Cells) != 1 || st.Cells[0] != c {
		t.Errorf("bad notes: got %s %v %q", st.Technique, st.Cells, st.Text)
	}
	checkWording(t, st)

	if _, ok := NextStep(p.Givens, p.Solution, [81]uint16{}); ok {
		t.Error("solved board still gets a hint")
	}
}

// The UI reads houses and digits as JSON numbers; uint8 slices would
// otherwise encode as base64 strings.
func TestStepJSONIsNumeric(t *testing.T) {
	b, err := json.Marshal(Step{Houses: []House{18, 3}, Digits: DigitList{8}, Text: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(b); !strings.Contains(got, `"houses":[18,3]`) || !strings.Contains(got, `"digits":[8]`) {
		t.Fatalf("got %s", got)
	}
}

// Snapshot of the wording for a few hand-built moves; update deliberately.
func TestHintWording(t *testing.T) {
	var s state
	cases := []struct {
		m           move
		cand        map[int]uint16
		title, text string
	}{
		{
			m:     move{tech: HiddenSingle, place: 39, digit: 7, houses: [6]uint8{22}, nh: 1, cells: [9]uint8{39}, nc: 1},
			title: "Only one place for the 7",
			text:  "In box 5, the 7 can only go in row 5, column 4. Every other spot is ruled out.",
		},
		{
			m:     move{tech: Pointing, place: -1, digit: 8, houses: [6]uint8{22, 3}, nh: 2, cells: [9]uint8{30, 32}, nc: 2},
			cand:  map[int]uint16{27: CandidateBit(8), 35: CandidateBit(8)},
			title: "Box 5's 8 must be in row 4",
			text:  "In box 5, the 8 can only go in row 4. So row 4 gets its 8 from box 5. Cross 8 off the rest of row 4: columns 1 and 9.",
		},
		{
			m:     move{tech: NakedPair, place: -1, digits: CandidateBit(3) | CandidateBit(8), houses: [6]uint8{9 + 6}, nh: 1, cells: [9]uint8{6, 60}, nc: 2},
			cand:  map[int]uint16{24: CandidateBit(3), 33: CandidateBit(8) | CandidateBit(1)},
			title: "These two cells share 3 and 8",
			text:  "Column 7, rows 1 and 7 can only be 3 or 8. One is the 3, the other the 8. Cross 3 and 8 off the rest of column 7: rows 3 and 4.",
		},
		{
			m:     move{tech: XYWing, place: -1, digit: 5, digits: CandidateBit(3) | CandidateBit(7) | CandidateBit(5), cells: [9]uint8{10, 16, 46}, nc: 3},
			cand:  map[int]uint16{10: CandidateBit(3) | CandidateBit(7), 16: CandidateBit(3) | CandidateBit(5), 46: CandidateBit(7) | CandidateBit(5), 52: CandidateBit(5)},
			title: "One of two cells must be a 5",
			text: "Row 2, column 2 is 3 or 7. If it's 3, row 2, column 8 is a 5. If it's 7, row 6, column 2 is a 5. " +
				"Either way, one of them is a 5. Cross 5 off row 6, column 8, which shares a row, column or box with both.",
		},
	}
	for _, tc := range cases {
		s.cand = [81]uint16{}
		for c, k := range tc.cand {
			s.cand[c] = k
		}
		m := tc.m
		for c, k := range tc.cand {
			if c != int(m.cells[0]) && c != int(m.cells[1]) && c != int(m.cells[2]) {
				m.elim[c] = k & (m.digits | CandidateBit(m.digit))
			}
		}
		st := describe(&s, &m)
		if st.Title != tc.title || st.Text != tc.text {
			t.Errorf("%s:\n got  %q\n     %q\n want %q\n     %q", m.tech, st.Title, st.Text, tc.title, tc.text)
		}
	}
	s.val = Grid{}
	for c := range s.cand {
		s.cand[c] = allDigits
	}
	s.cand[40] = CandidateBit(3) | CandidateBit(8)
	var sol Grid
	sol[40] = 8
	if st := reveal(&s, sol); st.Title != "Here's one for free" ||
		st.Text != "None of my tricks work here, so here's a free one: row 5, column 5 is an 8." {
		t.Errorf("reveal: %q %q", st.Title, st.Text)
	}
	var board Grid
	board[0], board[8] = 5, 5
	if st := wrongEntries(board, []int{8}); st.Title != "Row 1 already has a 5" ||
		st.Text != "Row 1, column 9 can't be a 5. Row 1 already has one in column 1." {
		t.Errorf("clash: %q %q", st.Title, st.Text)
	}
}
