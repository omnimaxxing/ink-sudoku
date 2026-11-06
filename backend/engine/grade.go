package engine

import (
	"fmt"
	"strings"
)

// Technique is a human solving technique. Values run from easiest to
// hardest, and the logical solver always applies the easiest one that makes
// progress. Hidden singles rank below naked singles because cross-hatching
// works without pencil marks; naked singles need every digit checked.
//
// The Expert techniques come in families (see expertFamilies), each a run
// of the ladder: fish, simple colouring, single-digit patterns, wings. The
// one-digit families come first, as on SudokuWiki (X-wing, then simple
// colouring), and the skyscraper and kite follow colouring as chains with
// a weak link in the middle; wings reason about several digits at once.
// Lists differ on the details (HoDoKu puts the skyscraper below the X-wing),
// but keeping families contiguous lets the generator aim at one.
type Technique uint8

const (
	HiddenSingle Technique = iota
	NakedSingle
	Pointing // locked candidates, type 1
	Claiming // locked candidates, type 2
	NakedPair
	HiddenPair
	NakedTriple
	HiddenTriple
	NakedQuad
	HiddenQuad
	XWing
	Swordfish
	SimpleColouring
	Skyscraper
	TwoStringKite
	XYWing
	WWing
	XYZWing

	// Mistake and Reveal only appear in hints, never in grades.
	Mistake // the player's entries or notes contradict the solution
	Reveal  // no technique applies, so the hint gives away a cell
)

// NumTechniques is the number of solving techniques (HiddenSingle..XYZWing).
const NumTechniques = int(lastTechnique) + 1

// lastTechnique is the top of the ladder.
const lastTechnique = XYZWing

var techniqueNames = [...]string{
	HiddenSingle:    "Hidden single",
	NakedSingle:     "Naked single",
	Pointing:        "Locked candidates (pointing)",
	Claiming:        "Locked candidates (claiming)",
	NakedPair:       "Naked pair",
	HiddenPair:      "Hidden pair",
	NakedTriple:     "Naked triple",
	HiddenTriple:    "Hidden triple",
	NakedQuad:       "Naked quad",
	HiddenQuad:      "Hidden quad",
	XWing:           "X-wing",
	Swordfish:       "Swordfish",
	SimpleColouring: "Simple colouring",
	Skyscraper:      "Skyscraper",
	TwoStringKite:   "Two-string kite",
	XYWing:          "XY-wing",
	WWing:           "W-wing",
	XYZWing:         "XYZ-wing",
	Mistake:         "Mistake",
	Reveal:          "Reveal",
}

func (t Technique) String() string {
	if int(t) < len(techniqueNames) {
		return techniqueNames[t]
	}
	return fmt.Sprintf("Technique(%d)", t)
}

func (t Technique) MarshalText() ([]byte, error) { return []byte(t.String()), nil }

// Level is a difficulty level.
type Level uint8

const (
	Easy Level = iota
	Medium
	Hard
	Expert
	Beyond // needs techniques this engine doesn't have; never generated
)

var levelNames = [...]string{"Easy", "Medium", "Hard", "Expert", "Beyond"}

func (l Level) String() string {
	if int(l) < len(levelNames) {
		return levelNames[l]
	}
	return fmt.Sprintf("Level(%d)", l)
}

func (l Level) MarshalText() ([]byte, error) { return []byte(l.String()), nil }

// ParseLevel parses a level name, case-insensitively.
func ParseLevel(s string) (Level, error) {
	for i, n := range levelNames {
		if strings.EqualFold(s, n) {
			return Level(i), nil
		}
	}
	return 0, fmt.Errorf("engine: unknown level %q", s)
}

// Level mapping. Difficulty comes from what the solver has to see, not from
// the clue count:
//
//   - Easy: singles only, and at every point at least easyMinChoices cells
//     can be filled by a single. There are always several ways forward, so
//     nobody gets stuck hunting for the one cell that moves.
//   - Medium: singles with a narrow point (only one or two placements
//     available somewhere), or locked candidates. Both still work by
//     scanning houses; neither needs a full grid of pencil marks.
//   - Hard: naked or hidden pairs, triples and quads. These need complete
//     pencil marks and reasoning about several cells at once.
//   - Expert: patterns that span several houses and are only visible in the
//     candidate grid. Each Expert puzzle is built around one family, picked
//     from its seed: fish (X-wing, swordfish), simple colouring, single-digit
//     patterns (skyscraper, two-string kite) or wings (XY-, W- and
//     XYZ-wing). Its hardest step is from that family.
//
// Puzzles our techniques can't finish are Beyond and never generated.
//
// Clue count barely separates Easy from Medium (both carve to ~28 givens);
// narrowness does. 3 was picked from sudokugen runs: at 2, Easy keeps
// two-option bottlenecks; at 4+, Medium fills with near-Easy puzzles.
const easyMinChoices = 3

