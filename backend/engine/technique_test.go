package engine

import (
	"fmt"
	"strings"
	"testing"
)

// HoDoKu's worked examples, as the import strings on their example pages:
// ":nnnn:digits:grid:deleted candidates:eliminations:". In the grid a '+'
// marks a digit placed while solving, and candidates are written digit,
// row, column ("414" is the 4 in row 1, column 4). The grid's candidates
// (minus the deleted ones) are the position the technique is shown in, and
// the eliminations are the ones the page publishes for it.
var hodokuExamples = []struct {
	name, src string
	tech      Technique
	pm        string
}{
	{"naked quad in a row", "https://hodoku.sourceforge.net/en/show_example.php?file=n401&tech=Naked+Quadruple", NakedQuad,
		":0202:389:.+1.+7+2.+56+3.+5+6.3.+247+7325+4+6+1+8+96+9+3+2+8" + "7+4+152+47+61+59+38+581+3+94........2...........1..587....::" + "387 887 988:"},
	{"naked quad in a box", "https://hodoku.sourceforge.net/en/show_example.php?file=n402&tech=Naked+Quadruple", NakedQuad,
		":0202:469:+53+2+78+6...9+78+241.+6...19+5+32+87.254..6+7...3+" + "617.5+27..+5........+1........+8.51.6...3...98::471 472 481 " + "491 492 671 672 691 692 972:"},
	{"hidden quad in a box", "https://hodoku.sourceforge.net/en/show_example.php?file=h401&tech=Hidden+Quadruple", HiddenQuad,
		":0212:36:8+1+657+329+439+2......+4+5+72.+9..+6+9+41...5+68+7" + "+8+5496+1+2+3+6+2+38...+4.2+79.....1+1+38....7.56+4....82:76" + "6 377 987:375 675 685:"},
	{"hidden quad in a column", "https://hodoku.sourceforge.net/en/show_example.php?file=h402&tech=Hidden+Quadruple", HiddenQuad,
		":0212:3678:.3.....1...8.9....4..6.8......+57694....+9+8+352." + "...124...2+76..+519....7.+9....95...47.:121 151 361 861 485 " + "299:329 339 389 619 629 689 719 729 739 819 889:"},
	{"skyscraper in columns", "https://hodoku.sourceforge.net/en/show_example.php?file=sk01&tech=Skyscraper", Skyscraper,
		":0400:1:+6+9+7.....+2..19+72.6+3..+3..679.9+12...6.+737+4+2+" + "6.95.+8+65+7.+9.+2414+8+6+93+2+757.9.24..+6..+68.+7..+9::11" + "7 118 134 135:"},
	{"skyscraper in rows", "https://hodoku.sourceforge.net/en/show_example.php?file=sk02&tech=Skyscraper", Skyscraper,
		":0400:4:..1.+2+8+7+59.8+79.+5+132+9+52173+4+8+6.2." + "+7..+3+4....+5..27.+7148+3+26+9+5....9.+8+1+7.+7+8.5" + "+1+96319..+87+5+2+4::414:"},
	{"two-string kite", "https://hodoku.sourceforge.net/en/show_example.php?file=2sk01&tech=2-String+Kite", TwoStringKite,
		":0401:5:.81.2.+6...+4+2.+6..+89.+568..+24.+6+931+4+27+5+8+4+" + "28+357916+1+7+5+6+8+9+3+245+1..+36+89223...84+6.+8+6.+2....." + "::524:"},
	{"two-string kite 2", "https://hodoku.sourceforge.net/en/show_example.php?file=2sk02&tech=2-String+Kite", TwoStringKite,
		":0401:9:+3617..+2+95842+395+6+7+1.5.+26+14+8+3+1.+8+5+2+6.3" + "+4+625....+1+8.+341..+526+4..+6+1.+8+5+2+58...+2+1672+1+6+8+5" + "+7349::976:"},
	{"xyz-wing", "https://hodoku.sourceforge.net/en/show_example.php?file=xyz01&tech=XYZ-Wing", XYZWing,
		":0801:7:+869+4+5+3+7+2+1...+921+5+6+8+2+1+58..4+3+9+6+2153+4" + "+98+7+4.76+1.+35+2...+2..1+4+6...+1.+2+8.39+32+78+5+61+4+1.." + "34.2.5::792:"},
	{"xyz-wing 2", "https://hodoku.sourceforge.net/en/show_example.php?file=xyz02&tech=XYZ-Wing", XYZWing,
		":0801:6:+673.+8.+2.9514.92..+3+9+82.....+5+167+359..28.+9.1." + "53+6..+5+8..+7+9+1..+614.9+2+8..+1..835+4..+8...1" + "+67::635 636:"},
	{"w-wing", "https://hodoku.sourceforge.net/en/show_example.php?file=w01&tech=W-Wing", WWing,
		":0803:5:+9251+3+4+6+8+7+8..+6+59+43+24+3+67+289+5+1+64..+1.8" + "..15.4+8..+6.+3.+8.6..+14+5..+2+7+6+3+48263+84+1...+78+439+5" + "1+26::549:"},
	{"w-wing 2", "https://hodoku.sourceforge.net/en/show_example.php?file=w02&tech=W-Wing", WWing,
		":0803:4:6..+9+5..7...+9.2.....58.+31...+1+64+3+8+9+7+52...1+" + "7+59+46597+24+6..892+54+1+76+8+3...5+6+2.....68+93...::417 4" + "27 437 489 499:"},
	{"colour trap", "https://hodoku.sourceforge.net/en/show_example.php?file=sc01&tech=Simple+Colors", SimpleColouring,
		":0500:3:21+4..+6.....+79.+2..4...4.7.....+1+87..32..+26+9..." + "..48.21..642.+7.986+1..91+68...1+86+2+4...9:937 938 387 587 " + "787 588 788 789:319 339 381:"},
	{"colour wrap", "https://hodoku.sourceforge.net/en/show_example.php?file=sc02&tech=Simple+Colors", SimpleColouring,
		":0500:8:+6+5+9...+1+3...+1.3.6+252.+31+65.4+9.+2..9+63+1.+36" + ".7..5+9.+91.+3.4.+6279.6..+2+5+35.+6...9811.2...+4+7+6:814 " + "815 424 826 443 843 849 855 856 863 875 285 896:819 821 824" + " 832 844 853 867 876 892 895:"},
}

