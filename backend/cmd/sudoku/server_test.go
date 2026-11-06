package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"sudoku/engine"
	"sudoku/game"
	"sudoku/hwr"
)

type sent struct {
	typ  int32
	body map[string]any
}

// five reads every glyph as a confident 5.
type five struct{}

func (five) Classify([]hwr.Stroke, float32) hwr.Result {
	return hwr.Result{Digit: 5, Confidence: 0.99, Class: '5', Top3: [3]hwr.Guess{{Digit: 5, Prob: 0.99}, {Digit: 6, Prob: 0.005}, {Digit: 3, Prob: 0.003}}}
}

func hasKeys(t *testing.T, what string, m map[string]any, keys ...string) {
	t.Helper()
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			t.Fatalf("%s: missing %q in %v", what, k, m)
		}
	}
}

// TestHandlerFlow drives hello -> newGame -> ink -> undo -> hint through
// handle and checks the replies against PROTOCOL.md.
func TestHandlerFlow(t *testing.T) {
	dir := t.TempDir()
	var out []sent
	s := newServer(game.Store{Dir: dir}, func(typ int32, body string) {
		var m map[string]any
		if err := json.Unmarshal([]byte(body), &m); err != nil {
			t.Fatalf("type %d: body is not a JSON object: %s", typ, body)
		}
		out = append(out, sent{typ, m})
	})
	s.analyzer = hwr.Analyzer{Classifier: five{}, Config: hwr.DefaultConfig()}
	call := func(typ int32, body string, want ...int32) []sent {
		t.Helper()
		out = nil
		s.handle(typ, body)
		if len(out) != len(want) {
			t.Fatalf("message %d: sent %d replies %+v, want types %v", typ, len(out), out, want)
		}
		for i, w := range want {
			if out[i].typ != w {
				t.Fatalf("message %d: reply %d is type %d (%v), want %d", typ, i, out[i].typ, out[i].body, w)
			}
		}
		return out
	}
	checkState := func(st map[string]any) {
		t.Helper()
		hasKeys(t, "state", st, "puzzle", "entries", "candidates", "conflicts", "ink", "elapsedMs", "solved", "canUndo")
		hasKeys(t, "state.puzzle", st["puzzle"].(map[string]any), "number", "seed", "level", "hardest", "givens")
		if len(st["entries"].(string)) != 81 || len(st["candidates"].([]any)) != 81 {
			t.Fatalf("state: entries/candidates not 81 long: %v", st)
		}
		_ = st["conflicts"].([]any)
		_ = st["ink"].(map[string]any)
	}

	st := call(msgHello, `{"screen": [1404, 1872], "gridSize": 900}`, msgState)[0].body
	checkState(st)
	if lvl := st["puzzle"].(map[string]any)["level"]; lvl != "easy" {
		t.Fatalf("first game is %v, want a new easy one", lvl)
	}

	st = call(msgNewGame, `{"level": "medium"}`, msgState)[0].body
	checkState(st)
	pz := st["puzzle"].(map[string]any)
	if pz["level"] != "medium" || pz["number"] != pz["seed"] || st["canUndo"] != false {
		t.Fatalf("newGame: %v", st)
	}
	cell := strings.IndexByte(pz["givens"].(string), '.')

	// A tall vertical stroke in the middle of the cell: an answer (read as 5).
	x, y := float32(cell%9*100), float32(cell/9*100)
	var pts []string
	for i := 0; i <= 70; i++ {
		pts = append(pts, fmt.Sprintf("%g,%g,%d,1", x+50, y+15+float32(i), i*3))
	}
	ink := fmt.Sprintf(`{"id": 17, "cell": %d, "gridSize": 900, "strokes": [[%s]]}`, cell, strings.Join(pts, ","))
	mk := call(msgInk, ink, msgMarks)[0].body
	hasKeys(t, "marks", mk, "id", "cell", "marks", "cellState", "conflicts", "unknown")
	if mk["id"] != 17.0 || mk["cell"] != float64(cell) || mk["unknown"] != false {
		t.Fatalf("marks: %v", mk)
	}
	marks := mk["marks"].([]any)
	if len(marks) != 1 {
		t.Fatalf("marks: %v", marks)
	}
	m0 := marks[0].(map[string]any)
	hasKeys(t, "mark", m0, "kind", "digit", "confidence", "top3", "bbox", "strokes", "erases")
	if m0["kind"] != "answer" || m0["digit"] != 5.0 || len(m0["bbox"].([]any)) != 4 || len(m0["top3"].([]any)) != 3 {
		t.Fatalf("mark: %v", m0)
	}
	if top := m0["top3"].([]any)[0].([]any); top[0] != 5.0 || top[1] != 0.99 {
		t.Fatalf("top3: %v", m0["top3"])
	}
	cs := mk["cellState"].(map[string]any)
	hasKeys(t, "cellState", cs, "entry", "candidates", "ink")
	cellInk := cs["ink"].([]any)
	if cs["entry"] != 5.0 || len(cellInk) != 1 {
		t.Fatalf("cellState: %v", cs)
	}
	in := cellInk[0].(map[string]any)
	if in["role"] != "answer" || len(in["points"].([]any)) != 4*71 {
		t.Fatalf("cell ink: role %v, %d values", in["role"], len(in["points"].([]any)))
	}

	st = call(msgUndo, `{}`, msgState)[0].body
	checkState(st)
	if st["entries"].(string)[cell] != '.' || len(st["ink"].(map[string]any)) != 0 {
		t.Fatalf("undo left %c / ink %v", st["entries"].(string)[cell], st["ink"])
	}

	h := call(msgHintRequest, `{}`, msgHint)[0].body
	hasKeys(t, "hint", h, "technique", "text")

	if e := call(msgInk, `{"id": 18, "cell": 81, "strokes": []}`, msgError)[0].body; e["id"] != 18.0 || e["message"] == "" {
		t.Fatalf("bad cell: %v", e)
	}
	call(msgNewGame, `{"level": "beyond"}`, msgError)
	call(msgLog, `{"message": "hi"}`)

	// Typing in the solution: the last digit brings state, then solved.
	givens, _ := engine.ParseGrid(pz["givens"].(string))
	sol, _ := engine.Solve(givens)
	lastEmpty := strings.LastIndexByte(pz["givens"].(string), '.')
	var done []sent
	for c, v := range givens {
		if v != 0 {
			continue
		}
		want := []int32{msgState}
		if c == lastEmpty {
			want = append(want, msgSolved)
		}
		done = call(msgSetCell, fmt.Sprintf(`{"cell": %d, "digit": %d}`, c, sol[c]), want...)
	}
	if done[0].body["solved"] != true {
		t.Fatalf("final state not solved: %v", done[0].body)
	}
	hasKeys(t, "solved", done[1].body, "elapsedMs", "mistakes", "hints", "best", "streak")

	if _, err := os.Stat(filepath.Join(dir, "state.json")); err != nil {
		t.Fatalf("nothing saved: %v", err)
	}
}

