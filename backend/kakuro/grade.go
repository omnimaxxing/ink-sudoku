package kakuro

import "fmt"

// Technique is a human solving technique. Values run from easiest to
// hardest, and the logical solver always applies the easiest one that
// makes progress.
//
// Every technique works on the candidates the baseline leaves (see state):
// a cell may only hold digits that both its runs' combinations allow, given
// the digits already placed. The first four rungs are the ways a cell gets
// filled from that baseline alone, without any notes:
//
//   - LastCell: one open cell left in a run; it is the clue minus the rest.
//   - UniqueCombination: two runs that can each only be one set of digits
//     ("17 in two cells is 8 + 9") cross, and share one digit.
//   - CrossRun: the digits the two runs allow meet in one digit, though at
//     least one run has several combinations.
//   - RequiredDigit: every combination of a run needs some digit, and only
//     one of its cells can take it (Kakuro's hidden single).
//
// The rest need pencil marks:
//
//   - NakedSingle: notes and earlier eliminations leave one digit.
//   - CombinationExclusion: a combination is ruled out because one of its
//     digits fits no cell of the run, or a cell takes none of its digits;
//     whatever only that combination offered goes, or a digit all the
//     survivors need gets its only cell.
//   - TwoCellSum: the last two open cells of a run make a known sum, so
//     each one's candidates mirror the other's (a + b = 10: a in {1,2,3}
//     means b in {9,8,7}).
//   - NakedPair/NakedTriple: n cells of a run sharing n digits.
//   - HiddenPair/HiddenTriple: n digits the run must hold (in every
//     combination that still fits) with only n cells to go in.
//   - SumArithmetic: with three or more open cells, a digit is crossed off
//     a cell when the others can't make up the rest of the sum with
//     different digits from their candidates. This is the full check of a
//     run on its own; it subsumes the run techniques below it.
//
// "Combination exclusion across intersecting runs" isn't a separate rung:
// two runs share at most one cell, so what one run's combinations say about
// the other passes entirely through that cell's candidates, which the
// techniques above already use.
type Technique uint8

const (
	LastCell Technique = iota
	UniqueCombination
	CrossRun
	RequiredDigit
	NakedSingle
	CombinationExclusion
	TwoCellSum
	NakedPair
	HiddenPair
	NakedTriple
	HiddenTriple
	SumArithmetic

	// Mistake and Reveal only appear in hints, never in grades.
	Mistake // the player's entries or notes contradict the solution
	Reveal  // no technique applies, so the hint gives away a cell
)

// NumTechniques is the number of solving techniques (LastCell..SumArithmetic).
const NumTechniques = int(lastTechnique) + 1

const lastTechnique = SumArithmetic

var techniqueNames = [...]string{
	LastCell:             "Last cell",
	UniqueCombination:    "Unique combination",
	CrossRun:             "Cross-run intersection",
	RequiredDigit:        "Required digit",
	NakedSingle:          "Naked single",
	CombinationExclusion: "Combination exclusion",
	TwoCellSum:           "Two-cell sum",
	NakedPair:            "Naked pair",
	HiddenPair:           "Hidden pair",
	NakedTriple:          "Naked triple",
	HiddenTriple:         "Hidden triple",
	SumArithmetic:        "Sum arithmetic",
	Mistake:              "Mistake",
	Reveal:               "Reveal",
}

func (t Technique) String() string {
	if int(t) < len(techniqueNames) {
		return techniqueNames[t]
	}
	return fmt.Sprintf("Technique(%d)", t)
}

func (t Technique) MarshalText() ([]byte, error) { return []byte(t.String()), nil }

// Level mapping. A level is the hardest technique its solve needs; the
// grid also grows with the level (see specs).
//
//   - Easy: filling cells from the run sums alone (last cells, unique
//     combinations, crossing runs, required digits). No notes needed; like
//     Sudoku's singles.
//   - Medium: light pencil work on one run at a time: ruling out
//     combinations that don't fit the cells, and mirroring the last two
//     cells of a sum.
//   - Hard: naked and hidden pairs and triples within a run. These need
//     full notes and looking at several cells together.
//   - Expert: sum arithmetic over three or more open cells, testing which
//     digits the rest of the run could still make up.
//
// Puzzles our techniques can't finish are never generated.
var ceilings = [...]Technique{Easy: RequiredDigit, Medium: TwoCellSum, Hard: HiddenTriple, Expert: SumArithmetic}

// floors is the easiest technique a level's hardest step can be.
var floors = [...]Technique{Easy: LastCell, Medium: CombinationExclusion, Hard: NakedPair, Expert: SumArithmetic}

func levelOf(hardest Technique) Level {
	for l := Easy; l < Expert; l++ {
		if hardest <= ceilings[l] {
			return l
		}
	}
	return Expert
}

// Rating is the result of grading a puzzle with the logical solver.
type Rating struct {
	Solved  bool      // false: the logic got stuck
	Level   Level     // set when Solved
	Hardest Technique // hardest technique the solve needed
	Steps   int       // logical steps (placements and elimination rounds)
	Counts  [NumTechniques]int
}

// Techniques maps the name of each technique the solve used to its count.
func (r Rating) Techniques() map[string]int {
	out := map[string]int{}
	for t, n := range r.Counts {
		if n > 0 {
			out[Technique(t).String()] = n
		}
	}
	return out
}

// Grade solves p's clues with human techniques, easiest first, and rates
// them. A malformed puzzle comes back unsolved.
func Grade(p Puzzle) Rating {
	b, err := puzzleBoard(p)
	if err != nil {
		return Rating{}
	}
	return new(solver).rate(b, upTo(lastTechnique))
}