// The first hint in some of these positions, word for word: a snapshot of
// the wording for each new technique. Update deliberately.
var textbookHints = map[string][2]string{
	"naked quad in a row": {
		"These four cells share 3, 4, 8 and 9",
		"Row 8, columns 1, 3, 4 and 6 can only hold 3, 4, 8 and 9 between them. So those four numbers go there. Cross 3, 4, 8 and 9 off the rest of row 8: columns 7 and 8.",
	},
	"hidden quad in a box": {
		"2, 4, 5 and 8 only fit in four cells",
		"In box 8, the 2, 4, 5 and 8 can only go in row 7, columns 5 and 6 and row 8, columns 5 and 6. So those four cells hold 2, 4, 5 and 8. Cross every other number off them.",
	},
	"skyscraper in rows": {
		"The 4s in rows 2 and 8 almost line up",
		"In row 2, the 4 can only go in columns 1 and 5. In row 8, it can only go in columns 1 and 4. Column 1 can't have 4s in both rows 2 and 8, so row 2 or row 8 uses its other spot: row 2, column 5 or row 8, column 4. Cross 4 off row 1, column 4, which shares a row, column or box with both.",
	},
	"two-string kite": {
		"The 5s in row 8 and column 7 meet in box 9",
		"In row 8, the 5 can only go in columns 4 and 9. In column 7, it can only go in rows 2 and 9. Box 9 can't have 5s at both row 8, column 9 and row 9, column 7, so row 8 or column 7 uses its other spot: row 8, column 4 or row 2, column 7. Cross 5 off row 2, column 4, which shares a row, column or box with both.",
	},
	"xyz-wing 2": {
		"One of three cells must be a 6",
		"Row 3, column 4 is 4, 6 or 7. If it's 4, row 3, column 7 is a 6; if it's 7, row 2, column 4 is a 6; and if it's 6, it's the 6 itself. Either way, one of these three cells is a 6. Cross 6 off row 3, columns 5 and 6, which share a row, column or box with all three.",
	},
	"w-wing": {
		"Two 5-or-9 cells, linked by the 9s",
		"Row 4, column 4 and row 8, column 9 can each only be 5 or 9. In column 8, the 9 can only go in rows 4 and 8, and each of those spots shares a row, column or box with one of the two cells. If neither cell were a 5, both would be 9s, and column 8 would have no spot left for its 9. So one of them is a 5. Cross 5 off row 4, column 9, which shares a row, column or box with both.",
	},
	"colour trap": {
		"Follow the chain of 3s",
		"The 3 has only two spots in column 4 (rows 1 and 6), in box 5 (row 6, column 4 and row 5, column 6), in column 6 (rows 5 and 9), in row 9 (columns 6 and 7) and in box 9 (row 9, column 7 and row 8, column 9). Those pairs link up into a chain, and each pair holds one 3, so every other spot along it is a 3. One of its two ends is a 3: row 1, column 4 or row 8, column 9. Cross 3 off row 1, column 9, which shares a row, column or box with both.",
	},
	"colour wrap": {
		"Follow the chain of 8s",
		"The 8 has only two spots in row 7 (columns 6 and 3), in column 3 (rows 7 and 5), in row 5 (columns 3 and 9), in box 6 (row 5, column 9 and row 6, column 7), in row 6 (columns 7 and 5) and in column 5 (rows 6 and 9). Those pairs link up into a chain, and each pair holds one 8, so every other spot along it is an 8. If row 7, column 6 were an 8, row 9, column 5 would be one too, but they share box 8. So the 8s are in the other spots. Cross 8 off row 5, column 3; row 6, column 7; row 7, column 6; and row 9, column 5.",
	},
}

