// Package kgame is one Ink Kakuro game: the puzzle, the player's entries,
// candidates and ink, the undo history and the timer, plus the rules that
// change them. It mirrors package game (Ink Sudoku) for an N×N grid of
// clue (black) and white cells. It does no I/O except Store's.
package kgame

import (
	"fmt"
	"slices"
	"time"

	"sudoku/hwr"
	"sudoku/kakuro"
)

// UndoDepth caps the undo history.
const UndoDepth = 200

// Correction logging, as in Ink Sudoku: an answer rewritten in the same cell
// within CorrectionWindow as a different digit is logged as a misread of the
// earlier strokes when the earlier read was unsure (Confidence below
// CorrectionConfidence) or the new answer is written over the old one (at
// least CorrectionOverlap of its ink box inside the old ink's). Otherwise,
// and always after a scratch-out, undo or setCell took the old answer away,
// it's a change of mind.
const (
	CorrectionWindow     = 20 * time.Second
	CorrectionConfidence = 0.995
	CorrectionOverlap    = 0.5
)

// MaxGap caps what one gap between player messages adds to the timer, so
// the tablet sleeping or the player walking away with the app open doesn't
// count (see Touch).
const MaxGap = 90 * time.Second

// Ink roles.
const (
	RoleAnswer    = "answer"
	RoleCandidate = "candidate"
)

// Ink is one stored stroke in protocol form. Points are x, y, t, p, … in
// grid-local pixels.
type Ink struct {
	Role   string    `json:"role"`
	Digit  int       `json:"digit"`
	Points []float32 `json:"points"`
}

// ref is one stroke of a cell's ink: an id into Game.points plus its role.
type ref struct {
	id    int
	cand  bool
	digit uint8
}

// board is the undoable part of a game. Its slices are N*N long. Inner ink
// slices are never modified in place, so clone (which copies the outer
// slices) is a snapshot.
type board struct {
	entries    []uint8
	candidates []uint16
	ink        [][]ref
}

func newBoard(cells int) board {
	return board{make([]uint8, cells), make([]uint16, cells), make([][]ref, cells)}
}

func (b *board) clone() board {
	return board{slices.Clone(b.entries), slices.Clone(b.candidates), slices.Clone(b.ink)}
}

func (b *board) equal(o *board) bool {
	if !slices.Equal(b.entries, o.entries) || !slices.Equal(b.candidates, o.candidates) || len(b.ink) != len(o.ink) {
		return false
	}
	for c := range b.ink {
		if !slices.Equal(b.ink[c], o.ink[c]) {
			return false
		}
	}
	return true
}

// answerRec remembers the last answer written in a cell, for correction
// logging.
type answerRec struct {
	seq     int
	at      time.Time
	digit   int
	rect    hwr.Rect
	box     hwr.Rect // the strokes' bounds, grown a little so a straight 1 has an area
	strokes []hwr.Stroke
	mark    hwr.Mark
}

// misread reports whether rewriting r as next fixes a misread rather than
// changing the player's mind: r was read unsurely, or next is written over it.
func (r *answerRec) misread(next *answerRec) bool {
	if r.mark.Confidence < CorrectionConfidence {
		return true
	}
	b := next.box
	return b.Area() > 0 && r.box.Intersect(b).Area() >= CorrectionOverlap*b.Area()
}

// Game is the state of one puzzle in play. It is not safe for concurrent use.
type Game struct {
	puzzle   kakuro.Puzzle
	daily    string // date of the daily puzzle, "" for other games
	recorded bool   // the solve has been counted in Stats

	board
	undo   []board
	points map[int][]float32 // stroke id -> x, y, t, p, …
	nextID int

	gridSize float32
	mistakes int
	hints    int
	// ruledOut holds, per cell, the digits a revealed hint eliminated. Hints
	// only reason from a correct board, so these stay true through undo; they
	// let the next hint move on for players who don't keep pencil marks.
	ruledOut []uint16

	elapsed   time.Duration
	since     time.Time // start of the current running stretch; zero when stopped
	connected bool

	seq  int          // ApplyMarks calls
	last []*answerRec // per cell
}

