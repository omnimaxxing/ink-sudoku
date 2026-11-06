package engine

import "testing"

func mustParse(t *testing.T, s string) Grid {
	t.Helper()
	g, err := ParseGrid(s)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

func TestSolveHardPuzzles(t *testing.T) {
	for name, s := range map[string]string{
		// "AI Escargot", Arto Inkala (2006).
		"escargot": "1....7.9..3..2...8..96..5....53..9...1..8...26....4...3......1..4......7..7...3..",
		// 17 clues, built to defeat naive backtracking; Wikipedia,
		// "Sudoku solving algorithms", brute-force section.
		"anti-brute-force": "..............3.85..1.2.......5.7.....4...1...9.......5......73..2.1........4...9",
	} {
		g := mustParse(t, s)
		if n := CountSolutions(g, 2); n != 1 {
			t.Errorf("%s: %d solutions, want 1", name, n)
		}
		sol, ok := Solve(g)
		if !ok || sol.Count() != 81 || !sol.Valid() {
			t.Fatalf("%s: bad solution %v %s", name, ok, sol)
		}
		for i, v := range g {
			if v != 0 && sol[i] != v {
				t.Fatalf("%s: solution overwrites given at %s", name, CellName(i))
			}
		}
	}
}

func TestSolveRejectsBadGrids(t *testing.T) {
	var g Grid
	g[0], g[1] = 5, 5
	if _, ok := Solve(g); ok || CountSolutions(g, 2) != 0 {
		t.Error("conflicting grid solved")
	}
	if n := CountSolutions(Grid{}, 2); n != 2 {
		t.Errorf("empty grid: %d solutions, want limit 2", n)
	}
}
