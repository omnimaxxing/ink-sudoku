package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"sudoku/kakuro"
	"sudoku/kgame"
)

// TestFixtures records a representative session against the real backend
// (real engine, real hwr reading synthetic strokes) and writes every
// message the backend sent, with the request that caused it, to
// $INK_KAKURO_FIXTURES/session.json, plus session.js (the same as a QML
// .pragma library). From backend/:
//
//	INK_KAKURO_FIXTURES=$PWD/../dev/fixtures-kakuro pixi run go test ./cmd/kakuro -run Fixtures
//
// It skips while kakuro.Generate is still the stub.
func TestFixtures(t *testing.T) {
	out := os.Getenv("INK_KAKURO_FIXTURES")
	if out == "" {
		t.Skip("set INK_KAKURO_FIXTURES=<dir> to write the golden fixtures")
	}
	if _, err := kgame.Generate(kakuro.Generate, 1, kakuro.Easy); err != nil {
		t.Skipf("no engine yet: %v", err)
	}
	recordSession(t, kakuro.Generate, out)
}

func recordSession(t *testing.T, gen kgame.GenFunc, out string) {
	names := map[int32]string{
		msgState: "state", msgMarks: "marks", msgHint: "hint", msgPen: "pen", msgError: "error", msgSolved: "solved",
		msgHello: "hello", msgNewGame: "newGame", msgInk: "ink", msgSetCell: "setCell",
		msgToggleCandidate: "toggleCandidate", msgHintRequest: "hint", msgUndo: "undo", msgSettings: "settings",
	}
	type message struct {
		Type int32           `json:"type"`
		Name string          `json:"name"`
		Body json.RawMessage `json:"body"`
	}
	type step struct {
		Step    int       `json:"step"`
		Note    string    `json:"note"`
		Send    message   `json:"send"`
		Replies []message `json:"replies"`
	}

	// A restored easy game (so the session is deterministic), no samples.
	dir := t.TempDir()
	store := kgame.Store{Dir: dir, SudokuDir: t.TempDir()}
	clock := time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC) // a Thursday: the daily is medium
	easy, err := kgame.Generate(gen, 1234, kakuro.Easy)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Save(kgame.New(easy), clock); err != nil {
		t.Fatal(err)
	}
	var sent []message
	s := newServer(store, func(typ int32, body string) {
		sent = append(sent, message{typ, names[typ], json.RawMessage(body)})
	}, gen)
	s.now = func() time.Time { return clock }
	p := <-s.personalReady // adopt the personal recognizer now, so every read is deterministic
	s.personalReady <- p
	s.adoptPersonal()

	var steps []step
	do := func(record bool, note string, typ int32, body any, want ...string) []map[string]any {
		t.Helper()
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		clock = clock.Add(7 * time.Second)
		sent = nil
		s.handle(typ, string(b))
		var got []string
		var bodies []map[string]any
		for _, m := range sent {
			got = append(got, m.Name)
			var v map[string]any
			if err := json.Unmarshal(m.Body, &v); err != nil {
				t.Fatalf("%s: %s body is not a JSON object: %s", note, m.Name, m.Body)
			}
			bodies = append(bodies, v)
		}
		if strings.Join(got, " ") != strings.Join(want, " ") {
			t.Fatalf("%s: sent %v, want %v\n%v", note, got, want, bodies)
		}
		if record {
			steps = append(steps, step{len(steps) + 1, note, message{typ, names[typ], b}, sent})
		}
		return bodies
	}
	rec := func(note string, typ int32, body any, want ...string) []map[string]any {
		t.Helper()
		return do(true, note, typ, body, want...)
	}
	const grid = 1180.0
	var n int
	var S float64
	var sol []uint8
	var whites []int
	setPuzzle := func(st map[string]any) {
		t.Helper()
		pz := s.g.Puzzle()
		n, sol, whites = pz.N, pz.Solution, nil
		S = grid / float64(n)
		for c, cell := range pz.Cells {
			if !cell.Black {
				whites = append(whites, c)
			}
		}
		if st["puzzle"].(map[string]any)["n"] != float64(n) || len(st["entries"].([]any)) != n*n {
			t.Fatalf("state is not %d×%d: %v", n, n, st["puzzle"])
		}
	}
	center := func(c int) (float64, float64) { return (float64(c%n) + 0.5) * S, (float64(c/n) + 0.52) * S }
	answer := func(c, d int) [][]float64 { x, y := center(c); return glyph(d, x, y, S*0.55) }
	pencil := func(c, d int) [][]float64 { // small, at the digit's keypad spot
		x0, y0 := float64(c%n)*S, float64(c/n)*S
		return glyph(d, x0+S*(0.2+0.3*float64((d-1)%3)), y0+S*(0.2+0.3*float64((d-1)/3)), S*0.23)
	}
	inkID := 0
	ink := func(c int, strokes [][]float64) map[string]any {
		inkID++
		return map[string]any{"id": inkID, "cell": c, "gridSize": grid, "strokes": strokes}
	}
	mark := func(mk map[string]any, i int) map[string]any { return mk["marks"].([]any)[i].(map[string]any) }

	st := rec("hello: the restored game; no handwriting samples", msgHello,
		map[string]any{"screen": []int{1404, 1872}, "gridSize": grid}, "state")[0]
	setPuzzle(st)
	if st["personalSamples"] != 0.0 {
		t.Fatalf("personalSamples %v", st["personalSamples"])
	}
	st = rec("newGame medium (seeded; the frontend sends no seed)", msgNewGame, map[string]any{"level": "medium", "seed": 48213}, "state")[0]
	setPuzzle(st)
	c1, c2, c3 := whites[0], whites[1], whites[2]

	mk := rec("ink: an answer", msgInk, ink(c1, answer(c1, int(sol[c1]))), "marks")[0]
	if m := mark(mk, 0); m["kind"] != "answer" || m["digit"] != float64(sol[c1]) {
		t.Fatalf("answer read as %v", m)
	}
	other := 1 + int(sol[c2])%9
	mk = rec("ink: two pencil marks in one message", msgInk, ink(c2, append(pencil(c2, int(sol[c2])), pencil(c2, other)...)), "marks")[0]
	if len(mk["marks"].([]any)) != 2 || mark(mk, 0)["kind"] != "candidate" || mark(mk, 1)["kind"] != "candidate" {
		t.Fatalf("pencil marks read as %v", mk["marks"])
	}
	x, y := center(c1)
	mk = rec("ink: a scratch-out over the answer", msgInk, ink(c1, [][]float64{zigzag(x-32, y-40, x+32, y+40, 6)}), "marks")[0]
	if mark(mk, 0)["kind"] != "scratch" || mk["cellState"].(map[string]any)["entry"] != 0.0 {
		t.Fatalf("scratch-out: %v", mk)
	}
	x, y = center(c3)
	mk = rec("ink: an 'o', not a digit (unknown)", msgInk, ink(c3, [][]float64{ring(x, y, S*0.28)}), "marks")[0]
	if mk["unknown"] != true || mark(mk, 0)["kind"] != "unknown" {
		t.Fatalf("o read as %v", mk)
	}
	clue := n + 0 // row 1, column 0: always a clue cell
	if e := rec("ink in a clue cell (refused: an error naming the ink)", msgInk, ink(clue, answer(clue, 5)), "error")[0]; e["id"] != float64(inkID) {
		t.Fatalf("clue cell: %v", e)
	}
	h1 := rec("hint nudge (first press)", msgHintRequest, map[string]any{"reveal": false}, "hint")[0]
	h2 := rec("hint reveal (second press)", msgHintRequest, map[string]any{"reveal": true}, "hint")[0]
	if fmt.Sprint(h1) != fmt.Sprint(h2) {
		t.Fatalf("nudge %v, reveal %v", h1, h2)
	}
	st = rec("undo: the scratch-out", msgUndo, map[string]any{}, "state")[0]
	if st["entries"].([]any)[c1] != float64(sol[c1]) {
		t.Fatalf("undo: %v", st["entries"])
	}

	st = rec("newGame daily", msgNewGame, map[string]any{"daily": true}, "state")[0]
	setPuzzle(st)
	rec("newGame daily again (progress kept)", msgNewGame, map[string]any{"daily": true}, "state")

	// A digit repeated in an across run: the neighbour's answer, written again.
	ca := -1
	for _, c := range whites {
		if slices.Contains(whites, c+1) && (c+1)%n != 0 {
			ca = c
			break
		}
	}
	cb := ca + 1
	rec("setCell: the neighbour's answer", msgSetCell, map[string]any{"cell": cb, "digit": sol[cb]}, "state")
	mk = rec("ink: a wrong answer that repeats its neighbour's digit in the run", msgInk, ink(ca, answer(ca, int(sol[cb]))), "marks")[0]
	if mark(mk, 0)["digit"] != float64(sol[cb]) || !slices.Contains(mk["conflicts"].([]any), any(float64(ca))) {
		t.Fatalf("wrong answer: %v", mk)
	}
	rec("hint on a board with a mistake", msgHintRequest, map[string]any{"reveal": false}, "hint")
	rec("undo: the wrong answer", msgUndo, map[string]any{}, "state")

	rest := slices.DeleteFunc(slices.Clone(whites), func(c int) bool { return c == cb })
	last, prev := rest[len(rest)-1], rest[len(rest)-2]
	for _, c := range rest[:len(rest)-2] {
		do(false, "setCell", msgSetCell, map[string]any{"cell": c, "digit": sol[c]}, "state")
	}
	rec(fmt.Sprintf("setCell (after %d unrecorded ones): one cell left", len(rest)-2), msgSetCell,
		map[string]any{"cell": prev, "digit": sol[prev]}, "state")
	mk = rec("ink: the last answer solves it", msgInk, ink(last, answer(last, int(sol[last]))), "marks", "solved")[0]
	if mark(mk, 0)["digit"] != float64(sol[last]) {
		t.Fatalf("last answer read as %v", mark(mk, 0))
	}
	rec("hint on the solved board", msgHintRequest, map[string]any{"reveal": false}, "error")
	rec("undo: unsolved again", msgUndo, map[string]any{}, "state")
	rec("setCell solves it again (counted once)", msgSetCell, map[string]any{"cell": last, "digit": sol[last]}, "state", "solved")
	over := 1 + int(sol[last])%9
	mk = rec("ink: another digit over the last answer (unsolved again)", msgInk, ink(last, answer(last, over)), "marks")[0]
	if mk["cellState"].(map[string]any)["entry"] != float64(over) {
		t.Fatalf("overwrite read as %v", mk["marks"])
	}
	rec("newGame expert (a 13×13 grid)", msgNewGame, map[string]any{"level": "expert", "seed": 777}, "state")
	rec("newGame with an unknown level", msgNewGame, map[string]any{"level": "beyond"}, "error")

	// One message per line, so regenerated fixtures diff well.
	var b bytes.Buffer
	b.WriteString("[\n")
	for i, st := range steps {
		send, _ := json.Marshal(st.Send)
		note, _ := json.Marshal(st.Note)
		fmt.Fprintf(&b, "{\"step\": %d, \"note\": %s,\n \"send\": %s,\n \"replies\": [", st.Step, note, send)
		for k, m := range st.Replies {
			r, _ := json.Marshal(m)
			if k > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, "\n  %s", r)
		}
		b.WriteString("]}")
		if i < len(steps)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("]\n")
	if !json.Valid(b.Bytes()) {
		t.Fatal("session.json: invalid JSON")
	}
	js := "// Generated by TestFixtures (backend/cmd/kakuro/fixtures_test.go) from the real backend; don't edit.\n" +
		".pragma library\n\nvar steps = " + b.String()
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{"session.json": b.String(), "session.js": js} {
		if err := os.WriteFile(filepath.Join(out, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("wrote %d steps to %s", len(steps), out)
}