// hodokuState parses a HoDoKu import string into its givens, the digits
// placed since, the candidates and the published eliminations.
func hodokuState(t *testing.T, pm string) (givens, placed Grid, cand [81]uint16, elims []Cand) {
	t.Helper()
	f := strings.Split(pm, ":")
	if len(f) != 7 {
		t.Fatalf("want 7 fields, got %d", len(f))
	}
	n, plus := 0, false
	for _, r := range f[3] {
		switch {
		case r == '+':
			plus = true
			continue
		case n == 81:
			t.Fatal("grid has more than 81 cells")
		case r >= '1' && r <= '9' && plus:
			placed[n] = uint8(r - '0')
		case r >= '1' && r <= '9':
			givens[n] = uint8(r - '0')
		case r != '.':
			t.Fatalf("bad grid character %q", r)
		}
		n, plus = n+1, false
	}
	if n != 81 {
		t.Fatalf("grid has %d cells", n)
	}
	board := givens
	for c, v := range placed {
		if v != 0 {
			board[c] = v
		}
	}
	cand = AutoCandidates(board)
	for _, x := range candList(t, f[4]) {
		cand[x.Cell] &^= CandidateBit(x.Digit)
	}
	return givens, placed, cand, candList(t, f[5])
}

func candList(t *testing.T, s string) []Cand {
	var out []Cand
	for _, w := range strings.Fields(s) {
		if len(w) != 3 {
			t.Fatalf("bad candidate %q", w)
		}
		out = append(out, Cand{Cell: int(w[1]-'1')*9 + int(w[2]-'1'), Digit: w[0] - '0'})
	}
	return out
}

// checkMove fails if m doesn't make progress on s or contradicts sol.
func checkMove(t *testing.T, s *state, m *move, sol Grid) {
	t.Helper()
	if m.place >= 0 {
		if sol[m.place] != m.digit {
			t.Fatalf("%s places %d at %s, solution has %d", m.tech, m.digit, CellName(int(m.place)), sol[m.place])
		}
		return
	}
	any := false
	for c, e := range m.elim {
		if e&CandidateBit(sol[c]) != 0 && s.val[c] == 0 {
			t.Fatalf("%s crosses the solution's %d off %s", m.tech, sol[c], CellName(c))
		}
		if e&^s.cand[c] != 0 {
			t.Fatalf("%s crosses off a candidate %s doesn't have", m.tech, CellName(c))
		}
		any = any || e != 0
	}
	if !any {
		t.Fatalf("%s makes no progress", m.tech)
	}
}

