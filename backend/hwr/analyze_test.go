package hwr

import (
	"testing"
)

// stubClassifier accepts everything as a 5 so the tests below exercise only
// Analyze's geometry (grouping, kinds), not recognition accuracy.
type stubClassifier struct{ calls [][]Stroke }

func (s *stubClassifier) Classify(strokes []Stroke, _ float32) Result {
	s.calls = append(s.calls, strokes)
	return Result{Digit: 5, Confidence: 0.99, Class: '5', Top3: [3]Guess{{5, 0.99}, {6, 0.005}, {3, 0.003}}}
}

// glyph maps unit-square polylines into a box at (x, y) of height h,
// timestamped from t0 with 300 ms pen-up pauses between strokes.
func glyph(x, y, h float32, t0 int64, lines ...[][2]float32) []Stroke {
	var out []Stroke
	t := t0
	for _, ln := range lines {
		pts := make([][2]float32, len(ln))
		for i, p := range ln {
			pts[i] = [2]float32{x + p[0]*h, y + p[1]*h}
		}
		s := densePoly(pts...)
		for i := range s {
			s[i].T = t + int64(i)*3
		}
		t = s[len(s)-1].T + 300
		out = append(out, s)
	}
	return out
}

var (
	unitSeven = [][2]float32{{0, 0}, {0.6, 0}, {0.25, 1}}
	unitOne   = [][2]float32{{0.3, 0}, {0.3, 1}}
	unitTwo   = [][2]float32{{0, 0.2}, {0.3, 0}, {0.6, 0.2}, {0, 1}, {0.6, 1}}
	fourA     = [][2]float32{{0.45, 0}, {0, 0.65}, {0.7, 0.65}}
	fourB     = [][2]float32{{0.5, 0.35}, {0.5, 1}}
	fiveBody  = [][2]float32{{0.05, 0.05}, {0, 0.45}, {0.4, 0.4}, {0.6, 0.65}, {0.4, 1}, {0, 0.9}}
	fiveBar   = [][2]float32{{0.12, 0}, {0.6, 0}}
)

var cell = Rect{100, 100, 240, 240} // 140 px

func kinds(ms []Mark) map[MarkKind]int {
	k := map[MarkKind]int{}
	for _, m := range ms {
		k[m.Kind]++
	}
	return k
}

func TestAnalyzeAnswerVsCandidates(t *testing.T) {
	clf := &stubClassifier{}
	a := Analyzer{clf, DefaultConfig()}

	// Three pencil marks (~30 px) in corners, written in quick succession.
	var strokes []Stroke
	strokes = append(strokes, glyph(106, 105, 30, 0, unitOne)...)
	strokes = append(strokes, glyph(206, 106, 30, 900, unitTwo)...)
	strokes = append(strokes, glyph(108, 200, 32, 1800, unitSeven)...)
	ms := a.Analyze(cell, strokes)
	if k := kinds(ms); k[Candidate] != 3 || len(ms) != 3 {
		t.Fatalf("pencil marks: got %v (%d marks), want 3 candidates", k, len(ms))
	}
	for _, m := range ms {
		if len(m.Strokes) != 1 {
			t.Errorf("candidate grouped %d strokes, want 1", len(m.Strokes))
		}
	}

	// A big centred digit in an empty cell is an answer.
	ans := glyph(145, 125, 90, 0, unitSeven)
	ms = a.Analyze(cell, ans)
	if len(ms) != 1 || ms[0].Kind != Answer || ms[0].Digit != 5 {
		t.Fatalf("big digit: got %+v, want one Answer", ms)
	}

	// A short 1 centred across but sitting low is an answer (r5c3 of the
	// 2026-11-02 daily: 0.40 tall, 0.24 below the middle, and the solve never
	// came); the same mark off to one side is a pencil mark.
	low := glyph(170-0.3*55, 175, 55, 0, unitOne)
	if ms := a.Analyze(cell, low); len(ms) != 1 || ms[0].Kind != Answer {
		t.Fatalf("low centred 1: got %+v, want an Answer", ms)
	}
	side := glyph(205-0.3*55, 175, 55, 0, unitOne)
	if ms := a.Analyze(cell, side); len(ms) != 1 || ms[0].Kind != Candidate {
		t.Fatalf("low 1 to the side: got %+v, want a Candidate", ms)
	}

	// Answer written later next to the old pencil marks stays separate.
	all := append(append([]Stroke{}, strokes...), glyph(145, 125, 90, 10000, unitSeven)...)
	ms = a.Analyze(cell, all)
	if k := kinds(ms); k[Answer] != 1 || k[Candidate] != 3 {
		t.Fatalf("answer + candidates: got %v", k)
	}
}

