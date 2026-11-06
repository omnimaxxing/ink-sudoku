package kakuro

import (
	"strings"
	"testing"
)

// The example puzzle of Wikipedia's "Kakuro" article (File:Kakuro black
// box.svg, solution in File:Kakuro black box solution.svg), transcribed
// with its solution digits filled in.
const wikipedia = `
#####  23\   30\  ##### #####  27\   12\   16\
  \16    9     7  ##### 17\24    8     7     9
  \17    8     9  15\29    8     9     5     7
  \35    6     8     5     9     7  12\  #####
#####   \7     6     1   7\8     2     6   7\
#####  11\  10\16    4     6     1     3     2
  \21    8     9     3     1    \5     1     4
   \6    3     1     2  #####   \3     2     1
`

func mustParse(t *testing.T, s string) Puzzle {
	t.Helper()
	p, err := Parse(s)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSolveWikipedia(t *testing.T) {
	p := mustParse(t, wikipedia)
	if n := CountSolutions(p, 2); n != 1 {
		t.Fatalf("%d solutions, want 1", n)
	}
	sol, ok := Solve(p)
	if !ok || string(sol) != string(p.Solution) {
		t.Fatalf("solved %v, want %v", sol, p.Solution)
	}
	if r := Grade(p); !r.Solved {
		t.Errorf("logic got stuck: %+v", r)
	}
}

func TestFormatRoundTrip(t *testing.T) {
	p := mustParse(t, wikipedia)
	text := Format(p, p.Solution)
	q := mustParse(t, text)
	if Format(q, q.Solution) != text || q.N != 8 {
		t.Fatalf("round trip changed the puzzle:\n%s", Format(q, q.Solution))
	}
	if blank := Format(p, nil); !strings.Contains(blank, "  .  ") || strings.Contains(blank, "  9  ") {
		t.Fatalf("Format without digits:\n%s", blank)
	}
}

func TestRejectsBadPuzzles(t *testing.T) {
	for name, s := range map[string]string{
		"single-cell run": `
#####  3\  #####
  \3    .  #####
##### ##### #####`,
		"white clue row": `
  .    4\    3\
  \3    .     .
  \4    .     .`,
		"impossible clue": `
#####   4\   18\
  \3    .     .
  \4    .     .`,
	} {
		if _, err := Parse(s); err == nil {
			t.Errorf("%s: parsed", name)
		}
	}
	// Two runs that can swap digits: 1 2 / 2 1 and 2 1 / 1 2 both fit.
	p := mustParse(t, `
#####   3\    3\
  \3    .     .
  \3    .     .`)
	if n := CountSolutions(p, 2); n != 2 {
		t.Errorf("swappable square: %d solutions, want 2", n)
	}
	if r := Grade(p); r.Solved {
		t.Error("logic solved a puzzle with two solutions")
	}
}