// Validate reports whether p is a playable grid: N×N cells and solution,
// row 0 and column 0 black, a digit 1–9 in the solution of every white cell
// and 0 on every black one.
func Validate(p kakuro.Puzzle) error {
	n := p.N
	if n < 2 || n > 32 || len(p.Cells) != n*n || len(p.Solution) != n*n {
		return fmt.Errorf("kakuro puzzle: N %d with %d cells, %d solution digits", n, len(p.Cells), len(p.Solution))
	}
	if p.Level < kakuro.Easy || p.Level > kakuro.Expert {
		return fmt.Errorf("kakuro puzzle: level %d", p.Level)
	}
	white := 0
	for c, cell := range p.Cells {
		v := p.Solution[c]
		switch {
		case (c < n || c%n == 0) && !cell.Black:
			return fmt.Errorf("kakuro puzzle: cell %d in the clue row/column is white", c)
		case cell.Black && v != 0, !cell.Black && (v < 1 || v > 9):
			return fmt.Errorf("kakuro puzzle: cell %d: solution %d", c, v)
		}
		if !cell.Black {
			white++
		}
	}
	if white == 0 {
		return fmt.Errorf("kakuro puzzle: no white cells")
	}
	return nil
}

// New starts a game on p, which must pass Validate.
func New(p kakuro.Puzzle) *Game {
	p.Cells = slices.Clone(p.Cells)
	p.Solution = slices.Clone(p.Solution)
	n := p.N * p.N
	return &Game{puzzle: p, board: newBoard(n), points: map[int][]float32{}, last: make([]*answerRec, n),
		ruledOut: make([]uint16, n)}
}

// NewDaily starts the daily puzzle p for date ("2006-01-02").
func NewDaily(p kakuro.Puzzle, date string) *Game {
	g := New(p)
	g.daily = date
	return g
}

// Puzzle is the game's puzzle; its slices must not be modified.
func (g *Game) Puzzle() kakuro.Puzzle { return g.puzzle }

func (g *Game) N() int                         { return g.puzzle.N }
func (g *Game) Seed() uint64                   { return g.puzzle.Seed }
func (g *Game) Level() kakuro.Level            { return g.puzzle.Level }
func (g *Game) Hardest() string                { return g.puzzle.Hardest }
func (g *Game) Daily() string                  { return g.daily }
func (g *Game) Entries() []uint8               { return slices.Clone(g.entries) }
func (g *Game) Candidates() []uint16           { return slices.Clone(g.candidates) }
func (g *Game) CanUndo() bool                  { return len(g.undo) > 0 }
func (g *Game) Mistakes() int                  { return g.mistakes }
func (g *Game) Hints() int                     { return g.hints }
func (g *Game) GridSize() float32              { return g.gridSize }
func (g *Game) Entry(cell int) int             { return int(g.entries[cell]) }
func (g *Game) CellCandidates(cell int) uint16 { return g.candidates[cell] }

// Black reports whether cell is a clue cell.
func (g *Game) Black(cell int) bool { return g.puzzle.Cells[cell].Black }

// CellInk returns the cell's stored ink, oldest first (never nil).
func (g *Game) CellInk(cell int) []Ink {
	out := make([]Ink, 0, len(g.ink[cell]))
	for _, r := range g.ink[cell] {
		in := Ink{Role: RoleAnswer, Digit: int(r.digit), Points: g.points[r.id]}
		if r.cand {
			in.Role = RoleCandidate
		}
		out = append(out, in)
	}
	return out
}

// Solved reports whether every white cell holds its solution digit.
func (g *Game) Solved() bool { return slices.Equal(g.entries, g.puzzle.Solution) }

