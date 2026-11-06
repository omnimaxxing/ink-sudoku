package engine

import "testing"

// Textbook puzzles from Andrew Stuart's SudokuWiki strategy pages, each
// built to show one technique. Strings are the pages' "From the Start"
// grids (or the linked exemplar).
var textbook = []struct {
	name string
	src  string
	grid string
	tech Technique
}{
	{"x-wing", "https://www.sudokuwiki.org/X_Wing_Strategy (example 1)",
		"100000569402000008050009040000640801000010000208035000040500010900000402621000005", XWing},
	{"naked pair", "https://www.sudokuwiki.org/Naked_Candidates (naked pairs, figure 2)",
		"080090030030000000002060108020800500800907006004005070503040900000000010010050020", NakedPair},
	{"swordfish", "https://www.sudokuwiki.org/Sword_Fish_Strategy (perfect 3-3-3 swordfish)",
		"500010003006003002003200000002300076000050000190007500000009400200800600900040005", Swordfish},
	// SudokuWiki calls the XY-wing a "Y-wing".
	{"xy-wing", "https://www.sudokuwiki.org/Y_Wing_Strategy (exemplar 3)",
		"009765000600400007070000500090024000500000003000910070005000090100007004000156800", XYWing},
}

func TestGradeTextbook(t *testing.T) {
	for _, tc := range textbook {
		g := mustParse(t, tc.grid)
		if CountSolutions(g, 2) != 1 {
			t.Fatalf("%s: fixture is not unique", tc.name)
		}
		r := Grade(g)
		if !r.Solved || r.Hardest != tc.tech {
			t.Errorf("%s: solved=%v hardest=%s, want %s", tc.name, r.Solved, r.Hardest, tc.tech)
		}
		// Required means: stuck without it, finished with it.
		if below, _ := rate(g, tc.tech-1, -1); below.Solved {
			t.Errorf("%s: solved without %s", tc.name, tc.tech)
		}
		if at, _ := rate(g, tc.tech, -1); !at.Solved {
			t.Errorf("%s: not solved with techniques up to %s", tc.name, tc.tech)
		}
	}
}

func TestGradeBeyond(t *testing.T) {
	// AI Escargot needs chains; the grader must stop, not guess.
	r := Grade(mustParse(t, "1....7.9..3..2...8..96..5....53..9...1..8...26....4...3......1..4......7..7...3.."))
	if r.Solved || r.Level != Beyond {
		t.Errorf("escargot: solved=%v level=%s, want Beyond", r.Solved, r.Level)
	}
}

// Every puzzle the logic finishes must be unique, and the logic's answer
// must be the real one. carve keeps only logic-solved puzzles and has no
// uniqueness gate, so this exercises every finder's soundness.
func TestLogicIsSound(t *testing.T) {
	r := newRNG(7, Expert)
	for i := 0; i < 150; i++ {
		sol := randomSolution(r)
		g := carve(sol, r, Expert, lastTechnique)
		if CountSolutions(g, 2) != 1 {
			t.Fatalf("logic-solvable puzzle is not unique: %s", g)
		}
		if _, final := rate(g, lastTechnique, -1); final != sol {
			t.Fatalf("logic reached a wrong grid for %s", g)
		}
	}
}
