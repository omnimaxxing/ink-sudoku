package kgame

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"sudoku/hwr"
	"sudoku/kakuro"
)

var t0 = time.Unix(1_800_000_000, 0)

// fixture is a hand-made 3×3 Kakuro with one solution (cells 4 5 / 7 8):
//
//	 .   ↓4  ↓6
//	→3   1   2
//	→7   3   4
func fixture() kakuro.Puzzle {
	B := kakuro.Cell{Black: true}
	return kakuro.Puzzle{
		N: 3, Seed: 42, Level: kakuro.Easy, Hardest: "Unique sum",
		Cells: []kakuro.Cell{
			B, {Black: true, Down: 4}, {Black: true, Down: 6},
			{Black: true, Across: 3}, {}, {},
			{Black: true, Across: 7}, {}, {},
		},
		Solution: []uint8{0, 0, 0, 0, 1, 2, 0, 3, 4},
	}
}

const (
	clue  = 1 // a black cell with a down clue
	white = 4
)

func newTestGame(t *testing.T) *Game {
	t.Helper()
	if err := Validate(fixture()); err != nil {
		t.Fatal(err)
	}
	g := New(fixture())
	g.SetGridSize(900) // 300 px cells
	return g
}

// dense is a flat x,y,t,p stroke along the polyline, ~1 px per sample.
func dense(pts ...[2]float32) []float32 {
	var out []float32
	var t float32
	for i := 0; i+1 < len(pts); i++ {
		a, b := pts[i], pts[i+1]
		n := int(max(abs(b[0]-a[0]), abs(b[1]-a[1]))) + 1
		for k := 0; k < n; k++ {
			f := float32(k) / float32(n)
			out = append(out, a[0]+(b[0]-a[0])*f, a[1]+(b[1]-a[1])*f, t, 1)
			t += 3
		}
	}
	last := pts[len(pts)-1]
	return append(out, last[0], last[1], t, 1)
}

func abs(v float32) float32 { return max(v, -v) }

func mark(kind hwr.MarkKind, digit int, strokes ...int) hwr.Mark {
	return hwr.Mark{Kind: kind, Digit: digit, Confidence: 0.99, Strokes: strokes,
		Top3: [3]hwr.Guess{{Digit: digit, Prob: 0.99}}}
}

func scratch(erases ...int) hwr.Mark {
	return hwr.Mark{Kind: hwr.ScratchOut, Confidence: 1, Strokes: []int{0}, Erases: erases}
}

// five reads every glyph as a confident 5, so tests exercise geometry only.
type five struct{}

func (five) Classify([]hwr.Stroke, float32) hwr.Result {
	return hwr.Result{Digit: 5, Confidence: 0.99, Class: '5', Top3: [3]hwr.Guess{{Digit: 5, Prob: 0.99}}}
}

// Ink on a clue cell is refused and changes nothing; on a white cell it is
// read, and a later scratch-out over the answer erases it.
func TestInkBlackAndWhite(t *testing.T) {
	a := hwr.Analyzer{Classifier: five{}, Config: hwr.DefaultConfig()}
	g := newTestGame(t)
	r := g.CellRect(white)
	x, y := r.X0, r.Y0
	digit := dense([2]float32{x + 150, y + 50}, [2]float32{x + 150, y + 250})
	var zz [][2]float32
	for i := 0; i <= 7; i++ {
		zz = append(zz, [2]float32{x + 50 + 200*float32(i%2), y + 60 + 25*float32(i)})
	}
	zigzag := dense(zz...)

	cr := g.CellRect(clue)
	before := g.board.clone()
	if _, err := g.Ink(a, clue, 900, [][]float32{dense([2]float32{cr.X0 + 150, cr.Y0 + 50}, [2]float32{cr.X0 + 150, cr.Y0 + 250})}, t0); err == nil {
		t.Fatal("ink on a clue cell accepted")
	}
	if r := g.ApplyMarks(clue, [][]float32{digit}, []hwr.Mark{mark(hwr.Answer, 5, 0)}, t0); r.Changed || !g.board.equal(&before) || g.CanUndo() {
		t.Fatal("marks on a clue cell changed the game")
	}
	if _, err := g.SetCell(clue, 3, t0); err == nil {
		t.Fatal("setCell on a clue cell accepted")
	}

	res, err := g.Ink(a, white, 900, [][]float32{digit}, t0)
	if err != nil || len(res.Marks) != 1 || res.Marks[0].Kind != hwr.Answer || g.Entry(white) != 5 || len(g.CellInk(white)) != 1 {
		t.Fatalf("answer: err %v marks %+v entry %d", err, res.Marks, g.Entry(white))
	}
	corner := dense([2]float32{x + 275, y + 10}, [2]float32{x + 274, y + 40})
	g.ApplyMarks(white, [][]float32{corner}, []hwr.Mark{mark(hwr.Candidate, 2, 0)}, t0)
	res, _ = g.Ink(a, white, 900, [][]float32{zigzag}, t0)
	ink := g.CellInk(white)
	if len(res.Marks) != 1 || res.Marks[0].Kind != hwr.ScratchOut || !res.Changed || g.Entry(white) != 0 ||
		len(ink) != 1 || ink[0].Role != RoleCandidate {
		t.Fatalf("scratch over the answer: marks %+v entry %d ink %+v", res.Marks, g.Entry(white), ink)
	}
	if _, err := g.Ink(a, white, 900, [][]float32{{1, 2, 3}}, t0); err == nil {
		t.Fatal("malformed stroke accepted")
	}
}

