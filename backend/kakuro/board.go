package kakuro

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// maxN is the largest grid the engine handles (Expert's).
const maxN = 13

const maxCells = maxN * maxN

// maxRuns bounds the runs of a grid: a line of n cells starting with a
// black one holds at most n/3 runs (a clue cell and two white cells each).
const maxRuns = 2 * (maxN - 1) * (maxN / 3)

// run is a maximal across or down line of white cells.
type run struct {
	cells [9]uint8
	n     uint8 // length
	sum   uint8 // clue
	clue  uint8 // the clue cell left of or above it
	down  bool
}

// board is a puzzle's layout and clues in the form the solvers use.
type board struct {
	n      int
	white  [maxCells]bool
	runs   [maxRuns]run
	nr     int
	runOf  [maxCells][2]uint8 // across and down run of each white cell
	whites [maxCells]uint8    // white cells in reading order
	nw     int
}

// newBoard builds the runs of a layout (sums left 0). Row 0 and column 0
// must be black and every run 2-9 cells long.
func newBoard(n int, white []bool) (*board, error) {
	if n < 3 || n > maxN || len(white) != n*n {
		return nil, fmt.Errorf("kakuro: grid must be N×N with 3 <= N <= %d", maxN)
	}
	b := &board{n: n}
	copy(b.white[:], white)
	for i := 0; i < n; i++ {
		if b.white[i] || b.white[i*n] {
			return nil, errors.New("kakuro: row 0 and column 0 must be black")
		}
	}
	for c := 0; c < n*n; c++ {
		if b.white[c] {
			b.whites[b.nw] = uint8(c)
			b.nw++
			continue
		}
		for _, down := range [2]bool{false, true} {
			step := 1
			if down {
				step = n
			}
			first := c + step
			if down && first >= n*n || !down && first%n == 0 || !b.white[first] {
				continue
			}
			if b.nr == maxRuns {
				return nil, errors.New("kakuro: too many runs")
			}
			rn := &b.runs[b.nr]
			*rn = run{clue: uint8(c), down: down}
			for x := first; x < n*n && b.white[x] && (down || x%n != 0); x += step {
				if rn.n == 9 {
					return nil, fmt.Errorf("kakuro: run at %s is longer than 9 cells", cellPhrase(n, first))
				}
				rn.cells[rn.n] = uint8(x)
				rn.n++
				b.runOf[x][btoi(down)] = uint8(b.nr)
			}
			if rn.n < 2 {
				return nil, fmt.Errorf("kakuro: single-cell run at %s", cellPhrase(n, first))
			}
			b.nr++
		}
	}
	return b, nil
}

func btoi(v bool) int {
	if v {
		return 1
	}
	return 0
}

// puzzleBoard builds the board of p and checks its clues.
func puzzleBoard(p Puzzle) (*board, error) {
	if len(p.Cells) != p.N*p.N {
		return nil, fmt.Errorf("kakuro: %d cells for N=%d", len(p.Cells), p.N)
	}
	white := make([]bool, len(p.Cells))
	for i, c := range p.Cells {
		white[i] = !c.Black
	}
	b, err := newBoard(p.N, white)
	if err != nil {
		return nil, err
	}
	for i := 0; i < b.nr; i++ {
		rn := &b.runs[i]
		clue := p.Cells[rn.clue]
		rn.sum = clue.Across
		if rn.down {
			rn.sum = clue.Down
		}
		if !sumOK(int(rn.n), int(rn.sum)) {
			return nil, fmt.Errorf("kakuro: no %d digits add up to %d (run at %s)", rn.n, rn.sum, cellPhrase(p.N, int(rn.cells[0])))
		}
	}
	return b, nil
}

// Validate checks that p is a well-formed puzzle: layout, clues each
// reachable with distinct digits, and, if present, a Solution that fits.
// It doesn't check uniqueness (see CountSolutions).
func Validate(p Puzzle) error {
	b, err := puzzleBoard(p)
	if err != nil {
		return err
	}
	if p.Solution == nil {
		return nil
	}
	if len(p.Solution) != len(p.Cells) {
		return errors.New("kakuro: solution has the wrong length")
	}
	if !b.fits(p.Solution, true) {
		return errors.New("kakuro: solution breaks a run")
	}
	return nil
}