func TestAnalyzeScratchOut(t *testing.T) {
	a := Analyzer{&stubClassifier{}, DefaultConfig()}
	digit := glyph(145, 125, 90, 0, unitSeven)
	// Zig-zag over the digit three seconds later: 7 legs back and forth.
	var zz [][2]float32
	for i := 0; i <= 7; i++ {
		x := float32(135)
		if i%2 == 1 {
			x = 215
		}
		zz = append(zz, [2]float32{x, 140 + float32(i)*9})
	}
	scr := densePoly(zz...)
	for i := range scr {
		scr[i].T = 3000 + int64(i)*2
	}
	strokes := append(digit, scr)
	ms := a.Analyze(cell, strokes)
	if len(ms) != 1 || ms[0].Kind != ScratchOut {
		t.Fatalf("got %+v, want a single ScratchOut", ms)
	}
	if len(ms[0].Erases) != 1 || ms[0].Erases[0] != 0 || ms[0].Strokes[0] != 1 {
		t.Errorf("scratch-out erases %v (strokes %v), want [0] by stroke 1", ms[0].Erases, ms[0].Strokes)
	}

	// Real digits are never scratch-outs, even zig-zaggy ones.
	if ms := a.Analyze(cell, glyph(140, 120, 95, 0, unitTwo)); len(ms) != 1 || ms[0].Kind == ScratchOut {
		t.Errorf("a 2 was taken for a scratch-out: %+v", ms)
	}
}

func TestAnalyzeMultiStrokeDigit(t *testing.T) {
	clf := &stubClassifier{}
	a := Analyzer{clf, DefaultConfig()}
	// A 4 in two strokes (crossing) and a 5 whose top bar is lifted and
	// slightly detached from the body.
	four := glyph(140, 120, 90, 0, fourA, fourB)
	ms := a.Analyze(cell, four)
	if len(ms) != 1 || len(ms[0].Strokes) != 2 || ms[0].Kind != Answer {
		t.Fatalf("two-stroke 4: got %+v", ms)
	}
	five := glyph(150, 125, 85, 0, fiveBody)
	bar := glyph(150, 120, 85, five[0][len(five[0])-1].T+250, fiveBar) // ~4 px above the body
	ms = a.Analyze(cell, append(five, bar...))
	if len(ms) != 1 || len(ms[0].Strokes) != 2 {
		t.Fatalf("5 with separate bar: got %d marks %+v", len(ms), ms)
	}
	// The classifier saw both strokes together.
	if last := clf.calls[len(clf.calls)-1]; len(last) != 2 {
		t.Errorf("classifier got %d strokes, want 2", len(last))
	}
}

// rejectAll rejects every glyph, so only the lone-1 rule can produce a digit.
type rejectAll struct{}

func (rejectAll) Classify([]Stroke, float32) Result {
	return Result{Confidence: 0.9, Class: '?', Top3: [3]Guess{{7, 0.06}, {1, 0.03}, {4, 0.005}}}
}

