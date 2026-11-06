package kakuro

import (
	"encoding/json"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// shortName catches "r3c5" style cell names; hints say "row 3, column 5".
var shortName = regexp.MustCompile(`r\d+c\d+`)

func checkWording(t *testing.T, st Step) {
	t.Helper()
	if st.Title == "" || st.Text == "" || shortName.MatchString(st.Title+" "+st.Text) {
		t.Fatalf("%s: bad wording: title %q, text %q", st.Technique, st.Title, st.Text)
	}
}

// Following hints from an empty board, keeping their eliminations as
// notes, must reach the solution without ever contradicting it, and retrace
// the grader's path step for step.
func TestNextStepWalksToSolution(t *testing.T) {
	for _, tc := range []struct {
		seed  uint64
		level Level
	}{{100, Easy}, {101, Medium}, {102, Hard}, {103, Expert}, {104, Expert}} {
		p := Generate(tc.seed, tc.level)
		r := Grade(p)
		hinted := map[string]bool{}
		entries := make([]uint8, len(p.Cells))
		cands := AutoCandidates(p, nil)
		steps := 0
		for {
			st, ok := NextStep(p, entries, cands)
			if !ok {
				break
			}
			steps++
			if st.Technique == "Mistake" || st.Technique == "Reveal" {
				t.Fatalf("%s step %d: unexpected %s %q", tc.level, steps, st.Technique, st.Text)
			}
			checkWording(t, st)
			hinted[st.Technique] = true
			if pl := st.Placement; pl != nil {
				if int(p.Solution[pl.Cell]) != pl.Digit {
					t.Fatalf("%s: %q places a wrong digit", tc.level, st.Text)
				}
				entries[pl.Cell] = uint8(pl.Digit)
				auto := AutoCandidates(p, entries)
				for c := range cands {
					cands[c] &= auto[c]
				}
			}
			if st.Placement == nil && len(st.Eliminations) == 0 {
				t.Fatalf("%s: %q makes no progress", tc.level, st.Text)
			}
			for _, e := range st.Eliminations {
				if int(p.Solution[e.Cell]) == e.Digit {
					t.Fatalf("%s: %q eliminates the solution digit at %s", tc.level, st.Text, cellPhrase(p.N, e.Cell))
				}
				cands[e.Cell] &^= bit(uint8(e.Digit))
			}
			if steps > 1000 {
				t.Fatalf("%s: hints don't terminate", tc.level)
			}
		}
		if string(entries) != string(p.Solution) {
			t.Fatalf("%s: hints stopped before the solution", tc.level)
		}
		if steps != r.Steps {
			t.Errorf("%s: %d hint steps, grader took %d", tc.level, steps, r.Steps)
		}
		if !hinted[p.Hardest] {
			t.Errorf("%s seed %d: no hint used %s, the hardest step", tc.level, tc.seed, p.Hardest)
		}
	}
}

func TestNextStepFlagsMistakes(t *testing.T) {
	p := Generate(5, Medium)
	c := 0
	for p.Cells[c].Black {
		c++
	}
	wrong := p.Solution[c]%9 + 1

	entries := make([]uint8, len(p.Cells))
	entries[c] = wrong
	st, ok := NextStep(p, entries, nil)
	if !ok || st.Technique != "Mistake" || len(st.Cells) != 1 || st.Cells[0] != c {
		t.Errorf("wrong entry: got %s %v %q", st.Technique, st.Cells, st.Text)
	}
	checkWording(t, st)

	notes := make([]uint16, len(p.Cells))
	notes[c] = bit(wrong)
	st, ok = NextStep(p, nil, notes)
	if !ok || st.Technique != "Mistake" || len(st.Cells) != 1 || st.Cells[0] != c {
		t.Errorf("bad notes: got %s %v %q", st.Technique, st.Cells, st.Text)
	}
	checkWording(t, st)

	if _, ok := NextStep(p, p.Solution, nil); ok {
		t.Error("solved board still gets a hint")
	}
	// Without a stored solution the engine solves the clues itself.
	p.Solution = nil
	if st, ok := NextStep(p, entries, nil); !ok || st.Technique != "Mistake" {
		t.Errorf("no stored solution: got %s %v", st.Technique, ok)
	}
}

// Snapshot of the mistake and reveal wording; update deliberately.
func TestMistakeWording(t *testing.T) {
	p := parsePosition(t, tiny1).p // solution 1 2 / 3 1
	for _, tc := range []struct {
		entries     map[int]uint8
		title, text string
	}{
		{map[int]uint8{4: 2, 5: 2}, "Two 2s in one run",
			"Row 1, column 1 can't be a 2. The across run in row 1, columns 1 and 2, already has one in column 2."},
		{map[int]uint8{4: 1, 5: 5}, "This run doesn't add up",
			"The across run in row 1, columns 1 and 2, adds up to 6, but its clue is 3."},
		{map[int]uint8{5: 5}, "That 5 isn't right",
			"The 5 in row 1, column 2 is wrong. Nothing clashes yet, but it leads to a dead end."},
		{map[int]uint8{5: 5, 7: 4}, "Some numbers are wrong",
			"2 of your numbers are wrong: row 1, column 2 and row 2, column 1."},
	} {
		entries := make([]uint8, len(p.Cells))
		for c, v := range tc.entries {
			entries[c] = v
		}
		st, ok := NextStep(p, entries, nil)
		if !ok || st.Technique != "Mistake" || st.Title != tc.title || st.Text != tc.text {
			t.Errorf("%v:\n got  %q %q\n want %q %q", tc.entries, st.Title, st.Text, tc.title, tc.text)
		}
	}
	b, _ := puzzleBoard(p)
	var s state
	s.init(b, nil)
	if st := reveal(&s, p.Solution); st.Technique != "Reveal" || *st.Placement != (Cand{4, 1}) ||
		st.Text != "None of my tricks work here, so here's a free one: row 1, column 1 is a 1." {
		t.Errorf("reveal: %+v", st)
	}
}

// The UI reads cells and digits as JSON numbers.
func TestStepJSON(t *testing.T) {
	b, err := json.Marshal(Step{
		Technique: "Naked pair", Title: "t", Text: "x",
		Cells: []int{12, 13}, Digits: []int{3, 8},
		Placement:    &Cand{7, 3},
		Eliminations: []Cand{{9, 3}},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	for _, want := range []string{`"technique":"Naked pair"`, `"title":"t"`, `"text":"x"`, `"cells":[12,13]`, `"digits":[3,8]`,
		`"placement":{"cell":7,"digit":3}`, `"eliminations":[{"cell":9,"digit":3}]`} {
		if !strings.Contains(got, want) {
			t.Errorf("%s lacks %s", got, want)
		}
	}
	b, _ = json.Marshal(Step{Technique: "Reveal", Title: "t", Text: "x"})
	if got := string(b); got != `{"technique":"Reveal","title":"t","text":"x"}` {
		t.Errorf("empty fields not omitted: %s", got)
	}
}

func TestCheck(t *testing.T) {
	p := parsePosition(t, tiny1).p // cells 4 5 / 7 8, solution 1 2 / 3 1
	for _, tc := range []struct {
		entries map[int]uint8
		want    []int
	}{
		{map[int]uint8{4: 1, 5: 2, 7: 3}, nil},
		{map[int]uint8{4: 1, 7: 1}, []int{4, 7}},          // repeated in the down run
		{map[int]uint8{4: 1, 5: 5}, []int{4, 5}},          // full run, wrong sum
		{map[int]uint8{4: 2, 5: 2, 8: 2}, []int{4, 5, 8}}, // twice across, twice down
		{map[int]uint8{0: 5, 4: 1}, nil},                  // black cells are ignored
	} {
		entries := make([]uint8, len(p.Cells))
		for c, v := range tc.entries {
			entries[c] = v
		}
		if got := Check(p, entries); !slices.Equal(got, tc.want) {
			t.Errorf("%v: got %v, want %v", tc.entries, got, tc.want)
		}
	}
}

func TestAutoCandidates(t *testing.T) {
	p := parsePosition(t, tiny2).p // 3 across / 12 across, 11 down / 4 down
	got := AutoCandidates(p, nil)
	// Each run's combinations, met cell by cell; nothing is placed yet.
	want := map[int]uint16{4: bit(2), 5: bit(1), 7: comboUnion[2][12], 8: bit(3)}
	for c := range got {
		if got[c] != want[c] {
			t.Errorf("cell %d: %09b, want %09b", c, got[c], want[c])
		}
	}
	p = mustParse(t, wikipedia)
	entries := make([]uint8, len(p.Cells))
	// A 9 in row 1, column 1 leaves 7 for the rest of the 16 across, and
	// 14 without a 9 (6 + 8) for the rest of the 23 down, which meets the
	// 17 across (8 + 9) in an 8.
	entries[9] = 9
	got = AutoCandidates(p, entries)
	if got[9] != 0 || got[10] != bit(7) || got[17] != bit(8) {
		t.Errorf("after an entry: %09b %09b %09b", got[9], got[10], got[17])
	}
}
