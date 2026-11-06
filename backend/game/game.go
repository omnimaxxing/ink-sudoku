// Package game is one Ink Sudoku game: the puzzle, the player's entries,
// candidates and ink, the undo history and the timer, plus the rules that
// change them. It does no I/O except Store's load and save.
package game

import (
	"fmt"
	"time"

	"sudoku/engine"
	"sudoku/hwr"
)

// UndoDepth caps the undo history.
const UndoDepth = 200

// Correction logging: an answer rewritten in the same cell within
// CorrectionWindow as a different digit is logged as a misread of the
// earlier strokes when the earlier read was unsure (Confidence below
// CorrectionConfidence) or the new answer is written over the old one (at
// least CorrectionOverlap of its ink box inside the old ink's). Otherwise,
// and always after a scratch-out of the old answer, it's a change of mind.
const (
	CorrectionWindow     = 20 * time.Second
	CorrectionConfidence = 0.995
	CorrectionOverlap    = 0.5
)

// MaxGap caps what one stretch between timer events (player messages, the
// board shown or hidden, the display sleeping or waking) adds, in case a
// sleep goes unnoticed. Thinking time counts: the cap is far above it.
const MaxGap = 30 * time.Minute

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

// board is the undoable part of a game. Ink slices are never modified in
// place, so a copy of a board is a snapshot.
type board struct {
	entries    engine.Grid
	candidates [81]uint16
	ink        [81][]ref
}

func (b *board) equal(o *board) bool {
	if b.entries != o.entries || b.candidates != o.candidates {
		return false
	}
	for c := range b.ink {
		if len(b.ink[c]) != len(o.ink[c]) {
			return false
		}
		for i := range b.ink[c] {
			if b.ink[c][i] != o.ink[c][i] {
				return false
			}
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
	seed     uint64
	level    engine.Level
	hardest  string
	givens   engine.Grid
	solution engine.Grid
	daily    string // date of the daily puzzle, "" for other games
	recorded bool   // the solve has been counted in Stats
	byNumber bool   // started from a written puzzle number: kept out of Stats

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
	ruledOut [81]uint16

	elapsed   time.Duration
	since     time.Time // start of the current running stretch; zero when stopped
	connected bool
	away      bool // the board is off screen: another page is up, or the display sleeps

	seq  int // ApplyMarks calls
	last [81]*answerRec
}

// New starts a game on p.
func New(p engine.Puzzle) *Game {
	g := &Game{
		seed:     p.Seed,
		level:    p.Level,
		givens:   p.Givens,
		solution: p.Solution,
		points:   map[int][]float32{},
	}
	if n := len(p.Techniques); n > 0 {
		g.hardest = p.Techniques[n-1].String()
	}
	return g
}

// NewByNumber starts p, asked for by its puzzle number. It may be a puzzle
// the player has solved before, so it never counts in Stats.
func NewByNumber(p engine.Puzzle) *Game {
	g := New(p)
	g.byNumber = true
	return g
}

// NewDaily starts the daily puzzle p for date ("2006-01-02").
func NewDaily(p engine.Puzzle, date string) *Game {
	g := New(p)
	g.daily = date
	return g
}

func (g *Game) Seed() uint64                   { return g.seed }
func (g *Game) Daily() string                  { return g.daily }
func (g *Game) ByNumber() bool                 { return g.byNumber }
func (g *Game) Level() engine.Level            { return g.level }
func (g *Game) Hardest() string                { return g.hardest }
func (g *Game) Givens() engine.Grid            { return g.givens }
func (g *Game) Entries() engine.Grid           { return g.entries }
func (g *Game) Candidates() [81]uint16         { return g.candidates }
func (g *Game) CanUndo() bool                  { return len(g.undo) > 0 }
func (g *Game) Mistakes() int                  { return g.mistakes }
func (g *Game) Hints() int                     { return g.hints }
func (g *Game) GridSize() float32              { return g.gridSize }
func (g *Game) Entry(cell int) int             { return int(g.entries[cell]) }
func (g *Game) CellCandidates(cell int) uint16 { return g.candidates[cell] }

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

// Solved reports whether givens plus entries are the solution.
func (g *Game) Solved() bool {
	for c := range g.solution {
		v := g.givens[c]
		if v == 0 {
			v = g.entries[c]
		}
		if v != g.solution[c] {
			return false
		}
	}
	return true
}

// Conflicts lists the entered (non-given) cells whose digit also appears in
// their row, column or box (never nil).
func (g *Game) Conflicts() []int {
	out := []int{}
	for c, v := range g.entries {
		if v == 0 || g.givens[c] != 0 {
			continue
		}
		for _, p := range peers[c] {
			if g.givens[p] == v || g.entries[p] == v {
				out = append(out, c)
				break
			}
		}
	}
	return out
}

var peers [81][]int

func init() {
	for c := 0; c < 81; c++ {
		for p := 0; p < 81; p++ {
			if p != c && (p/9 == c/9 || p%9 == c%9 || (p/27 == c/27 && p%9/3 == c%9/3)) {
				peers[c] = append(peers[c], p)
			}
		}
	}
}

// Elapsed is the play time so far.
func (g *Game) Elapsed(now time.Time) time.Duration {
	if g.since.IsZero() {
		return g.elapsed
	}
	return g.elapsed + gap(g.since, now)
}

// Connect starts the timer (a frontend is attached); Disconnect stops it.
// SetAway pauses it while the board is off screen. The timer also stops
// while the puzzle is solved.
func (g *Game) Connect(now time.Time)    { g.connected = true; g.retime(now) }
func (g *Game) Disconnect(now time.Time) { g.connected = false; g.retime(now) }
func (g *Game) SetAway(away bool, now time.Time) {
	g.away = away
	g.retime(now)
}

// Touch notes a player message at now. While the timer runs, the time since
// the previous one (or since Connect) counts, up to MaxGap.
func (g *Game) Touch(now time.Time) { g.retime(now) }

func (g *Game) retime(now time.Time) {
	if !g.since.IsZero() {
		g.elapsed += gap(g.since, now)
		g.since = time.Time{}
	}
	if g.connected && !g.away && !g.Solved() {
		g.since = now
	}
}

// gap is the play time between two timer events: at most MaxGap, and never
// negative (the clock may be set back). It is wall time, not Go's monotonic
// clock: the tablet suspends between strokes with the page on screen, the
// monotonic clock stops then, and that thinking time counts.
func gap(from, to time.Time) time.Duration {
	return min(max(to.Round(0).Sub(from.Round(0)), 0), MaxGap)
}

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
		g.last = [81]*answerRec{}
	}
	g.gridSize = px
}

