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

// Puzzle numbers are shared ("Hard No. 482,137") and dailies are the same for
// everyone, so a seed must keep giving the same puzzle. If this fails, the
// change to the generator remaps every puzzle number: don't ship it as is.
func TestGenerateStable(t *testing.T) {
	for _, c := range []struct {
		seed   uint64
		level  Level
		givens string
	}{
		{111111, Easy, "........7..819..4.6...7.81.48.92..3.7.......8.3..57.94.46.8...9.2..615..8........"},
		{482137, Medium, "..13.67..32.74..1.....1....7..9..4.6....3....9.3..4..7....9.....7..52.41..46.35.."},
		{482137, Hard, ".........5.839.....3...7.583.6.7....9..2.8..6....4.1.981.7...9.....854.3........."},
		{999999, Expert, "1.....8.....4..73..7..63.9.9...1..7.3..2.6..5.8..4...9.3.62..4..64..1.....9.....8"},
		{735291, Expert, ".4...7.1...9.5...76...4....8.2...3.5.1.....6.5.4...9.1....6...32...9.6...5.7...8."},
		{20261104, Medium, "3.....6..514..6....76.8.....5.6.14...4.....1...98.7.2.....6.34....9..875..2.....9"},
		{48213, Hard, ".84...27....5.431.....1.......7....9.6.2.9.5.4....6.......2.....356.8....21...98."},
		{264851, Easy, ".....6....7.5....66.98...257...6......39178......5...113...45.28....5.7....2....."},
	} {
		if got := Generate(c.seed, c.level).Givens.String(); got != c.givens {
			t.Errorf("%s No. %d changed:\n got  %s\n want %s", c.level, c.seed, got, c.givens)
		}
	}
}