// Each technique finds its textbook example in the exact position the
// page shows, and among the eliminations it makes there (applying only
// itself until it runs dry) are the published ones. A colour wrap here
// clears only the chain it explains, so its first step must be a subset of
// HoDoKu's, which clears the whole colour.
func TestTechniqueTextbookPositions(t *testing.T) {
	for _, tc := range hodokuExamples {
		t.Run(tc.name, func(t *testing.T) {
			givens, placed, cand, published := hodokuState(t, tc.pm)
			board := givens
			for c, v := range placed {
				if v != 0 {
					board[c] = v
				}
			}
			// Guard the transcription: the position must be consistent and
			// the givens must have one solution that it agrees with.
			sol, ok := uniqueSolution(givens)
			if !ok {
				t.Fatalf("givens don't have one solution (%s)", tc.src)
			}
			for c := range board {
				if board[c] != 0 && board[c] != sol[c] {
					t.Fatalf("position disagrees with the solution at %s", CellName(c))
				}
				if board[c] == 0 && cand[c]&CandidateBit(sol[c]) == 0 {
					t.Fatalf("deleted candidates rule out the solution at %s", CellName(c))
				}
			}

			s := state{val: board, cand: cand}
			for _, v := range board {
				if v == 0 {
					s.empty++
				}
			}
			got := map[Cand]bool{}
			var first []Cand
			for i := 0; ; i++ {
				var m move
				if !finders[tc.tech](&s, &m) {
					if i == 0 {
						t.Fatalf("%s not found (%s)", tc.tech, tc.src)
					}
					break
				}
				checkMove(t, &s, &m, sol)
				st := describe(&s, &m)
				checkWording(t, st)
				if i == 0 {
					first = st.Eliminations
					if want, ok := textbookHints[tc.name]; ok && (st.Title != want[0] || st.Text != want[1]) {
						t.Errorf("wording:\n got  %q\n      %q\n want %q\n      %q", st.Title, st.Text, want[0], want[1])
					}
				}
				for _, e := range st.Eliminations {
					got[e] = true
				}
				s.apply(&m)
			}
			if tc.name == "colour wrap" {
				want := map[Cand]bool{}
				for _, e := range published {
					want[e] = true
				}
				for _, e := range first {
					if !want[e] {
						t.Errorf("wrap crosses %d off %s; HoDoKu doesn't", e.Digit, CellName(e.Cell))
					}
				}
				return
			}
			for _, e := range published {
				if !got[e] {
					t.Errorf("missed the published elimination of %d at %s", e.Digit, CellName(e.Cell))
				}
			}
		})
	}
}

// SudokuWiki's examples, given as the puzzles they start from. Solving
// with the whole ladder (or, if an easier technique always gets there
// first, with just the Hard techniques and this one), the technique must
// come up, nothing may contradict the solution, and the technique must make
// the published eliminations listed (where the page names them and our
// order leaves them to it).
var sudokuwikiExamples = []struct {
	name, src string
	tech      Technique
	grid      string
	want      []Cand // published eliminations, if the page names them
}{
	{"naked quad", "https://www.sudokuwiki.org/Naked_Candidates (naked quads, second example)", NakedQuad,
		"600000001430070000020008070080050002100000006900060030000700050000090068300000027", nil},
	{"hidden quad", "https://www.sudokuwiki.org/Hidden_Candidates (hidden quads: {1,4,6,9} in box 5)", HiddenQuad,
		"000500000425090001800010020500000000019000460000000002090040003200060807000001600", nil},
	{"xyz-wing", "https://www.sudokuwiki.org/XYZ_Wing (example 1: 1 off F7)", XYZWing,
		"090001700500200008000030200070004960200060005069700030008090000700003009003800040", []Cand{{5*9 + 6, 1}}},
	{"xyz-wing 2", "https://www.sudokuwiki.org/XYZ_Wing (example 2: 6 off E9)", XYZWing,
		"600000000500900007820001030340209080000080000080307025050400092900005004000000003", []Cand{{4*9 + 8, 6}}},
	{"w-wing", "https://www.sudokuwiki.org/W_Wing_Strategy (example 1: 3 off D6 and E6)", WWing,
		"000020007000507800057090030008900010700040008020001900090050740002109000600080000", []Cand{{3*9 + 5, 3}, {4*9 + 5, 3}}},
	{"colour wrap", "https://www.sudokuwiki.org/Simple_Colouring (rule 2, twice in a unit)", SimpleColouring,
		"000000030002090500080706004900054006030000070600380009300601020007020600060000000", nil},
	// The page's colouring also takes the 7 off J3; here claiming gets it first.
	{"colour trap", "https://www.sudokuwiki.org/Simple_Colouring (rule 4: 7 off J3 and H5)", SimpleColouring,
		"000000050050409000008060400860010500001040700009050083006090300000108090040000000", []Cand{{7*9 + 4, 7}}},
}

func TestTechniqueTextbookPuzzles(t *testing.T) {
	for _, tc := range sudokuwikiExamples {
		t.Run(tc.name, func(t *testing.T) {
			g := mustParse(t, tc.grid)
			sol, ok := uniqueSolution(g)
			if !ok {
				t.Fatalf("fixture is not unique (%s)", tc.src)
			}
			got := map[Cand]bool{}
			used := 0
			for _, set := range []techSet{upTo(lastTechnique), upTo(HiddenQuad) | 1<<tc.tech} {
				s, _ := newState(g)
				var m move
				for s.empty > 0 && s.nextIn(&m, set) {
					checkMove(t, &s, &m, sol)
					if m.tech == tc.tech {
						used++
						st := describe(&s, &m)
						checkWording(t, st)
						for _, e := range st.Eliminations {
							got[e] = true
						}
					}
					s.apply(&m)
				}
				if used > 0 {
					break
				}
			}
			if used == 0 {
				t.Fatalf("%s never used (%s)", tc.tech, tc.src)
			}
			for _, e := range tc.want {
				if !got[e] {
					t.Errorf("missed the published elimination of %d at %s", e.Digit, CellName(e.Cell))
				}
			}
		})
	}
}

