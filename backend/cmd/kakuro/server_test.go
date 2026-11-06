package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"sudoku/hwr"
	"sudoku/kakuro"
	"sudoku/kgame"
)

// fixture is a hand-made 3×3 Kakuro with one solution (cells 4 5 / 7 8):
//
//	 .   ↓4  ↓6
//	→3   1   2
//	→7   3   4
func fixture(seed uint64, level kakuro.Level) kakuro.Puzzle {
	B := kakuro.Cell{Black: true}
	return kakuro.Puzzle{
		N: 3, Seed: seed, Level: level, Hardest: "Unique sum",
		Cells: []kakuro.Cell{
			B, {Black: true, Down: 4}, {Black: true, Down: 6},
			{Black: true, Across: 3}, {}, {},
			{Black: true, Across: 7}, {}, {},
		},
		Solution: []uint8{0, 0, 0, 0, 1, 2, 0, 3, 4},
	}
}

var whites = []int{4, 5, 7, 8}

type sent struct {
	typ  int32
	body map[string]any
}

// script reads glyphs as the given results, in turn (the last one repeats).
type script struct {
	results []hwr.Result
	n       int
}

func (s *script) Classify([]hwr.Stroke, float32) hwr.Result {
	r := s.results[min(s.n, len(s.results)-1)]
	s.n++
	return r
}

func read(d int, conf float32) hwr.Result {
	return hwr.Result{Digit: d, Confidence: conf, Class: byte('0' + d), Top3: [3]hwr.Guess{{Digit: d, Prob: conf}}}
}

func hasKeys(t *testing.T, what string, m map[string]any, keys ...string) {
	t.Helper()
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			t.Fatalf("%s: missing %q in %v", what, k, m)
		}
	}
}

// harness drives a server through handle with a fixed clock. Replies may
// also come from the server's first-game goroutine, hence the lock.
type harness struct {
	t     *testing.T
	s     *server
	clock time.Time

	mu  sync.Mutex
	out []sent
}

func newHarness(t *testing.T, store kgame.Store, gen kgame.GenFunc) *harness {
	h := &harness{t: t, clock: time.Now()} // today, so the daily is the one generated at startup
	h.s = newServer(store, func(typ int32, body string) {
		var m map[string]any
		if err := json.Unmarshal([]byte(body), &m); err != nil {
			t.Errorf("type %d: body is not a JSON object: %s", typ, body)
		}
		h.mu.Lock()
		h.out = append(h.out, sent{typ, m})
		h.mu.Unlock()
	}, gen)
	h.s.now = func() time.Time { return h.clock }
	h.s.analyzer = hwr.Analyzer{Classifier: &script{results: []hwr.Result{read(5, 0.99)}}, Config: hwr.DefaultConfig()}
	return h
}

func (h *harness) take() []sent {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := h.out
	h.out = nil
	return out
}

// call sends one message and returns the reply bodies, which must have the
// wanted types.
func (h *harness) call(typ int32, body string, want ...int32) []map[string]any {
	h.t.Helper()
	h.take()
	h.s.handle(typ, body)
	out := h.take()
	if len(out) != len(want) {
		h.t.Fatalf("message %d: sent %+v, want types %v", typ, out, want)
	}
	var bodies []map[string]any
	for i, w := range want {
		if out[i].typ != w {
			h.t.Fatalf("message %d: reply %d is type %d (%v), want %d", typ, i, out[i].typ, out[i].body, w)
		}
		bodies = append(bodies, out[i].body)
	}
	return bodies
}

// saveGame leaves an easy game (seed 7) in dir for hello to restore, so
// tests don't race the queue for a first puzzle (see TestHelloNeverBlocks).
func saveGame(t *testing.T, dir string) {
	t.Helper()
	if err := (kgame.Store{Dir: dir}).Save(kgame.New(fixture(7, kakuro.Easy)), time.Now()); err != nil {
		t.Fatal(err)
	}
}

// stroke is a tall vertical line in the middle of cell (300 px cells): an answer.
func stroke(cell int) string {
	x, y := cell%3*300, cell/3*300
	var pts []string
	for i := 0; i <= 200; i++ {
		pts = append(pts, fmt.Sprintf("%d,%d,%d,1", x+150, y+50+i, i*3))
	}
	return "[" + strings.Join(pts, ",") + "]"
}

func inkMsg(id, cell int) string {
	return fmt.Sprintf(`{"id": %d, "cell": %d, "gridSize": 900, "strokes": [%s]}`, id, cell, stroke(cell))
}

