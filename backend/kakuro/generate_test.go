package kakuro

import (
	"maps"
	"reflect"
	"testing"
)

func TestGenerate(t *testing.T) {
	seeds := map[Level]int{Easy: 30, Medium: 15, Hard: 6, Expert: 4}
	for level := Easy; level <= Expert; level++ {
		t.Run(level.String(), func(t *testing.T) {
			t.Parallel()
			for seed := uint64(1); seed <= uint64(seeds[level]); seed++ {
				p := Generate(seed, level)
				n := p.N
				if n != level.Size() || p.Level != level || p.Seed != seed {
					t.Fatalf("seed %d: N=%d level=%s seed=%d", seed, n, p.Level, p.Seed)
				}
				if err := Validate(p); err != nil {
					t.Fatalf("seed %d: %v", seed, err)
				}
				for c, cell := range p.Cells {
					r, col := c/n, c%n
					if (r == 0 || col == 0) && !cell.Black {
						t.Fatalf("seed %d: white cell in the clue row or column", seed)
					}
					if v := p.Solution[c]; cell.Black != (v == 0) || v > 9 {
						t.Fatalf("seed %d: solution %d on a black=%v cell", seed, v, cell.Black)
					}
					if r > 0 && col > 0 && cell.Black != p.Cells[(n-r)*n+n-col].Black {
						t.Fatalf("seed %d: layout not 180° symmetric", seed)
					}
				}
				if CountSolutions(p, 2) != 1 {
					t.Fatalf("seed %d: not unique", seed)
				}
				r := Grade(p)
				if !r.Solved || r.Level != level || r.Hardest.String() != p.Hardest || !maps.Equal(r.Techniques(), p.Techniques) {
					t.Fatalf("seed %d: graded %+v, puzzle says %s %v", seed, r, p.Hardest, p.Techniques)
				}
				if r.Hardest < floors[level] || r.Hardest > ceilings[level] {
					t.Fatalf("seed %d: hardest %s outside %s", seed, r.Hardest, level)
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
		if c := Generate(43, level); reflect.DeepEqual(c.Cells, a.Cells) {
			t.Errorf("%s: seeds 42 and 43 gave the same puzzle", level)
		}
	}
}

func TestLevelNames(t *testing.T) {
	for l := Easy; l <= Expert; l++ {
		if got, err := ParseLevel(l.String()); err != nil || got != l {
			t.Errorf("ParseLevel(%q) = %v, %v", l, got, err)
		}
	}
	if _, err := ParseLevel("beyond"); err == nil {
		t.Error("ParseLevel accepted an unknown level")
	}
}