// harness drives a server through handle with a fixed, settable clock.
type harness struct {
	t     *testing.T
	s     *server
	dir   string
	clock time.Time
	out   []sent
}

func newHarness(t *testing.T, dir string) *harness {
	h := &harness{t: t, dir: dir, clock: time.Date(2026, 10, 29, 9, 0, 0, 0, time.UTC)} // a Thursday
	h.s = newServer(game.Store{Dir: dir}, func(typ int32, body string) {
		var m map[string]any
		if err := json.Unmarshal([]byte(body), &m); err != nil {
			t.Fatalf("type %d: body is not a JSON object: %s", typ, body)
		}
		h.out = append(h.out, sent{typ, m})
	})
	h.s.now = func() time.Time { return h.clock }
	h.s.analyzer = hwr.Analyzer{Classifier: five{}, Config: hwr.DefaultConfig()}
	return h
}

// call sends one message and returns the reply bodies, which must have the
// wanted types.
func (h *harness) call(typ int32, body string, want ...int32) []map[string]any {
	h.t.Helper()
	h.out = nil
	h.s.handle(typ, body)
	if len(h.out) != len(want) {
		h.t.Fatalf("message %d: sent %+v, want types %v", typ, h.out, want)
	}
	var bodies []map[string]any
	for i, w := range want {
		if h.out[i].typ != w {
			h.t.Fatalf("message %d: reply %d is type %d (%v), want %d", typ, i, h.out[i].typ, h.out[i].body, w)
		}
		bodies = append(bodies, h.out[i].body)
	}
	return bodies
}