// Every finder must be sound on its own, not only when it's the easiest
// move: on every position along the solves of many logic-carved puzzles,
// each finder's move must keep the solution and its hint must read well.
// Half the runs also cross off random wrong candidates as they go, the way
// a player's notes can run ahead of the logic, which makes far more
// chains, wings and subsets to find.
func TestFindersAreSound(t *testing.T) {
	r := newRNG(11, Expert)
	fired := [NumTechniques]int{}
	for i := 0; i < 60; i++ {
		sol := randomSolution(r)
		g := carve(sol, r, Expert, lastTechnique)
		s, _ := newState(g)
		var m move
		for s.empty > 0 {
			for tt := HiddenSingle; int(tt) < NumTechniques; tt++ {
				probe := s
				if finders[tt](&probe, &m) {
					fired[tt]++
					if m.tech != tt {
						t.Fatalf("%s finder made a %s move", tt, m.tech)
					}
					checkMove(t, &probe, &m, sol)
					checkWording(t, describe(&probe, &m))
				}
			}
			if !s.next(&m, lastTechnique) {
				t.Fatalf("logic stuck on its own carve %s", g)
			}
			s.apply(&m)
			if i%2 == 1 {
				for c, k := range s.cand {
					if extra := k &^ CandidateBit(sol[c]); extra != 0 && r.intn(4) == 0 {
						s.cand[c] &^= extra & -extra
					}
				}
			}
		}
		if s.val != sol {
			t.Fatalf("logic reached a wrong grid for %s", g)
		}
	}
	for tt, n := range fired {
		if n == 0 {
			t.Errorf("%s never fired; the test doesn't exercise it", Technique(tt))
		}
	}
}

// When the next step would be one of the new techniques, notes that rule
// out a pattern cell's answer must still come back as a mistake about that
// cell, never as a deduction built on the bad note.
func TestNextStepNotesMistakeOnNewTechniques(t *testing.T) {
	for _, tc := range hodokuExamples {
		givens, placed, cand, _ := hodokuState(t, tc.pm)
		sol, _ := uniqueSolution(givens)
		st, ok := NextStep(givens, placed, cand)
		if !ok || st.Technique >= Mistake {
			t.Fatalf("%s: got %s %q", tc.name, st.Technique, st.Text)
		}
		checkWording(t, st)
		for _, e := range st.Eliminations {
			if sol[e.Cell] == e.Digit {
				t.Fatalf("%s: %s %q crosses off the answer", tc.name, st.Technique, st.Text)
			}
		}

		board := givens
		for c, v := range placed {
			if v != 0 {
				board[c] = v
			}
		}
		s := state{val: board, cand: cand}
		var m move
		finders[tc.tech](&s, &m)
		c := int(m.cells[0])
		bad := cand
		bad[c] = cand[c] &^ CandidateBit(sol[c])
		st, ok = NextStep(givens, placed, bad)
		if !ok || st.Technique != Mistake || fmt.Sprint(st.Cells) != fmt.Sprint([]int{c}) ||
			st.Title != "Your notes rule out the answer here" {
			t.Errorf("%s: bad note at %s gave %s %v %q", tc.name, CellName(c), st.Technique, st.Cells, st.Title)
		}
		checkWording(t, st)
	}
}

// carve's quick solvability check must agree with rate on every puzzle it
// sees, or carving would change what it keeps.
func TestSolvesMatchesRate(t *testing.T) {
	r := newRNG(3, Hard)
	sv := new(solver)
	for i := 0; i < 300; i++ {
		sol := randomSolution(r)
		var cells [81]uint8
		for c := range cells {
			cells[c] = uint8(c)
		}
		r.shuffle(cells[:])
		g := sol
		for _, c := range cells[:50+r.intn(15)] {
			g[c] = 0
		}
		for _, ceiling := range []Technique{NakedSingle, Claiming, HiddenQuad, Swordfish, TwoStringKite, lastTechnique} {
			rt, _ := rate(g, ceiling, -1)
			if got := sv.solves(g, upTo(ceiling)); got != rt.Solved {
				t.Fatalf("up to %s: solves=%v, rate solved=%v for %s", ceiling, got, rt.Solved, g)
			}
		}
	}
}
