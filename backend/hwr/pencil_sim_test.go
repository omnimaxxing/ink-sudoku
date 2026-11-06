package hwr

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"testing"
)

// TestPencilSimulation reports (never asserts) how real pen digits fare as
// pencil marks: shrunk to 14–40 px and written in a cell corner, some
// touching or crossing the grid line, read by the plain model and by each
// writer's Personal built from a 27-digit calibration page written large.
// Answers at normal size are the control. Report-only:
//
//	HWR_PERSONAL=$PWD/../hwrtrain/data/cache/personal_strokes.jsonl \
//	  pixi run go test ./hwr/ -run PencilSimulation -v
func TestPencilSimulation(t *testing.T) {
	path := os.Getenv("HWR_PERSONAL")
	if path == "" {
		t.Skip("set HWR_PERSONAL to hwrtrain/data/cache/personal_strokes.jsonl")
	}
	writers := loadWriterInk(t, path, "test")
	m := DefaultModel()
	cfg := DefaultConfig()
	const cell = 1180.0 / 9
	rng := rand.New(rand.NewSource(1))

	type tally map[string]int
	results := map[string]tally{}
	add := func(row, outcome string) {
		if results[row] == nil {
			results[row] = tally{}
		}
		results[row][outcome]++
		results[row]["n"]++
	}

	for _, w := range writers {
		// Calibration: the first 3 samples of each digit, written large.
		var calib []Sample
		used := map[int]int{}
		var rest []writerDigit
		for _, s := range w {
			if used[s.digit] < 3 {
				used[s.digit]++
				calib = append(calib, sampleFrom(place(s.ink, 400, 100, 100), s.digit))
				continue
			}
			rest = append(rest, s)
		}
		personal := NewPersonal(m, calib)
		readers := []struct {
			name string
			c    Classifier
		}{{"model", m}, {"personal", personal}}

		for _, s := range rest {
			for _, r := range readers {
				a := Analyzer{Classifier: r.c, Config: cfg}
				// Control: an answer, ~80 px, centred.
				box := Rect{0, 0, cell, cell}
				ans := place(s.ink, 70+rng.Float32()*25, cell/2, cell/2)
				add(r.name+" answer", outcome(a.Analyze(box, ans), len(ans), s.digit, Answer))

				// Pencil mark in a random corner, max dimension 14..40 px.
				size := 14 + rng.Float32()*26
				edge := -4 + rng.Float32()*12 // distance of the mark's box from the corner lines; < 0 crosses them
				cx, cy := edge+size/2, edge+size/2
				if rng.Intn(2) == 0 {
					cx = cell - cx
				}
				if rng.Intn(2) == 0 {
					cy = cell - cy
				}
				pm := place(s.ink, size, cx, cy)
				bucket := "28-40px"
				switch {
				case size < 20:
					bucket = "14-20px"
				case size < 28:
					bucket = "20-28px"
				}
				o := outcome(a.Analyze(box, pm), len(pm), s.digit, Candidate)
				add(r.name+" pencil "+bucket, o)
				if edge < 0 {
					add(r.name+" pencil crossing the line", o)
				}
			}
		}
	}

	cols := []string{"ok", "unknown: too small", "unknown: outside cell", "unknown: rejected", "misread", "wrong kind"}
	rows := []string{"model answer", "personal answer"}
	for _, who := range []string{"model", "personal"} {
		for _, b := range []string{"14-20px", "20-28px", "28-40px"} {
			rows = append(rows, who+" pencil "+b)
		}
		rows = append(rows, who+" pencil crossing the line")
	}
	t.Logf("%-32s %6s  %s", "", "n", fmt.Sprint(cols))
	for _, r := range rows {
		tl := results[r]
		line := fmt.Sprintf("%-32s %6d ", r, tl["n"])
		for _, c := range cols {
			line += fmt.Sprintf(" %5.1f%%", 100*float64(tl[c])/float64(max(tl["n"], 1)))
		}
		t.Log(line)
	}
}

type writerDigit struct {
	digit int
	ink   []Stroke
}

// loadWriterInk reads the digit samples of one split, grouped by writer.
func loadWriterInk(t *testing.T, path, split string) map[string][]writerDigit {
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out := map[string][]writerDigit{}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var r struct {
			Split, Writer, Label string
			Strokes              [][][]float32
		}
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		if r.Split != split || len(r.Label) != 1 || r.Label[0] < '1' || r.Label[0] > '9' {
			continue
		}
		var ink []Stroke
		for _, s := range r.Strokes {
			st := make(Stroke, len(s))
			for i, p := range s {
				st[i] = Point{X: p[0], Y: p[1], P: 0.5}
			}
			ink = append(ink, st)
		}
		out[r.Writer] = append(out[r.Writer], writerDigit{int(r.Label[0] - '0'), ink})
	}
	return out
}

// place scales ink so its larger side is size px, centres it on (cx, cy)
// and gives it pen timing: ~6 ms per point, 150 ms between strokes.
func place(ink []Stroke, size, cx, cy float32) []Stroke {
	b, _ := Bounds(ink...)
	k := size / max(b.W(), b.H(), 1)
	bx, by := b.Center()
	out := make([]Stroke, len(ink))
	var tm int64
	for i, s := range ink {
		st := make(Stroke, len(s))
		for j, p := range s {
			st[j] = Point{X: cx + (p.X-bx)*k, Y: cy + (p.Y-by)*k, P: 0.5, T: tm}
			tm += 6
		}
		tm += 150
		out[i] = st
	}
	return out
}

func sampleFrom(ink []Stroke, digit int) Sample {
	s := Sample{V: 1, Source: "calibration", Kind: "answer", Label: digit}
	for _, st := range ink {
		var pts [][4]float64
		for _, p := range st {
			pts = append(pts, [4]float64{float64(p.X), float64(p.Y), float64(p.T), float64(p.P)})
		}
		s.Strokes = append(s.Strokes, pts)
	}
	return s
}

// outcome sorts one Analyze result into the report's columns.
func outcome(marks []Mark, nStrokes, digit int, want MarkKind) string {
	covered := 0
	var got []Mark
	for _, mk := range marks {
		covered += len(mk.Strokes) + len(mk.Erases)
		if mk.Kind != ScratchOut {
			got = append(got, mk)
		}
	}
	switch {
	case covered < nStrokes && len(got) == 0:
		return "unknown: outside cell"
	case len(got) != 1:
		return "misread" // split into several marks, or merged with nothing
	}
	mk := got[0]
	switch {
	case mk.Kind == Unknown && mk.Confidence == 0 && len(mk.Top3) == 0:
		return "unknown: too small"
	case mk.Kind == Unknown:
		return "unknown: rejected"
	case mk.Digit != digit:
		return "misread"
	case mk.Kind != want:
		return "wrong kind"
	}
	return "ok"
}