func TestLoneOne(t *testing.T) {
	c := Rect{100, 100, 231, 231} // 131 px, as on the tablet
	a := Analyzer{rejectAll{}, DefaultConfig()}
	read := func(strokes []Stroke) []Mark { return a.Analyze(c, strokes) }
	bowed := func(x, y, h, bow float32) []Stroke {
		return []Stroke{densePoly([2]float32{x, y - h/2}, [2]float32{x + bow*h, y}, [2]float32{x, y + h/2})}
	}

	for _, tc := range []struct {
		name string
		ink  []Stroke
		kind MarkKind
	}{
		{"answer, upright", line(165, 165, 85, 0), Answer},
		{"answer, 20 deg", line(165, 165, 80, -20), Answer},
		{"candidate, 30 px", line(120, 120, 30, 6), Candidate},
		{"candidate, 22 px, slight bow", bowed(210, 120, 22, 0.06), Candidate},
	} {
		ms := read(tc.ink)
		if len(ms) != 1 || ms[0].Kind != tc.kind || ms[0].Digit != 1 || ms[0].Confidence != 0.99 || ms[0].Top3[0].Digit != 1 {
			t.Errorf("%s: %+v, want a %v 1", tc.name, ms, tc.kind)
		}
	}

	retrace := []Stroke{densePoly([2]float32{165, 130}, [2]float32{165, 200}, [2]float32{166, 150})}
	flag := []Stroke{densePoly([2]float32{150, 145}, [2]float32{165, 130}, [2]float32{165, 200})}
	var zz [][2]float32
	for i := 0; i <= 7; i++ {
		zz = append(zz, [2]float32{150 + 3*float32(i), 130 + 70*float32(i%2)})
	}
	vscratch := []Stroke{densePoly(zz...)}
	for _, tc := range []struct {
		name string
		ink  []Stroke
	}{
		{"dot", line(165, 165, 3, 0)},
		{"tick", line(165, 165, 14, 0)},
		{"short stroke, 18 px", line(165, 165, 18, 0)},
		{"35 deg", line(165, 165, 70, 35)},
		{"horizontal", []Stroke{densePoly([2]float32{130, 165}, [2]float32{200, 166})}},
		{"curved", bowed(160, 165, 70, 0.25)},
		{"retrace", retrace},
		{"flagged 1", flag},
		{"two-stroke 4", glyph(140, 120, 90, 0, fourA, fourB)},
		{"7 from two strokes", glyph(140, 120, 90, 0, [][2]float32{{0, 0}, {0.6, 0}}, [][2]float32{{0.6, 0}, {0.45, 1}})},
	} {
		for _, mk := range read(tc.ink) {
			if mk.Digit != 0 || mk.Kind != Unknown {
				t.Errorf("%s: %+v, want Unknown", tc.name, mk)
			}
		}
	}
	if ms := read(vscratch); len(ms) != 1 || ms[0].Kind != ScratchOut {
		t.Errorf("up-down scratch-out: %+v, want a ScratchOut", ms)
	}

	// An accepted read is the classifier's call, and the rule can be off.
	if ms := (Analyzer{&stubClassifier{}, DefaultConfig()}).Analyze(c, line(165, 165, 85, 0)); len(ms) != 1 || ms[0].Digit != 5 {
		t.Errorf("accepted read overridden: %+v", ms)
	}
	off := DefaultConfig()
	off.LoneOneMaxSlant = 0
	if ms := (Analyzer{rejectAll{}, off}).Analyze(c, line(165, 165, 85, 0)); len(ms) != 1 || ms[0].Kind != Unknown {
		t.Errorf("rule disabled: %+v", ms)
	}

	// The tablet case: a ~30 px pencil-mark 1, slightly slanted, with the
	// real model.
	if ms := Analyze(c, line(120, 125, 30, 8)); len(ms) != 1 || ms[0].Kind != Candidate || ms[0].Digit != 1 {
		t.Errorf("pencil-mark 1 with the shipped model: %+v", ms)
	}
}