// Narrowest ignores states with fewer empty cells than this, since the
// endgame can't offer more placements than there are empty cells.
const narrowFloor = easyMinChoices

// Rating is the result of grading a puzzle with the logical solver.
type Rating struct {
	Level   Level
	Solved  bool      // false: the logic got stuck (Level is Beyond)
	Hardest Technique // hardest technique the solve needed
	Steps   int       // logical steps (placements and elimination rounds)
	// Narrowest is the fewest cells a single could fill at any point of the
	// solve; 0 means a harder technique had to make progress somewhere.
	Narrowest int
	Counts    [NumTechniques]int // uses per technique
}

// Techniques lists the techniques the solve used, easiest first.
func (r Rating) Techniques() []Technique {
	var out []Technique
	for t, n := range r.Counts {
		if n > 0 {
			out = append(out, Technique(t))
		}
	}
	return out
}

// Grade solves g with human techniques, easiest first, and rates it.
func Grade(g Grid) Rating {
	r, _ := rate(g, lastTechnique, 0)
	return r
}

// rate runs the logical solver using techniques up to ceiling. If
// minChoices > 0 it gives up as soon as fewer singles than that are
// available; if minChoices < 0 it skips measuring Narrowest (and leaves it
// 0). It returns the rating and the final board.
func rate(g Grid, ceiling Technique, minChoices int) (Rating, Grid) {
	return new(solver).rate(g, upTo(ceiling), minChoices)
}

// solver is scratch space for rating. The finders are called through a
// table, so a state and move on the stack would escape to the heap on every
// solve; carve reuses one solver for its dozens of solves instead.
type solver struct {
	s state
	m move
}

// solves reports whether the techniques in set finish g, which is what
// rate's Solved says, only faster. Rate places one single per step and
// starts the ladder over; this places every single a sweep finds. Singles
// never block one another, so both reach the same position before anything
// harder is needed, and from there both take the same steps.
func (sv *solver) solves(g Grid, set techSet) bool {
	var ok bool
	if sv.s, ok = newState(g); !ok {
		return false
	}
	s, m := &sv.s, &sv.m
	harder := set &^ (1<<HiddenSingle | 1<<NakedSingle)
	for s.empty > 0 && !s.broken {
		if s.placeSingles() {
			continue
		}
		if s.broken || !s.nextIn(m, harder) {
			return false
		}
		s.apply(m)
	}
	return !s.broken
}

func (sv *solver) rate(g Grid, set techSet, minChoices int) (Rating, Grid) {
	r := Rating{Level: Beyond, Narrowest: 81}
	var ok bool
	if sv.s, ok = newState(g); !ok {
		return r, g
	}
	s, m := &sv.s, &sv.m
	for s.empty > 0 {
		if minChoices >= 0 && s.empty >= narrowFloor {
			if a := s.singlesAvailable(); a < r.Narrowest {
				r.Narrowest = a
				if a < minChoices {
					return r, s.val
				}
			}
		}
		if !s.nextIn(m, set) {
			return r, s.val
		}
		s.apply(m)
		r.Steps++
		r.Counts[m.tech]++
		r.Hardest = max(r.Hardest, m.tech)
	}
	if s.broken {
		return r, s.val
	}
	if minChoices < 0 {
		r.Narrowest = 0
	}
	r.Solved = true
	r.Level = levelOf(&r)
	return r, s.val
}

func levelOf(r *Rating) Level {
	switch {
	case !r.Solved:
		return Beyond
	case r.Hardest >= XWing:
		return Expert
	case r.Hardest >= NakedPair:
		return Hard
	case r.Hardest >= Pointing || r.Narrowest < easyMinChoices:
		return Medium
	default:
		return Easy
	}
}