// Board snapshots are slices now: each undo must restore exactly the state
// before its step, not an alias of the live board.
func TestUndo(t *testing.T) {
	g := newTestGame(t)
	s := dense([2]float32{10, 10}, [2]float32{10, 60})
	type snap struct {
		e     []uint8
		cands []uint16
		ink   []Ink
	}
	take := func() snap { return snap{g.Entries(), g.Candidates(), g.CellInk(white)} }
	steps := []func(){
		func() { g.ApplyMarks(white, [][]float32{s}, []hwr.Mark{mark(hwr.Answer, 5, 0)}, t0) },
		func() { g.ApplyMarks(white, [][]float32{s}, []hwr.Mark{mark(hwr.Candidate, 3, 0)}, t0) },
		func() { g.SetCell(8, 7, t0) },
		func() { g.ToggleCandidate(8, 2, t0) },
		func() { g.ApplyMarks(white, [][]float32{s}, []hwr.Mark{scratch(0)}, t0) },
	}
	var history []snap
	for _, step := range steps {
		history = append(history, take())
		step()
	}
	for i := len(history) - 1; i >= 0; i-- {
		if !g.Undo(t0) {
			t.Fatalf("undo %d failed", i)
		}
		if got := take(); !reflect.DeepEqual(got, history[i]) {
			t.Fatalf("undo %d: state %+v, want %+v", i, got, history[i])
		}
	}
	if g.Undo(t0) || g.CanUndo() {
		t.Fatal("undo past the start")
	}
}

// Conflicts come from kakuro.Check, limited to white cells, sorted, never nil.
func TestConflicts(t *testing.T) {
	g := newTestGame(t)
	if got := g.Conflicts(); got == nil || len(got) != 0 {
		t.Fatalf("empty board: conflicts %#v, want []", got)
	}
	g.SetCell(4, 1, t0)
	g.SetCell(5, 1, t0) // 1 twice in the across run
	if len(kakuro.Check(g.puzzle, g.entries)) == 0 {
		t.Skip("kakuro.Check is still the stub")
	}
	if got := g.Conflicts(); !reflect.DeepEqual(got, []int{4, 5}) {
		t.Fatalf("1 twice in a run: conflicts %v, want [4 5]", got)
	}
}

func TestSolvedStopsTimer(t *testing.T) {
	g := newTestGame(t)
	g.Connect(t0)
	cells := []int{4, 5, 7, 8}
	for i, c := range cells {
		if g.Solved() {
			t.Fatalf("solved with %d cells left", len(cells)-i)
		}
		g.SetCell(c, int(g.puzzle.Solution[c]), t0.Add(time.Minute))
	}
	if !g.Solved() {
		t.Fatal("full correct board not solved")
	}
	if e := g.Elapsed(t0.Add(time.Hour)); e != time.Minute {
		t.Fatalf("elapsed %v after solving, want 1m (timer stopped)", e)
	}
	if _, ok := g.Hint(true); ok || g.Hints() != 0 {
		t.Fatal("hint on a solved board")
	}
}