// CellRect is cell's square in grid-local pixels.
func (g *Game) CellRect(cell int) hwr.Rect {
	s := g.gridSize / 9
	x, y := float32(cell%9)*s, float32(cell/9)*s
	return hwr.Rect{X0: x, Y0: y, X1: x + s, Y1: y + s}
}

func validCell(c int) bool { return c >= 0 && c < 81 }

func checkCell(cell, digit, lo int) error {
	if !validCell(cell) {
		return fmt.Errorf("cell %d out of range 0..80", cell)
	}
	if digit < lo || digit > 9 {
		return fmt.Errorf("digit %d out of range %d..9", digit, lo)
	}
	return nil
}

// SetCell types digit (0 clears) into cell, dropping its answer ink. It
// reports whether anything changed; givens never change.
func (g *Game) SetCell(cell, digit int, now time.Time) (bool, error) {
	if err := checkCell(cell, digit, 0); err != nil {
		return false, err
	}
	if g.givens[cell] != 0 {
		return false, nil
	}
	prev := g.board
	if int(g.entries[cell]) != digit {
		g.dropAnswer(cell)
		g.setEntry(cell, digit)
		g.last[cell] = nil // its ink is gone: what's written next is a change of mind
	}
	return g.commit(&prev, now), nil
}

// ToggleCandidate flips digit's pencil mark in cell. Turning it off also
// drops that digit's candidate ink.
func (g *Game) ToggleCandidate(cell, digit int, now time.Time) (bool, error) {
	if err := checkCell(cell, digit, 1); err != nil {
		return false, err
	}
	if g.givens[cell] != 0 {
		return false, nil
	}
	prev := g.board
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
	g.last = [81]*answerRec{} // what's written next is a change of mind, as after a scratch-out
	g.retime(now)
	return true
}

// Hint returns the next logical step from the player's position; ok is
// false once the puzzle is solved. It counts as a hint when reveal is set
// (the player sees the whole step), not for a nudge (its title alone).
func (g *Game) Hint(reveal bool) (engine.Step, bool) {
	st, ok := engine.NextStep(g.givens, g.entries, g.hintCandidates())
	if ok && reveal {
		g.hints++
		for _, e := range st.Eliminations {
			g.ruledOut[e.Cell] |= engine.CandidateBit(e.Digit)
		}
	}
	return st, ok
}

// hintCandidates is the player's pencil marks minus what revealed hints ruled
// out. A cell with marks never drops to 0 (NextStep's "no marks"), so marks
// that leave out the answer are still reported as a mistake.
func (g *Game) hintCandidates() [81]uint16 {
	const all = 1<<9 - 1
	var out [81]uint16
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

// ApplyMarks applies the marks recognised in one ink message to cell.
// strokes are the message's strokes (x, y, t, p, … in grid px) and each
// mark's Strokes index them. A scratch-out's Erases index the cell's stored
// ink as it was before this call (CellInk order) followed by strokes, which
// is what hwr.Analyze returns when given stored ++ new ink.
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
// Ink on a given cell changes nothing.
func (g *Game) ApplyMarks(cell int, strokes [][]float32, marks []hwr.Mark, now time.Time) Applied {
	var res Applied
	g.seq++
	if !validCell(cell) || g.givens[cell] != 0 {
		return res
	}
	prev := g.board
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
	if digit != 0 && int(g.entries[cell]) != digit && uint8(digit) != g.solution[cell] {
		g.mistakes++
	}
	g.entries[cell] = uint8(digit)
}

// toggleCandidate flips digit in cell; when it turns on, the ink from add
// (if any) is stored with it.
func (g *Game) toggleCandidate(cell, digit int, add func() []ref) {
	bit := engine.CandidateBit(uint8(digit))
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
			g.candidates[cell] &^= engine.CandidateBit(r.digit)
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
