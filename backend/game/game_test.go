package game

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"sudoku/engine"
	"sudoku/hwr"
)

var t0 = time.Unix(1_800_000_000, 0)

func newTestGame(t *testing.T) *Game {
	t.Helper()
	g := New(engine.Generate(7, engine.Easy))
	g.SetGridSize(900)
	return g
}

func emptyCells(g *Game) []int {
	var out []int
	for c, v := range g.givens {
		if v == 0 {
			out = append(out, c)
		}
	}
	return out
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

func roles(ink []Ink) (answer, cand int) {
	for _, in := range ink {
		if in.Role == RoleAnswer {
			answer++
		} else {
			cand++
		}
	}
	return
}

func TestApplyMarks(t *testing.T) {
	g := newTestGame(t)
	c := emptyCells(g)[0]
	stroke := dense([2]float32{10, 10}, [2]float32{10, 60})

	r := g.ApplyMarks(c, [][]float32{stroke, stroke}, []hwr.Mark{mark(hwr.Answer, 5, 0, 1)}, t0)
	if !r.Changed || g.Entry(c) != 5 {
		t.Fatalf("answer: changed %v entry %d", r.Changed, g.Entry(c))
	}
	if a, _ := roles(g.CellInk(c)); a != 2 {
		t.Fatalf("answer ink: %d strokes, want 2", a)
	}
	g.ApplyMarks(c, [][]float32{stroke}, []hwr.Mark{mark(hwr.Answer, 3, 0)}, t0)
	if a, _ := roles(g.CellInk(c)); g.Entry(c) != 3 || a != 1 {
		t.Fatalf("rewrite: entry %d, %d answer strokes; want 3 and 1 (old ink replaced)", g.Entry(c), a)
	}

	g.ApplyMarks(c, [][]float32{stroke}, []hwr.Mark{mark(hwr.Candidate, 4, 0)}, t0)
	if a, k := roles(g.CellInk(c)); g.CellCandidates(c) != engine.CandidateBit(4) || a != 1 || k != 1 {
		t.Fatalf("candidate on: mask %b, ink %d+%d", g.CellCandidates(c), a, k)
	}
	g.ApplyMarks(c, [][]float32{stroke}, []hwr.Mark{mark(hwr.Candidate, 4, 0)}, t0)
	if _, k := roles(g.CellInk(c)); g.CellCandidates(c) != 0 || k != 0 {
		t.Fatalf("candidate toggled off: mask %b, %d candidate strokes", g.CellCandidates(c), k)
	}

	if r := g.ApplyMarks(c, [][]float32{stroke}, []hwr.Mark{{Kind: hwr.Unknown, Strokes: []int{0}}}, t0); !r.Unknown || r.Changed {
		t.Fatalf("unknown: %+v", r)
	}

	var given int
	for given = 0; g.givens[given] == 0; given++ {
	}
	before := g.board
	if r := g.ApplyMarks(given, [][]float32{stroke}, []hwr.Mark{mark(hwr.Answer, 1, 0)}, t0); r.Changed || !g.board.equal(&before) {
		t.Fatal("ink on a given cell changed the game")
	}
}

func TestScratchErases(t *testing.T) {
	g := newTestGame(t)
	c := emptyCells(g)[0]
	s := dense([2]float32{10, 10}, [2]float32{10, 60})
	g.ApplyMarks(c, [][]float32{s, s}, []hwr.Mark{mark(hwr.Answer, 5, 0, 1)}, t0)
	g.ApplyMarks(c, [][]float32{s}, []hwr.Mark{mark(hwr.Candidate, 2, 0)}, t0)

	// Stored ink is [answer, answer, candidate 2]; the scratch covers one
	// answer stroke, which takes the whole answer with it.
	r := g.ApplyMarks(c, [][]float32{s}, []hwr.Mark{scratch(1)}, t0)
	if a, k := roles(g.CellInk(c)); !r.Changed || g.Entry(c) != 0 || a != 0 || k != 1 {
		t.Fatalf("scratch over answer: entry %d, ink %d answer + %d candidate", g.Entry(c), a, k)
	}
	g.ApplyMarks(c, [][]float32{s}, []hwr.Mark{scratch(0)}, t0)
	if g.CellCandidates(c) != 0 || len(g.CellInk(c)) != 0 {
		t.Fatalf("scratch over candidate: mask %b, %d strokes left", g.CellCandidates(c), len(g.CellInk(c)))
	}
}

func TestUndo(t *testing.T) {
	g := newTestGame(t)
	cs := emptyCells(g)
	s := dense([2]float32{10, 10}, [2]float32{10, 60})
	type snap struct {
		e     engine.Grid
		cands [81]uint16
		ink   []Ink
	}
	take := func() snap { return snap{g.Entries(), g.Candidates(), g.CellInk(cs[0])} }
	var history []snap
	steps := []func(){
		func() { g.ApplyMarks(cs[0], [][]float32{s}, []hwr.Mark{mark(hwr.Answer, 5, 0)}, t0) },
		func() { g.ApplyMarks(cs[0], [][]float32{s}, []hwr.Mark{mark(hwr.Candidate, 3, 0)}, t0) },
		func() { g.SetCell(cs[1], 7, t0) },
		func() { g.ToggleCandidate(cs[1], 2, t0) },
		func() { g.ApplyMarks(cs[0], [][]float32{s}, []hwr.Mark{scratch(0)}, t0) },
	}
	for _, step := range steps {
		history = append(history, take())
		step()
	}
	for i := len(history) - 1; i >= 0; i-- {
		if !g.Undo(t0) {
			t.Fatalf("undo %d failed", i)
		}
		if got := take(); !reflect.DeepEqual(got, history[i]) {
			t.Fatalf("undo %d: state differs", i)
		}
	}
	if g.Undo(t0) || g.CanUndo() {
		t.Fatal("undo past the start")
	}
}

func TestConflicts(t *testing.T) {
	g := newTestGame(t)
	cs := emptyCells(g)
	c := cs[0]
	var givenDigit uint8
	for _, p := range peers[c] {
		if givenDigit = g.givens[p]; givenDigit != 0 {
			break
		}
	}
	g.SetCell(c, int(givenDigit), t0)
	if got := g.Conflicts(); !reflect.DeepEqual(got, []int{c}) {
		t.Fatalf("entry clashing with a given: conflicts %v, want [%d]", got, c)
	}
	g.SetCell(c, 0, t0)

	auto := engine.AutoCandidates(g.givens)
	for _, a := range cs {
		for _, b := range peers[a] {
			common := auto[a] & auto[b]
			if b < a || g.givens[b] != 0 || common == 0 {
				continue
			}
			d := 1
			for common&engine.CandidateBit(uint8(d)) == 0 {
				d++
			}
			g.SetCell(a, d, t0)
			g.SetCell(b, d, t0)
			if got := g.Conflicts(); !reflect.DeepEqual(got, []int{a, b}) {
				t.Fatalf("two entries of %d: conflicts %v, want [%d %d]", d, got, a, b)
			}
			return
		}
	}
	t.Fatal("no pair of peers to test")
}

func TestSolvedStopsTimer(t *testing.T) {
	g := newTestGame(t)
	g.Connect(t0)
	cs := emptyCells(g)
	for i, c := range cs {
		if g.Solved() {
			t.Fatalf("solved with %d cells left", len(cs)-i)
		}
		g.SetCell(c, int(g.solution[c]), t0.Add(time.Minute))
	}
	if !g.Solved() || len(g.Conflicts()) != 0 {
		t.Fatal("full correct board not solved")
	}
	if e := g.Elapsed(t0.Add(time.Hour)); e != time.Minute {
		t.Fatalf("elapsed %v after solving, want 1m (timer stopped)", e)
	}
}

func TestPersistenceRoundTrip(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	if _, err := st.Load(); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("load with no file: %v", err)
	}
	g := newTestGame(t)
	g.daily = "2026-10-29"
	g.Connect(t0)
	cs := emptyCells(g)
	s := dense([2]float32{10.04, 10}, [2]float32{10, 60})
	g.ApplyMarks(cs[0], [][]float32{s, s}, []hwr.Mark{mark(hwr.Answer, 5, 0, 1)}, t0)
	g.ApplyMarks(cs[0], [][]float32{s}, []hwr.Mark{mark(hwr.Candidate, 3, 0)}, t0)
	g.SetCell(cs[1], 9, t0)
	g.ApplyMarks(cs[0], [][]float32{s}, []hwr.Mark{scratch(0)}, t0) // erased ink lives on in undo
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
		if h.Entries() != g.Entries() || h.Candidates() != g.Candidates() || h.CanUndo() != g.CanUndo() {
			t.Fatalf("%s: board differs", when)
		}
		for c := 0; c < 81; c++ {
			if !reflect.DeepEqual(h.CellInk(c), g.CellInk(c)) {
				t.Fatalf("%s: cell %d ink differs", when, c)
			}
		}
	}
	same("loaded")
	if h.Seed() != g.Seed() || h.Level() != g.Level() || h.Hardest() != g.Hardest() || h.Givens() != g.Givens() ||
		h.solution != g.solution || h.GridSize() != 900 || h.Mistakes() != g.Mistakes() || h.Hints() != 1 || h.Daily() != g.Daily() {
		t.Fatal("puzzle or stats differ")
	}
	if e := h.Elapsed(t0.Add(time.Hour)); e != 5*time.Second {
		t.Fatalf("elapsed %v, want 5s (and stopped until Connect)", e)
	}
	for g.CanUndo() {
		g.Undo(t0)
		h.Undo(t0)
		same("after undo")
	}
	if names, _ := filepath.Glob(filepath.Join(st.Dir, "*")); len(names) != 1 {
		t.Fatalf("data dir holds %v, want only state.json", names)
	}
}