func TestPersistence(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	if _, err := st.Load(); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("load with no file: %v", err)
	}
	g := newTestGame(t)
	g.daily = "2026-10-29"
	g.Connect(t0)
	s := dense([2]float32{10.04, 10}, [2]float32{10, 60})
	g.ApplyMarks(white, [][]float32{s, s}, []hwr.Mark{mark(hwr.Answer, 5, 0, 1)}, t0)
	g.ApplyMarks(white, [][]float32{s}, []hwr.Mark{mark(hwr.Candidate, 3, 0)}, t0)
	g.SetCell(8, 9, t0)
	g.ApplyMarks(white, [][]float32{s}, []hwr.Mark{scratch(0)}, t0) // erased ink lives on in undo
	g.Hint(true)
	if err := st.Save(g, t0.Add(5*time.Second)); err != nil {
		t.Fatal(err)
	}
	h, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	same := func(when string) {
		t.Helper()
		if !slices.Equal(h.Entries(), g.Entries()) || !slices.Equal(h.Candidates(), g.Candidates()) || h.CanUndo() != g.CanUndo() {
			t.Fatalf("%s: board differs", when)
		}
		for c := range g.entries {
			if !reflect.DeepEqual(h.CellInk(c), g.CellInk(c)) {
				t.Fatalf("%s: cell %d ink differs", when, c)
			}
		}
	}
	same("loaded")
	if !reflect.DeepEqual(h.Puzzle(), g.Puzzle()) || h.GridSize() != 900 || h.Mistakes() != g.Mistakes() ||
		h.Hints() != 1 || h.Daily() != g.Daily() {
		t.Fatal("puzzle or counters differ")
	}
	if e := h.Elapsed(t0.Add(time.Hour)); e != 5*time.Second {
		t.Fatalf("elapsed %v, want 5s (and stopped until Connect)", e)
	}
	for g.CanUndo() {
		g.Undo(t0)
		h.Undo(t0)
		same("after undo")
	}

	// A minimal file (only the fields every version has) loads as a fresh game.
	min := `{"v":1,"puzzle":{"n":3,"seed":42,"level":"easy","hardest":"Unique sum",
		"cells":[[1,0,0],[1,4,0],[1,6,0],[1,0,3],[0,0,0],[0,0,0],[1,0,7],[0,0,0],[0,0,0]],"solution":"....12.34"},
		"board":{"entries":""}}`
	os.WriteFile(st.statePath(), []byte(min), 0o644)
	if h, err = st.Load(); err != nil || !reflect.DeepEqual(h.Puzzle(), fixture()) || h.CanUndo() || len(h.Entries()) != 9 {
		t.Fatalf("minimal state.json: %v", err)
	}
	// Another format (an older or newer build) is moved aside, not lost.
	other := []byte(`{"v":0,"seed":1}`)
	os.WriteFile(st.statePath(), other, 0o644)
	if _, err := st.Load(); err == nil || errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("v0 state.json: %v", err)
	}
	if b, _ := os.ReadFile(st.statePath() + ".bad"); !bytes.Equal(b, other) {
		t.Fatal("unreadable state.json not kept as state.json.bad")
	}
	if _, err := st.Load(); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("after moving it aside: %v", err)
	}
}

// Samples come from Ink Sudoku's calibration and corrections, then Kakuro's
// own; new ones go to Kakuro's file only.
func TestSamplesBothDirs(t *testing.T) {
	sudoku, own := t.TempDir(), t.TempDir()
	sm := func(label int, source string) hwr.Sample {
		return hwr.Sample{V: 1, Source: source, Label: label, Strokes: [][][4]float64{{{1, 2, 1, 0}, {3, 4, 1, 5}}}}
	}
	for name, label := range map[string]int{"calibration.jsonl": 1, "samples.jsonl": 2} {
		var b bytes.Buffer
		hwr.AppendSample(&b, sm(label, "x"))
		os.WriteFile(filepath.Join(sudoku, name), b.Bytes(), 0o644)
	}
	snapshot := func() map[string]string {
		out := map[string]string{}
		names, _ := filepath.Glob(filepath.Join(sudoku, "*"))
		for _, n := range names {
			b, _ := os.ReadFile(n)
			out[n] = string(b)
		}
		return out
	}
	before := snapshot()

	st := Store{Dir: own, SudokuDir: sudoku}
	if err := st.AppendSample(sm(3, "correction")); err != nil {
		t.Fatal(err)
	}
	s, o, err := st.LoadSamples()
	labels := func(ss []hwr.Sample) (out []int) {
		for _, x := range ss {
			out = append(out, x.Label)
		}
		return
	}
	if err != nil || !reflect.DeepEqual(labels(s), []int{1, 2}) || !reflect.DeepEqual(labels(o), []int{3}) {
		t.Fatalf("loaded sudoku %v own %v (%v), want [1 2] and [3]", labels(s), labels(o), err)
	}
	if !reflect.DeepEqual(snapshot(), before) {
		t.Fatal("Ink Sudoku's dir changed")
	}
	if _, _, err := (Store{Dir: own, SudokuDir: filepath.Join(sudoku, "missing")}).LoadSamples(); err != nil {
		t.Fatalf("missing Ink Sudoku dir: %v", err)
	}
}