// fits reports whether digits (0 = empty) repeat no digit within a run and,
// if full, fill every white cell with each run adding up to its clue.
func (b *board) fits(digits []uint8, full bool) bool {
	for i := 0; i < b.nr; i++ {
		rn := &b.runs[i]
		var seen uint16
		sum := 0
		for _, c := range rn.cells[:rn.n] {
			v := digits[c]
			if v == 0 {
				if full {
					return false
				}
				continue
			}
			if v > 9 || seen&bit(v) != 0 {
				return false
			}
			seen |= bit(v)
			sum += int(v)
		}
		if full && sum != int(rn.sum) {
			return false
		}
	}
	for c, v := range digits {
		if v != 0 && !b.white[c] {
			return false
		}
	}
	return true
}

// puzzle turns the board's layout and sums back into Puzzle cells.
func (b *board) cells() []Cell {
	cells := make([]Cell, b.n*b.n)
	for c := range cells {
		cells[c].Black = !b.white[c]
	}
	for i := 0; i < b.nr; i++ {
		rn := &b.runs[i]
		if rn.down {
			cells[rn.clue].Down = rn.sum
		} else {
			cells[rn.clue].Across = rn.sum
		}
	}
	return cells
}

// Format draws p as text, one row per line and five characters per cell:
// clue cells as "17\  " (down clue left of the slash, across clue right),
// plain black cells as "#####", white cells as their digit in digits, or
// "." (digits may be nil). Parse reads it back.
func Format(p Puzzle, digits []uint8) string {
	var sb strings.Builder
	for r := 0; r < p.N; r++ {
		for c := 0; c < p.N; c++ {
			i := r*p.N + c
			if c > 0 {
				sb.WriteByte(' ')
			}
			cell := p.Cells[i]
			switch {
			case cell.Black && cell.Down == 0 && cell.Across == 0:
				sb.WriteString("#####")
			case cell.Black:
				fmt.Fprintf(&sb, "%2s\\%-2s", clueText(cell.Down), clueText(cell.Across))
			case i < len(digits) && digits[i] != 0:
				fmt.Fprintf(&sb, "  %d  ", digits[i])
			default:
				sb.WriteString("  .  ")
			}
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func clueText(v uint8) string {
	if v == 0 {
		return ""
	}
	return strconv.Itoa(int(v))
}

// Parse reads the Format layout: whitespace-separated cells, one row per
// line. A cell is a run of '#' (black), "down\across" with either side
// possibly empty (a clue cell), '.' (an empty white cell) or a digit (a
// white cell with its solution digit). Solution is set only if every white
// cell has a digit.
func Parse(s string) (Puzzle, error) {
	var rows [][]string
	for _, line := range strings.Split(s, "\n") {
		if f := strings.Fields(line); len(f) > 0 {
			rows = append(rows, f)
		}
	}
	n := len(rows)
	p := Puzzle{N: n, Cells: make([]Cell, n*n)}
	sol := make([]uint8, n*n)
	full := true
	for r, row := range rows {
		if len(row) != n {
			return Puzzle{}, fmt.Errorf("kakuro: row %d has %d cells, want %d", r, len(row), n)
		}
		for c, tok := range row {
			i := r*n + c
			switch {
			case strings.Trim(tok, "#") == "":
				p.Cells[i].Black = true
			case strings.Contains(tok, `\`):
				down, across, _ := strings.Cut(tok, `\`)
				d, err1 := parseClue(down)
				a, err2 := parseClue(across)
				if err1 != nil || err2 != nil {
					return Puzzle{}, fmt.Errorf("kakuro: bad clue %q", tok)
				}
				p.Cells[i] = Cell{Black: true, Down: d, Across: a}
			case tok == ".":
				full = false
			case len(tok) == 1 && tok[0] >= '1' && tok[0] <= '9':
				sol[i] = tok[0] - '0'
			default:
				return Puzzle{}, fmt.Errorf("kakuro: bad cell %q", tok)
			}
		}
	}
	if full {
		p.Solution = sol
	}
	if err := Validate(p); err != nil {
		return Puzzle{}, err
	}
	return p, nil
}

func parseClue(s string) (uint8, error) {
	if s == "" {
		return 0, nil
	}
	v, err := strconv.Atoi(s)
	if err != nil || v < 1 || v > 45 {
		return 0, errors.New("bad clue")
	}
	return uint8(v), nil
}