// five reads every glyph as a confident 5, so tests exercise geometry only.
type five struct{}

func (five) Classify([]hwr.Stroke, float32) hwr.Result {
	return hwr.Result{Digit: 5, Confidence: 0.99, Class: '5', Top3: [3]hwr.Guess{{Digit: 5, Prob: 0.99}}}
}

func TestInkRecognition(t *testing.T) {
	a := hwr.Analyzer{Classifier: five{}, Config: hwr.DefaultConfig()}
	g := newTestGame(t)
	c := emptyCells(g)[0]
	r := g.CellRect(c)
	x, y := r.X0, r.Y0
	digit := dense([2]float32{x + 50, y + 15}, [2]float32{x + 50, y + 85})
	var zz [][2]float32
	for i := 0; i <= 7; i++ {
		zz = append(zz, [2]float32{x + 15 + 70*float32(i%2), y + 20 + 9*float32(i)})
	}
	zigzag := dense(zz...)

	res, err := g.Ink(a, c, 900, [][]float32{digit}, t0)
	if err != nil || len(res.Marks) != 1 || res.Marks[0].Kind != hwr.Answer || g.Entry(c) != 5 {
		t.Fatalf("answer: err %v marks %+v entry %d", err, res.Marks, g.Entry(c))
	}
	// A later scratch-out erases the stored answer.
	res, _ = g.Ink(a, c, 900, [][]float32{zigzag}, t0)
	if len(res.Marks) != 1 || res.Marks[0].Kind != hwr.ScratchOut || len(res.Marks[0].Erases) != 0 ||
		!res.Changed || g.Entry(c) != 0 || len(g.CellInk(c)) != 0 {
		t.Fatalf("scratch over stored answer: marks %+v entry %d ink %d", res.Marks, g.Entry(c), len(g.CellInk(c)))
	}
	// Digit and scratch in one message: nothing is stored.
	res, _ = g.Ink(a, c, 900, [][]float32{digit, zigzag}, t0)
	if len(res.Marks) != 1 || !reflect.DeepEqual(res.Marks[0].Erases, []int{0}) || res.Changed {
		t.Fatalf("scratch in the same message: %+v changed %v", res.Marks, res.Changed)
	}
	// Ink well outside the cell is unknown, not dropped silently.
	off := dense([2]float32{x + 350, y + 15}, [2]float32{x + 350, y + 85})
	res, _ = g.Ink(a, c, 900, [][]float32{off}, t0)
	if !res.Unknown || res.Changed || len(res.Marks) != 1 || res.Marks[0].Kind != hwr.Unknown {
		t.Fatalf("off-cell ink: %+v", res)
	}
	if _, err := g.Ink(a, c, 900, [][]float32{{1, 2, 3}}, t0); err == nil {
		t.Fatal("malformed stroke accepted")
	}
}