func checkState(t *testing.T, st map[string]any) {
	t.Helper()
	hasKeys(t, "state", st, "puzzle", "entries", "candidates", "conflicts", "ink", "elapsedMs", "solved", "canUndo",
		"stats", "personalSamples")
	pz := st["puzzle"].(map[string]any)
	hasKeys(t, "state.puzzle", pz, "number", "seed", "level", "hardest", "daily", "n", "cells")
	if pz["n"] != 3.0 || len(pz["cells"].([]any)) != 9 || len(st["entries"].([]any)) != 9 || len(st["candidates"].([]any)) != 9 {
		t.Fatalf("state: not 3×3: %v", st)
	}
	cells := pz["cells"].([]any)
	if !reflect.DeepEqual(cells[1], map[string]any{"b": 1.0, "d": 4.0, "a": 0.0}) || !reflect.DeepEqual(cells[4], map[string]any{"b": 0.0}) {
		t.Fatalf("cells: %v", cells)
	}
	_ = st["conflicts"].([]any)
	_ = st["ink"].(map[string]any)
	hasKeys(t, "stats", st["stats"].(map[string]any), "easy", "medium", "hard", "expert", "daily")
}

// TestHandlerShapes drives hello (restoring a saved game), newGame, ink
// (white and clue cell), hint, undo, the daily and a solve through handle
// and checks the replies' shapes.
func TestHandlerShapes(t *testing.T) {
	dir := t.TempDir()
	saveGame(t, dir)
	h := newHarness(t, kgame.Store{Dir: dir}, fixture)
	st := h.call(msgHello, `{"screen": [1404, 1872], "gridSize": 900}`, msgState)[0]
	checkState(t, st)
	if pz := st["puzzle"].(map[string]any); pz["seed"] != 7.0 || pz["level"] != "easy" {
		t.Fatalf("hello: not the saved game: %v", pz)
	}
	if again := h.call(msgHello, `{"gridSize": 900}`, msgState)[0]; !reflect.DeepEqual(again["puzzle"], st["puzzle"]) {
		t.Fatal("a repeated hello started another game")
	}

	st = h.call(msgNewGame, `{"level": "medium"}`, msgState)[0]
	checkState(t, st)
	if pz := st["puzzle"].(map[string]any); pz["level"] != "medium" || pz["number"] != pz["seed"] || pz["daily"] != "" || st["canUndo"] != false {
		t.Fatalf("newGame: %v", st)
	}

	mk := h.call(msgInk, inkMsg(17, 4), msgMarks)[0]
	hasKeys(t, "marks", mk, "id", "cell", "marks", "cellState", "conflicts", "unknown", "canUndo")
	if mk["id"] != 17.0 || mk["cell"] != 4.0 || mk["unknown"] != false || mk["canUndo"] != true {
		t.Fatalf("marks: %v", mk)
	}
	m0 := mk["marks"].([]any)[0].(map[string]any)
	hasKeys(t, "mark", m0, "kind", "digit", "confidence", "top3", "bbox", "strokes", "erases")
	if m0["kind"] != "answer" || m0["digit"] != 5.0 {
		t.Fatalf("mark: %v", m0)
	}
	if cs := mk["cellState"].(map[string]any); cs["entry"] != 5.0 || len(cs["ink"].([]any)) != 1 {
		t.Fatalf("cellState: %v", cs)
	}
	if e := h.call(msgInk, inkMsg(18, 1), msgError)[0]; e["id"] != 18.0 || !strings.Contains(e["message"].(string), "clue") {
		t.Fatalf("ink on a clue cell: %v", e)
	}
	if e := h.call(msgInk, `{"id": 19, "cell": 9, "strokes": []}`, msgError)[0]; e["id"] != 19.0 {
		t.Fatalf("ink out of range: %v", e)
	}
	if e := h.call(msgSetCell, `{"cell": 3, "digit": 2}`, msgError)[0]; e["id"] != nil {
		t.Fatalf("setCell on a clue cell: %v", e)
	}

	nudge := h.call(msgHintRequest, `{"reveal": false}`, msgHint)[0]
	hasKeys(t, "hint", nudge, "technique", "title", "text")
	if h.s.g.Hints() != 0 {
		t.Fatal("a nudge counted")
	}
	if full := h.call(msgHintRequest, `{"reveal": true}`, msgHint)[0]; !reflect.DeepEqual(full, nudge) || h.s.g.Hints() != 1 {
		t.Fatalf("reveal: hints %d, step %v (nudge was %v)", h.s.g.Hints(), full, nudge)
	}
	h.call(msgHintRequest, `{}`, msgHint)
	if h.s.g.Hints() != 2 {
		t.Fatalf("hint {}: hints %d, want 2", h.s.g.Hints())
	}

	st = h.call(msgUndo, `{}`, msgState)[0]
	if st["entries"].([]any)[4] != 0.0 || len(st["ink"].(map[string]any)) != 0 || st["canUndo"] != false {
		t.Fatalf("undo: %v", st)
	}

	daily := h.call(msgNewGame, `{"daily": true}`, msgState)[0]["puzzle"].(map[string]any)
	if date, _, _ := kgame.Daily(h.clock); daily["daily"] != date {
		t.Fatalf("daily: %v", daily)
	}
	for i, c := range whites {
		want := []int32{msgState}
		if i == len(whites)-1 {
			want = append(want, msgSolved)
		}
		r := h.call(msgSetCell, fmt.Sprintf(`{"cell": %d, "digit": %d}`, c, fixture(0, 0).Solution[c]), want...)
		if len(r) == 2 {
			hasKeys(t, "solved", r[1], "elapsedMs", "mistakes", "hints", "best", "streak")
			if r[0]["solved"] != true || r[1]["streak"] != 1.0 {
				t.Fatalf("solve: %v then %v", r[0], r[1])
			}
		}
	}
	h.call(msgHintRequest, `{}`, msgError) // solved
	h.call(msgNewGame, `{"level": "beyond"}`, msgError)
	h.call(109, `{}`, msgError) // no calibration in Kakuro
	h.call(msgLog, `{"message": "hi"}`)
	if _, err := os.Stat(filepath.Join(dir, "state.json")); err != nil {
		t.Fatalf("nothing saved: %v", err)
	}
}

