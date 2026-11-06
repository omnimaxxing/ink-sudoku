package engine

import (
	"reflect"
	"testing"
)

func TestGenerateUniqueAndSymmetric(t *testing.T) {
	seeds := map[Level]int{Easy: 100, Medium: 100, Hard: 50, Expert: 50}
	for level, n := range seeds {
		t.Run(level.String(), func(t *testing.T) {
			t.Parallel()
			for seed := uint64(1); seed <= uint64(n); seed++ {
				p := Generate(seed, level)
				g := p.Givens
				for i := 0; i < 81; i++ {
					if (g[i] == 0) != (g[80-i] == 0) {
						t.Fatalf("seed %d: not 180° symmetric at %s", seed, CellName(i))
					}
					if g[i] != 0 && g[i] != p.Solution[i] {
						t.Fatalf("seed %d: given disagrees with solution", seed)
					}
				}
				if CountSolutions(g, 2) != 1 {
					t.Fatalf("seed %d: not unique: %s", seed, g)
				}
				if p.Level != level || p.Grade.Level != level {
					t.Fatalf("seed %d: graded %s, want %s", seed, p.Grade.Level, level)
				}
			}
		})
	}
}

func TestGenerateDeterministic(t *testing.T) {
	for level := Easy; level <= Expert; level++ {
		a, b := Generate(42, level), Generate(42, level)
		if !reflect.DeepEqual(a, b) {
			t.Errorf("%s: same seed gave different puzzles", level)
		}
		if c := Generate(43, level); c.Givens == a.Givens {
			t.Errorf("%s: seeds 42 and 43 gave the same puzzle", level)
		}
	}
}