// A rewrite as a different digit within 20 s is logged as a misread only
// when the earlier read was unsure or the new digit is written over it.
func TestCorrectionLog(t *testing.T) {
	g := newTestGame(t)
	c := emptyCells(g)[0]
	seven := dense([2]float32{30, 20}, [2]float32{70, 20}, [2]float32{45, 80})
	one := dense([2]float32{50, 20}, [2]float32{48, 80}) // over the 7
	aside := dense([2]float32{88, 20}, [2]float32{87, 80})
	sure := func(d int) hwr.Mark {
		m := mark(hwr.Answer, d, 0)
		m.Confidence = 0.999
		return m
	}
	at := t0
	write := func(after time.Duration, s []float32, m hwr.Mark) *Correction {
		at = at.Add(after)
		return g.ApplyMarks(c, [][]float32{s}, []hwr.Mark{m}, at).Correction
	}

	write(0, seven, mark(hwr.Answer, 7, 0)) // read at 0.99: unsure
	r := write(5*time.Second, aside, sure(1))
	if r == nil {
		t.Fatal("unsure 7 rewritten as 1 beside it: no correction")
	}
	if write(5*time.Second, seven, sure(7)) != nil {
		t.Fatal("sure 1 rewritten as 7 beside it: logged, but that's a change of mind")
	}
	if write(5*time.Second, one, sure(1)) == nil {
		t.Fatal("sure 7 rewritten as 1 over it: no correction")
	}
	write(5*time.Second, seven, mark(hwr.Answer, 7, 0))
	write(2*time.Second, seven, scratch(0))
	if write(2*time.Second, one, sure(1)) != nil {
		t.Fatal("unsure 7 scratched out, then 1: logged, but that's a change of mind")
	}
	write(5*time.Second, seven, mark(hwr.Answer, 7, 0))
	if write(21*time.Second, one, sure(1)) != nil {
		t.Fatal("rewrite after 21 s logged as a correction")
	}
	// Undo takes the answer off the page, as a scratch-out does: what's
	// written next is a change of mind, never a misread of ink that's gone.
	write(30*time.Second, seven, sure(7))
	g.Undo(at)
	if write(2*time.Second, one, sure(1)) != nil {
		t.Fatal("sure 7 undone, then 1 written where it was: logged as a misread of the 7")
	}
	write(30*time.Second, seven, sure(7))
	g.SetCell(c, 0, at)
	if write(2*time.Second, one, sure(1)) != nil {
		t.Fatal("sure 7 cleared by setCell, then 1 written where it was: logged as a misread of the 7")
	}

	sm := r.Sample(hwr.DefaultModel(), t0)
	if sm.Label != 1 || sm.Pred != 7 || sm.Source != "correction" || len(sm.Strokes) != 1 ||
		len(sm.Strokes[0]) != len(seven)/4 || sm.Strokes[0][1][0] != float64(seven[4]) {
		t.Fatalf("sample: %+v", sm)
	}
	st := Store{Dir: t.TempDir()}
	if err := st.AppendSample(sm); err != nil {
		t.Fatal(err)
	}
	f, _ := os.Open(filepath.Join(st.Dir, "samples.jsonl"))
	defer f.Close()
	if got, err := hwr.ReadSamples(f); err != nil || len(got) != 1 || got[0].Label != 1 {
		t.Fatalf("samples.jsonl: %v %+v", err, got)
	}
}