// hello must not wait for the generator: with no puzzle ready it replies
// nothing, and the state follows on its own once the puzzle is generated.
func TestHelloNeverBlocks(t *testing.T) {
	release := make(chan struct{})
	h := newHarness(t, kgame.Store{Dir: t.TempDir()}, func(seed uint64, level kakuro.Level) kakuro.Puzzle {
		<-release
		return fixture(seed, level)
	})
	h.call(msgHello, `{"gridSize": 900}`)
	h.call(msgHello, `{"gridSize": 900}`) // the frontend's repeat: still nothing, and no second game
	close(release)
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(time.Millisecond) {
		h.mu.Lock()
		n := len(h.out)
		h.mu.Unlock()
		if n > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no state after the puzzle was generated")
		}
	}
	h.s.mu.Lock() // the goroutine that sent it is done
	h.s.mu.Unlock()
	out := h.take()
	if len(out) != 1 || out[0].typ != msgState {
		t.Fatalf("sent %+v, want one state", out)
	}
	checkState(t, out[0].body)
	if p := out[0].body["stats"].(map[string]any)["easy"].(map[string]any)["played"]; p != 1.0 {
		t.Fatalf("easy played %v, want 1", p)
	}
}

// Samples come from Ink Sudoku's files and Kakuro's own; a correction is
// learned and appended to Kakuro's file only.
func TestSamplesFromBothDirs(t *testing.T) {
	sudoku, own := t.TempDir(), t.TempDir()
	sm := func(label int) hwr.Sample {
		var pts [][4]float64
		for k := 0; k <= 40; k++ {
			pts = append(pts, [4]float64{100 + float64(label), 60 + 3*float64(k), 1, 4 * float64(k)})
		}
		return hwr.Sample{V: 1, Source: "calibration", Label: label, Strokes: [][][4]float64{pts}}
	}
	write := func(path string, labels ...int) {
		var b bytes.Buffer
		for _, l := range labels {
			hwr.AppendSample(&b, sm(l))
		}
		if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(filepath.Join(sudoku, "calibration.jsonl"), 1, 2)
	write(filepath.Join(sudoku, "samples.jsonl"), 3)
	write(filepath.Join(own, "samples.jsonl"), 4)
	sudokuBefore, _ := os.ReadDir(sudoku)
	calBefore, _ := os.ReadFile(filepath.Join(sudoku, "calibration.jsonl"))
	samplesBefore, _ := os.ReadFile(filepath.Join(sudoku, "samples.jsonl"))

	saveGame(t, own)
	h := newHarness(t, kgame.Store{Dir: own, SudokuDir: sudoku}, fixture)
	// An unsure 7, rewritten as a 1 seconds later: a misread.
	h.s.analyzer.Classifier = &script{results: []hwr.Result{read(7, 0.9), read(1, 0.999)}}
	if n := h.call(msgHello, `{"gridSize": 900}`, msgState)[0]["personalSamples"]; n != 4.0 {
		t.Fatalf("personalSamples %v, want 4 (3 from Ink Sudoku + 1)", n)
	}
	h.call(msgInk, inkMsg(1, 4), msgMarks)
	h.clock = h.clock.Add(5 * time.Second)
	h.call(msgInk, inkMsg(2, 4), msgMarks)
	if n := h.call(msgHello, `{"gridSize": 900}`, msgState)[0]["personalSamples"]; n != 5.0 {
		t.Fatalf("personalSamples %v after a correction, want 5", n)
	}

	f, err := os.Open(filepath.Join(own, "samples.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	got, err := hwr.ReadSamples(f)
	f.Close()
	if err != nil || len(got) != 2 || got[1].Source != "correction" || got[1].Label != 1 || got[1].Pred != 7 {
		t.Fatalf("Kakuro samples.jsonl: %v %+v", err, got)
	}
	sudokuAfter, _ := os.ReadDir(sudoku)
	calAfter, _ := os.ReadFile(filepath.Join(sudoku, "calibration.jsonl"))
	samplesAfter, _ := os.ReadFile(filepath.Join(sudoku, "samples.jsonl"))
	if len(sudokuAfter) != len(sudokuBefore) || !bytes.Equal(calAfter, calBefore) || !bytes.Equal(samplesAfter, samplesBefore) {
		t.Fatal("Ink Sudoku's dir changed")
	}
}