func TestDailyAndStats(t *testing.T) {
	h := newHarness(t, t.TempDir())
	st := h.call(msgHello, `{"gridSize": 900}`, msgState)[0]
	hasKeys(t, "state", st, "stats", "personalSamples")
	stats := st["stats"].(map[string]any)
	hasKeys(t, "stats", stats, "easy", "medium", "hard", "expert", "daily")
	if easy := stats["easy"].(map[string]any); easy["played"] != 1.0 || easy["streak"] != 0.0 {
		t.Fatalf("first game not counted: %v", easy)
	}

	st = h.call(msgNewGame, `{"daily": true}`, msgState)[0]
	pz := st["puzzle"].(map[string]any)
	if pz["daily"] != "2026-10-29" || pz["level"] != "medium" || pz["seed"] != 20261029.0 {
		t.Fatalf("daily puzzle: %v", pz)
	}
	cell := strings.IndexByte(pz["givens"].(string), '.')
	h.call(msgSetCell, fmt.Sprintf(`{"cell": %d, "digit": 1}`, cell), msgState)
	again := h.call(msgNewGame, `{"daily": true}`, msgState)[0]
	if again["entries"].(string)[cell] != '1' || again["stats"].(map[string]any)["medium"].(map[string]any)["played"] != 1.0 {
		t.Fatalf("asking for the daily again restarted it: %v", again)
	}
	if e := again["stats"].(map[string]any)["easy"].(map[string]any); e["streak"] != 0.0 || e["played"] != 1.0 {
		t.Fatalf("easy stats: %v", e)
	}

	givens, _ := engine.ParseGrid(pz["givens"].(string))
	sol, _ := engine.Solve(givens)
	var last []map[string]any
	for c, v := range givens {
		if v == 0 {
			h.clock = h.clock.Add(10 * time.Second)
			h.out = nil
			h.s.handle(msgSetCell, fmt.Sprintf(`{"cell": %d, "digit": %d}`, c, sol[c]))
			last = nil
			for _, o := range h.out {
				last = append(last, o.body)
			}
		}
	}
	if len(last) != 2 {
		t.Fatalf("final setCell sent %v", last)
	}
	solved := last[1]
	if solved["best"] != true || solved["streak"] != 1.0 {
		t.Fatalf("solved: %v", solved)
	}
	stats = last[0]["stats"].(map[string]any) // the state before solved already counts it
	med := stats["medium"].(map[string]any)
	if med["solved"] != 1.0 || med["streak"] != 1.0 || med["bestMs"] != solved["elapsedMs"] {
		t.Fatalf("medium stats: %v (solved %v)", med, solved)
	}
	if d := stats["daily"].(map[string]any); d["streak"] != 1.0 || d["last"] != "2026-10-29" {
		t.Fatalf("daily stats: %v", d)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "stats.json")); err != nil {
		t.Fatal(err)
	}
}