func TestQueueTakeIsPrompt(t *testing.T) {
	const slow = 40 * time.Millisecond // stands in for Hard/Expert on the tablet
	var seed atomic.Uint64
	q := newQueue(func(s uint64, l engine.Level) engine.Puzzle {
		time.Sleep(slow)
		return engine.Puzzle{Seed: s, Level: l}
	}, func() uint64 { return seed.Add(1) })
	ready := func(l engine.Level) bool {
		q.mu.Lock()
		defer q.mu.Unlock()
		return q.ready[l] != nil
	}
	waitReady := func(l engine.Level) {
		t.Helper()
		for deadline := time.Now().Add(2 * time.Second); !ready(l); time.Sleep(2 * time.Millisecond) {
			if time.Now().After(deadline) {
				t.Fatalf("%v never pre-generated", l)
			}
		}
	}
	waitReady(engine.Expert)
	start := time.Now()
	p := q.Take(engine.Expert)
	if dt := time.Since(start); dt > slow/4 || p.Level != engine.Expert {
		t.Fatalf("Take took %v (generation takes %v), level %v", dt, slow, p.Level)
	}
	waitReady(engine.Expert) // refilled in the background
	if q.Take(engine.Expert).Seed == p.Seed {
		t.Fatal("refill repeated the puzzle")
	}
}