// Conflicts lists the white cells kakuro.Check flags, sorted (never nil): a
// digit repeated within a run, or a complete run with the wrong sum.
func (g *Game) Conflicts() []int {
	out := []int{}
	for _, c := range kakuro.Check(g.puzzle, slices.Clone(g.entries)) {
		if g.validCell(c) && !g.Black(c) {
			out = append(out, c)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// Elapsed is the play time so far.
func (g *Game) Elapsed(now time.Time) time.Duration {
	if g.since.IsZero() {
		return g.elapsed
	}
	return g.elapsed + gap(g.since, now)
}

// Connect starts the timer (a frontend is attached); Disconnect stops it.
// The timer also stops while the puzzle is solved.
func (g *Game) Connect(now time.Time)    { g.connected = true; g.retime(now) }
func (g *Game) Disconnect(now time.Time) { g.connected = false; g.retime(now) }

// Touch notes a player message at now. While the timer runs, the time since
// the previous one (or since Connect) counts, up to MaxGap.
func (g *Game) Touch(now time.Time) { g.retime(now) }

func (g *Game) retime(now time.Time) {
	if !g.since.IsZero() {
		g.elapsed += gap(g.since, now)
		g.since = time.Time{}
	}
	if g.connected && !g.Solved() {
		g.since = now
	}
}

// gap is the play time between two player messages: at most MaxGap, and
// never negative (the clock may be set back).
func gap(from, to time.Time) time.Duration { return min(max(to.Sub(from), 0), MaxGap) }

// SetGridSize records the grid size in pixels that ink coordinates use,
// rescaling stored ink if it changed.
func (g *Game) SetGridSize(px float32) {
	if px <= 0 || px == g.gridSize {
		return
	}
	if g.gridSize > 0 {
		k := px / g.gridSize
		for _, pts := range g.points {
			for i := 0; i+1 < len(pts); i += 4 {
				pts[i], pts[i+1] = round(pts[i]*k, 10), round(pts[i+1]*k, 10)
			}
		}
		clear(g.last)
	}
	g.gridSize = px
}

// CellRect is cell's square in grid-local pixels (side gridSize / N).
func (g *Game) CellRect(cell int) hwr.Rect {
	n := g.puzzle.N
	s := g.gridSize / float32(n)
	x, y := float32(cell%n)*s, float32(cell/n)*s
	return hwr.Rect{X0: x, Y0: y, X1: x + s, Y1: y + s}
}

func (g *Game) validCell(c int) bool { return c >= 0 && c < len(g.entries) }

// checkWhite rejects a cell out of range or a clue cell.
func (g *Game) checkWhite(cell int) error {
	if !g.validCell(cell) {
		return fmt.Errorf("cell %d out of range 0..%d", cell, len(g.entries)-1)
	}
	if g.Black(cell) {
		return fmt.Errorf("cell %d is a clue cell", cell)
	}
	return nil
}

func (g *Game) checkCell(cell, digit, lo int) error {
	if err := g.checkWhite(cell); err != nil {
		return err
	}
	if digit < lo || digit > 9 {
		return fmt.Errorf("digit %d out of range %d..9", digit, lo)
	}
	return nil
}

// SetCell types digit (0 clears) into cell, dropping its answer ink. It
// reports whether anything changed; a clue cell is an error.
func (g *Game) SetCell(cell, digit int, now time.Time) (bool, error) {
	if err := g.checkCell(cell, digit, 0); err != nil {
		return false, err
	}
	prev := g.board.clone()
	if int(g.entries[cell]) != digit {
		g.dropAnswer(cell)
		g.setEntry(cell, digit)
		g.last[cell] = nil // its ink is gone: what's written next is a change of mind
	}
	return g.commit(&prev, now), nil
}

// ToggleCandidate flips digit's pencil mark in cell. Turning it off also
// drops that digit's candidate ink. A clue cell is an error.
func (g *Game) ToggleCandidate(cell, digit int, now time.Time) (bool, error) {
	if err := g.checkCell(cell, digit, 1); err != nil {
		return false, err
	}
	prev := g.board.clone()
	g.toggleCandidate(cell, digit, nil)
	return g.commit(&prev, now), nil
}

// Undo restores the state before the last change.
func (g *Game) Undo(now time.Time) bool {
	n := len(g.undo)
	if n == 0 {
		return false
	}
	g.board = g.undo[n-1]
	g.undo = g.undo[:n-1]
	clear(g.last) // what's written next is a change of mind, as after a scratch-out
	g.retime(now)
	return true
}

// Technique names of the steps Hint makes up when kakuro.NextStep has none.
const (
	Mistake = "Mistake"
	Reveal  = "Reveal"
)

// Hint returns the next logical step from the player's position; ok is
// false once the puzzle is solved. When kakuro.NextStep finds no step on an
// unsolved board, the hint names the wrong entries (a Mistake) or, with
// none, gives away one empty cell (a Reveal). It counts as a hint when
// reveal is set (the player sees the whole step), not for a nudge (its
// title alone).
func (g *Game) Hint(reveal bool) (kakuro.Step, bool) {
	if g.Solved() {
		return kakuro.Step{}, false
	}
	st, ok := kakuro.NextStep(g.puzzle, slices.Clone(g.entries), g.hintCandidates())
	if !ok {
		st = g.fallbackStep()
	}
	if reveal {
		g.hints++
		for _, e := range st.Eliminations {
			if g.validCell(e.Cell) {
				g.ruledOut[e.Cell] |= 1 << (e.Digit - 1)
			}
		}
	}
	return st, true
}

// hintCandidates is the player's pencil marks minus what revealed hints ruled
// out. A cell with marks never drops to 0 (NextStep's "no marks"), so marks
// that leave out the answer are still reported as a mistake.
func (g *Game) hintCandidates() []uint16 {
	const all = 1<<9 - 1
	out := make([]uint16, len(g.candidates))
	for c, marks := range g.candidates {
		if marks == 0 {
			marks = all
		}
		if m := marks &^ g.ruledOut[c]; m != 0 {
			out[c] = m
		} else if g.candidates[c] != 0 {
			out[c] = g.candidates[c]
		}
	}
	return out
}

// fallbackStep is a Mistake naming the wrong entries, else a Reveal of the
// first empty cell. The board must not be solved.
func (g *Game) fallbackStep() kakuro.Step {
	n := g.puzzle.N
	at := func(c int) string { return fmt.Sprintf("row %d, column %d", c/n, c%n) }
	var wrong []int
	empty := -1
	for c, v := range g.entries {
		switch {
		case g.Black(c):
		case v == 0:
			if empty < 0 {
				empty = c
			}
		case v != g.puzzle.Solution[c]:
			wrong = append(wrong, c)
		}
	}
	switch {
	case len(wrong) == 1:
		v := int(g.entries[wrong[0]])
		return kakuro.Step{Technique: Mistake, Cells: wrong, Digits: []int{v},
			Title: fmt.Sprintf("That %d isn't right", v),
			Text:  fmt.Sprintf("The %d in %s is wrong. It leads to a dead end.", v, at(wrong[0]))}
	case len(wrong) > 1:
		return kakuro.Step{Technique: Mistake, Cells: wrong,
			Title: "Some numbers are wrong",
			Text:  fmt.Sprintf("%d of your numbers are wrong. They're circled.", len(wrong))}
	}
	d := int(g.puzzle.Solution[empty])
	return kakuro.Step{Technique: Reveal, Cells: []int{empty}, Digits: []int{d},
		Placement: &kakuro.Cand{Cell: empty, Digit: d},
		Title:     "Here's one for free",
		Text:      fmt.Sprintf("None of my tricks work here, so here's a free one: %s is a %d.", at(empty), d)}
}

// Applied is what one ink message did to the game.
type Applied struct {
	Changed bool
	// Unknown is set when a mark could not be read; nothing was stored for it.
	Unknown bool
	// Correction is set when this ink rewrote a recent answer as a different
	// digit and the earlier strokes look misread (see CorrectionWindow).
	Correction *Correction
}

// Correction is a misread answer, ready for the hwr sample log.
type Correction struct {
	Cell    hwr.Rect
	Strokes []hwr.Stroke
	Mark    hwr.Mark // indexes Strokes
	Label   int      // the digit the player meant
}

// Sample turns c into a samples.jsonl record.
func (c Correction) Sample(m *hwr.Model, now time.Time) hwr.Sample {
	return hwr.NewSample(m, c.Cell, c.Strokes, c.Mark, c.Label, "correction", now)
}

// ApplyMarks applies the marks recognised in one ink message to cell, with
// Ink Sudoku's rules. strokes are the message's strokes (x, y, t, p, … in
// grid px) and each mark's Strokes index them. A scratch-out's Erases index
// the cell's stored ink as it was before this call (CellInk order) followed
// by strokes, which is what hwr.Analyze returns when given stored ++ new ink.
//
//   - answer: sets the entry and stores the strokes as answer ink, replacing
//     the previous answer ink;
//   - candidate: toggles the digit; on, its strokes are stored; off, the
//     digit's candidate ink goes too;
//   - scratch: removes the stored ink it covers. Ink is erased per digit:
//     touching any answer stroke clears the entry and all answer ink, touching
//     a candidate's stroke clears that candidate and all its ink;
//   - unknown: changes nothing and sets Unknown.
//
// Ink on a clue cell (or out of range) changes nothing.
func (g *Game) ApplyMarks(cell int, strokes [][]float32, marks []hwr.Mark, now time.Time) Applied {
	var res Applied
	g.seq++
	if g.checkWhite(cell) != nil {
		return res
	}
	prev := g.board.clone()
	stored := g.ink[cell]
	for _, m := range marks {
		switch {
		case m.Kind == hwr.ScratchOut:
			for _, i := range m.Erases {
				if i >= 0 && i < len(stored) {
					g.erase(cell, stored[i].id)
				}
			}
		case (m.Kind == hwr.Answer || m.Kind == hwr.Candidate) && m.Digit >= 1 && m.Digit <= 9:
			ok := len(m.Strokes) > 0
			for _, i := range m.Strokes {
				ok = ok && i >= 0 && i < len(strokes) && len(strokes[i]) >= 4
			}
			if !ok {
				res.Unknown = true
				continue
			}
			if m.Kind == hwr.Candidate {
				g.toggleCandidate(cell, m.Digit, func() []ref { return g.addInk(strokes, m.Strokes, true, m.Digit) })
				continue
			}
			rec := g.answerRecord(cell, strokes, m, now)
			if r := g.last[cell]; r != nil && r.seq != g.seq && now.Sub(r.at) <= CorrectionWindow && r.digit != m.Digit && r.misread(rec) {
				res.Correction = &Correction{Cell: r.rect, Strokes: r.strokes, Mark: r.mark, Label: m.Digit}
			}
			g.last[cell] = rec
			g.dropAnswer(cell)
			g.ink[cell] = append(clip(g.ink[cell]), g.addInk(strokes, m.Strokes, false, m.Digit)...)
			g.setEntry(cell, m.Digit)
		default:
			res.Unknown = true
		}
	}
	res.Changed = g.commit(&prev, now)
	return res
}

// commit pushes prev onto the undo stack if the board changed since.
func (g *Game) commit(prev *board, now time.Time) bool {
	if g.board.equal(prev) {
		return false
	}
	if len(g.undo) == UndoDepth {
		copy(g.undo, g.undo[1:])
		g.undo = g.undo[:UndoDepth-1]
	}
	g.undo = append(g.undo, *prev)
	g.retime(now)
	return true
}

func (g *Game) setEntry(cell, digit int) {
	if digit != 0 && int(g.entries[cell]) != digit && uint8(digit) != g.puzzle.Solution[cell] {
		g.mistakes++
	}
	g.entries[cell] = uint8(digit)
}

func candidateBit(d uint8) uint16 { return 1 << (d - 1) }

// toggleCandidate flips digit in cell; when it turns on, the ink from add
// (if any) is stored with it.
func (g *Game) toggleCandidate(cell, digit int, add func() []ref) {
	bit := candidateBit(uint8(digit))
	if g.candidates[cell]&bit != 0 {
		g.candidates[cell] &^= bit
		g.dropInk(cell, func(r ref) bool { return r.cand && int(r.digit) == digit })
		return
	}
	g.candidates[cell] |= bit
	if add != nil {
		g.ink[cell] = append(clip(g.ink[cell]), add()...)
	}
}

// erase removes stroke id from cell along with the rest of its digit's ink,
// clearing the entry or candidate it stood for.
func (g *Game) erase(cell, id int) {
	for _, r := range g.ink[cell] {
		if r.id != id {
			continue
		}
		if r.cand {
			g.candidates[cell] &^= candidateBit(r.digit)
			g.dropInk(cell, func(o ref) bool { return o.cand && o.digit == r.digit })
		} else {
			g.dropAnswer(cell)
			g.entries[cell] = 0
			g.last[cell] = nil // a new digit after a scratch-out is a change of mind
		}
		return
	}
}

func (g *Game) dropAnswer(cell int) { g.dropInk(cell, func(r ref) bool { return !r.cand }) }

// dropInk replaces cell's ink with a filtered copy (snapshots share the old).
func (g *Game) dropInk(cell int, drop func(ref) bool) {
	var keep []ref
	for _, r := range g.ink[cell] {
		if !drop(r) {
			keep = append(keep, r)
		}
	}
	g.ink[cell] = keep
}

// addInk stores strokes[idx] and returns refs to them.
func (g *Game) addInk(strokes [][]float32, idx []int, cand bool, digit int) []ref {
	out := make([]ref, 0, len(idx))
	for _, i := range idx {
		g.nextID++
		g.points[g.nextID] = strokes[i]
		out = append(out, ref{id: g.nextID, cand: cand, digit: uint8(digit)})
	}
	return out
}

func (g *Game) answerRecord(cell int, strokes [][]float32, m hwr.Mark, now time.Time) *answerRec {
	rec := &answerRec{seq: g.seq, at: now, digit: m.Digit, rect: g.CellRect(cell), mark: m}
	rec.mark.Strokes = make([]int, len(m.Strokes))
	rec.mark.Erases = nil
	for k, i := range m.Strokes {
		rec.strokes = append(rec.strokes, toStroke(strokes[i], 0))
		rec.mark.Strokes[k] = k
	}
	if b, ok := hwr.Bounds(rec.strokes...); ok {
		pad := rec.rect.W() * 0.03
		rec.box = hwr.Rect{X0: b.X0 - pad, Y0: b.Y0 - pad, X1: b.X1 + pad, Y1: b.Y1 + pad}
	}
	return rec
}

// clip forces the next append to copy, so snapshots sharing s stay intact.
func clip(s []ref) []ref { return s[:len(s):len(s)] }