func TestCalibration(t *testing.T) {
	dir := t.TempDir()
	h := newHarness(t, dir)
	h.call(msgHello, `{"gridSize": 900}`, msgState)
	cell := strings.IndexByte(h.call(msgNewGame, `{"level": "easy"}`, msgState)[0]["puzzle"].(map[string]any)["givens"].(string), '.')
	h.call(msgSetCell, fmt.Sprintf(`{"cell": %d, "digit": 4}`, cell), msgState)
	before := h.call(msgUndo, `{}`, msgState)[0]

	c := h.call(msgCalibrationStart, `{}`, msgCalibration)[0]
	if c["total"] != 27.0 || c["done"] != 0.0 || c["next"] != 1.0 || c["samples"] != 0.0 {
		t.Fatalf("calibration: %v", c)
	}
	var pts []string
	for i := 0; i <= 60; i++ {
		pts = append(pts, fmt.Sprintf("130,%d,%d,1", 40+3*i, 4*i))
	}
	ink := fmt.Sprintf(`{"id": 9, "digit": 1, "size": 260, "strokes": [[%s]]}`, strings.Join(pts, ","))
	r := h.call(msgCalibrationInk, ink, msgCalibrationRead, msgCalibration)
	if rd := r[0]; rd["id"] != 9.0 || rd["digit"] != 1.0 || rd["read"] != 5.0 || rd["confidence"] != 0.99 || rd["done"] != 1.0 || rd["total"] != 27.0 {
		t.Fatalf("calibrationRead: %v", rd)
	}
	if r[1]["done"] != 1.0 || r[1]["next"] != 2.0 || r[1]["samples"] != 1.0 {
		t.Fatalf("calibration after one sample: %v", r[1])
	}
	if e := h.call(msgCalibrationInk, `{"id": 10, "digit": 0, "size": 260, "strokes": [[1,2,0,1]]}`, msgError)[0]; e["id"] != 10.0 {
		t.Fatalf("bad digit: %v", e)
	}
	if e := h.call(msgCalibrationInk, `{"id": 11, "digit": 2, "size": 260, "strokes": []}`, msgError)[0]; e["id"] != 11.0 {
		t.Fatalf("no ink: %v", e)
	}

	r = h.call(msgCalibrationDone, `{}`, msgPersonalized, msgState)
	hasKeys(t, "personalized", r[0], "samples", "before", "after")
	if r[0]["samples"] != 1.0 || r[1]["personalSamples"] != 1.0 {
		t.Fatalf("personalized %v, personalSamples %v", r[0], r[1]["personalSamples"])
	}
	for _, k := range []string{"entries", "candidates", "ink", "canUndo", "conflicts"} {
		if !reflect.DeepEqual(r[1][k], before[k]) {
			t.Fatalf("calibration changed the game's %s: %v -> %v", k, before[k], r[1][k])
		}
	}

	f, err := os.Open(filepath.Join(dir, "calibration.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	got, err := hwr.ReadSamples(f)
	if err != nil || len(got) != 1 {
		t.Fatalf("calibration.jsonl: %v, %d samples", err, len(got))
	}
	if sm := got[0]; sm.Source != "calibration" || sm.Label != 1 || sm.Pred != 5 || sm.Kind != "answer" ||
		sm.Cell != [4]float32{0, 0, 260, 260} || len(sm.Strokes) != 1 || len(sm.Strokes[0]) != 61 {
		t.Fatalf("sample: %+v", sm)
	}
	h = newHarness(t, dir)
	h.waitPersonal()
	if n := h.s.personal.Len(); n != 1 {
		t.Fatalf("restart: personalized with %d samples, want 1", n)
	}
}

// waitPersonal blocks until the personal recognizer is built and swapped in.
func (h *harness) waitPersonal() {
	if ready := h.s.personalReady; ready != nil {
		ready <- <-ready // put back for adoptPersonal
		h.s.adoptPersonal()
	}
}

// The personal recognizer is built in the background: hello is answered
// meanwhile with the count it will have, and samples learned meanwhile end
// up in it.
func TestPersonalBuildsInBackground(t *testing.T) {
	dir := t.TempDir()
	st := game.Store{Dir: dir}
	for i := 0; i < 30; i++ {
		var pts [][4]float64
		for k := 0; k <= 40; k++ {
			pts = append(pts, [4]float64{100 + float64(i%7), 60 + 3*float64(k), 1, 4 * float64(k)})
		}
		if err := st.AppendCalibration(hwr.Sample{V: 1, Source: "calibration", Label: 1 + i%9, Strokes: [][][4]float64{pts}}); err != nil {
			t.Fatal(err)
		}
	}
	h := newHarness(t, dir)
	if n := h.call(msgHello, `{"gridSize": 900}`, msgState)[0]["personalSamples"]; n != 30.0 {
		t.Fatalf("personalSamples %v, want 30", n)
	}
	h.call(msgCalibrationStart, `{}`, msgCalibration)
	var pts []string
	for i := 0; i <= 60; i++ {
		pts = append(pts, fmt.Sprintf("130,%d,%d,1", 40+3*i, 4*i))
	}
	r := h.call(msgCalibrationInk, fmt.Sprintf(`{"id": 1, "digit": 1, "size": 260, "strokes": [[%s]]}`, strings.Join(pts, ",")),
		msgCalibrationRead, msgCalibration)
	if r[1]["samples"] != 31.0 {
		t.Fatalf("calibration: %v, want 31 samples", r[1])
	}
	h.waitPersonal()
	if n := h.s.personal.Len(); n != 31 {
		t.Fatalf("personalized with %d samples, want 31", n)
	}
	if _, ok := h.s.analyzer.Classifier.(five); !ok {
		t.Fatal("the swap replaced the test's classifier")
	}
}

// The timer counts wall time while the board is on screen, thinking
// included; it pauses while another page is up (`view`) or the display
// sleeps. A stretch with no event at all counts at most game.MaxGap.
func TestTimer(t *testing.T) {
	h := newHarness(t, t.TempDir())
	pz := h.call(msgHello, `{"gridSize": 900}`, msgState)[0]["puzzle"].(map[string]any)
	cell := strings.IndexByte(pz["givens"].(string), '.')
	set := func(after time.Duration, digit int) float64 {
		t.Helper()
		h.clock = h.clock.Add(after)
		return h.call(msgSetCell, fmt.Sprintf(`{"cell": %d, "digit": %d}`, cell, digit), msgState)[0]["elapsedMs"].(float64)
	}
	if e := set(30*time.Second, 1); e != 30_000 {
		t.Fatalf("elapsed %v ms after 30 s of play", e)
	}
	if e := set(4*time.Minute, 2); e != 270_000 {
		t.Fatalf("elapsed %v ms after a 4 min think, want 4.5 min", e)
	}
	h.call(msgView, `{"board": false}`) // the New game list
	h.clock = h.clock.Add(10 * time.Minute)
	h.call(msgView, `{"board": true}`)
	h.s.display(false) // a night asleep
	h.clock = h.clock.Add(8 * time.Hour)
	h.s.display(true)
	if e := set(20*time.Second, 3); e != 290_000 {
		t.Fatalf("elapsed %v ms, want 270 s + 20 s (the list and the night don't count)", e)
	}
	if e := set(2*time.Hour, 4); e != 290_000+float64(game.MaxGap.Milliseconds()) {
		t.Fatalf("elapsed %v ms after 2 h with no event, want 290 s + MaxGap", e)
	}
}

// hint {"reveal": false} is a nudge and isn't counted; true or absent is.
func TestHintReveal(t *testing.T) {
	h := newHarness(t, t.TempDir())
	h.call(msgHello, `{"gridSize": 900}`, msgState)
	nudge := h.call(msgHintRequest, `{"reveal": false}`, msgHint)[0]
	if n := h.s.g.Hints(); n != 0 {
		t.Fatalf("a nudge counted: hints %d", n)
	}
	full := h.call(msgHintRequest, `{"reveal": true}`, msgHint)[0]
	if !reflect.DeepEqual(nudge, full) || h.s.g.Hints() != 1 {
		t.Fatalf("reveal: hints %d, step %v (nudge was %v)", h.s.g.Hints(), full, nudge)
	}
	h.call(msgHintRequest, `{}`, msgHint)
	if n := h.s.g.Hints(); n != 2 {
		t.Fatalf("hint {}: hints %d, want 2", n)
	}
}

// The daily generated in the background is used only for its own date.
func TestTakeDaily(t *testing.T) {
	s := &server{dailyReady: make(chan dailyPuzzle, 1)}
	p := engine.Generate(7, engine.Easy)
	s.dailyReady <- dailyPuzzle{"2026-10-29", p}
	if _, ok := s.takeDaily("2026-10-30"); ok {
		t.Fatal("used another day's puzzle")
	}
	if got, ok := s.takeDaily("2026-10-29"); !ok || got.Givens != p.Givens {
		t.Fatal("pre-generated daily not used")
	}
}

func TestPlayByNumber(t *testing.T) {
	h := newHarness(t, t.TempDir())
	h.call(msgHello, `{"gridSize": 900}`, msgState)

	// A digit written in a number box is read (five{} reads everything as 5)
	// and never logged as a sample.
	stroke := `[[10, 10, 0, 0.5, 40, 70, 30, 0.5, 70, 120, 60, 0.5]]`
	r := h.call(msgReadInk, `{"id": 3, "size": 140, "strokes": `+stroke+`}`, msgInkRead)[0]
	if r["id"] != 3.0 || r["digit"] != 5.0 {
		t.Fatalf("inkRead: %v", r)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "samples.jsonl")); err == nil {
		t.Fatal("readInk logged a sample")
	}
	h.call(msgReadInk, `{"id": 4, "size": 140, "strokes": []}`, msgError)

	st := h.call(msgNewGame, `{"level": "hard", "number": 482137}`, msgState)[0]
	pz := st["puzzle"].(map[string]any)
	if pz["number"] != 482137.0 || pz["level"] != "hard" || pz["byNumber"] != true ||
		pz["givens"] != engine.Generate(482137, engine.Hard).Givens.String() {
		t.Fatalf("puzzle by number: %v", pz)
	}
	if hard := st["stats"].(map[string]any)["hard"].(map[string]any); hard["played"] != 0.0 {
		t.Fatalf("a replayed puzzle counted as played: %v", hard)
	}

	givens, _ := engine.ParseGrid(pz["givens"].(string))
	sol, _ := engine.Solve(givens)
	var solved map[string]any
	for c, v := range givens {
		if v == 0 {
			h.out = nil
			h.s.handle(msgSetCell, fmt.Sprintf(`{"cell": %d, "digit": %d}`, c, sol[c]))
			if len(h.out) == 2 {
				solved = h.out[1].body
			}
		}
	}
	if solved == nil || solved["best"] != false || solved["streak"] != 0.0 {
		t.Fatalf("solved by number: %v", solved)
	}

	for _, bad := range []string{`{"level": "hard", "number": 480213}`, `{"level": "hard", "number": 12345}`} {
		h.call(msgNewGame, bad, msgError)
	}
}