// A torn last line in a sample log (power lost mid-append) must cost only
// that sample: the ones appended after it still load.
func TestSampleLogSurvivesTornLine(t *testing.T) {
	st := Store{Dir: t.TempDir()}
	sm := hwr.Sample{V: 1, Source: "calibration", Label: 4, Strokes: [][][4]float64{{{1, 2, 1, 0}, {3, 4, 1, 5}}}}
	if err := st.AppendCalibration(sm); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(filepath.Join(st.Dir, "calibration.jsonl"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(`{"v":1,"source":"calibration","label":7,"strokes":[[[1,2`)
	f.Close()
	for _, label := range []int{8, 9} {
		sm.Label = label
		if err := st.AppendCalibration(sm); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := st.LoadSamples()
	var labels []int
	for _, s := range got {
		labels = append(labels, s.Label)
	}
	if !reflect.DeepEqual(labels, []int{4, 8, 9}) {
		t.Fatalf("loaded labels %v, want [4 8 9]", labels)
	}
}

// Samples logged by another game (Ink Kakuro) are learned too, never written.
func TestSharedSamples(t *testing.T) {
	kakuro := Store{Dir: t.TempDir()}
	sm := hwr.Sample{V: 1, Label: 6, Strokes: [][][4]float64{{{1, 2, 0, 1}, {3, 4, 5, 1}}}}
	if err := kakuro.AppendSample(sm); err != nil {
		t.Fatal(err)
	}
	st := Store{Dir: t.TempDir(), Shared: []string{filepath.Join(kakuro.Dir, "samples.jsonl"), filepath.Join(t.TempDir(), "missing.jsonl")}}
	sm.Label = 2
	if err := st.AppendSample(sm); err != nil {
		t.Fatal(err)
	}
	got, err := st.LoadSamples()
	if err != nil || len(got) != 2 || got[0].Label != 2 || got[1].Label != 6 {
		t.Fatalf("loaded %+v, %v; want own 2 then shared 6", got, err)
	}
	if b, _ := os.ReadFile(filepath.Join(kakuro.Dir, "samples.jsonl")); len(b) == 0 || string(b[len(b)-1]) != "\n" {
		t.Fatalf("shared log changed: %q", b)
	}
}

// eliminationGame plays solution digits until the next hint is an
// elimination step, so it can't change the board on its own.
func eliminationGame(t *testing.T) (*Game, engine.Step) {
	t.Helper()
	for seed := uint64(1); seed < 200; seed++ {
		g := New(engine.Generate(seed, engine.Hard))
		for {
			st, ok := g.Hint(false)
			if !ok {
				break
			}
			if st.Placement == nil && len(st.Eliminations) > 0 {
				return g, st
			}
			if st.Placement == nil {
				break // a reveal or mistake: try another puzzle
			}
			g.SetCell(st.Placement.Cell, int(st.Placement.Digit), t0)
		}
	}
	t.Fatal("no puzzle reached an elimination step")
	return nil, engine.Step{}
}

func sameStep(a, b engine.Step) bool {
	return a.Title == b.Title && reflect.DeepEqual(a.Eliminations, b.Eliminations)
}

func TestRevealedEliminationsAreKept(t *testing.T) {
	g, first := eliminationGame(t)
	// Without the game keeping it, the board alone gives the same step again.
	if again, _ := engine.NextStep(g.givens, g.entries, g.candidates); !sameStep(again, first) {
		t.Fatal("setup: the board alone should repeat the step")
	}
	if nudge, _ := g.Hint(false); !sameStep(nudge, first) {
		t.Fatal("a nudge must not consume the step")
	}
	g.Hint(true)
	next, ok := g.Hint(false)
	if !ok || sameStep(next, first) {
		t.Fatalf("after revealing %q the next hint repeated it", first.Title)
	}

	st := Store{Dir: t.TempDir()}
	if err := st.Save(g, t0); err != nil {
		t.Fatal(err)
	}
	h, err := st.Load()
	if err != nil {
		t.Fatal(err)
	}
	if h.ruledOut != g.ruledOut {
		t.Fatal("ruledOut lost in save/load")
	}
	if loaded, _ := h.Hint(false); !sameStep(loaded, next) {
		t.Fatal("a reloaded game gives a different next hint")
	}
}

func TestRuledOutKeepsNoteMistakes(t *testing.T) {
	g, first := eliminationGame(t)
	g.Hint(true)
	e := first.Eliminations[0]
	// Notes holding only the ruled-out digit leave out the answer: still a mistake.
	if _, err := g.ToggleCandidate(e.Cell, int(e.Digit), t0); err != nil {
		t.Fatal(err)
	}
	st, ok := g.Hint(false)
	if !ok || st.Technique != engine.Mistake {
		t.Fatalf("got %v %q, want a Mistake for the notes in cell %d", st.Technique, st.Title, e.Cell)
	}
}
