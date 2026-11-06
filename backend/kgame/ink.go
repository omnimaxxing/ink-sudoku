package kgame

import (
	"fmt"
	"math"
	"sort"
	"time"

	"sudoku/hwr"
)

// strokeGap is the pen-up pause assumed between consecutive strokes. The
// protocol times each stroke from its own start, and the frontend cuts a
// message once the pen rests ~600 ms, so all strokes in one message are one
// go for hwr (pauses < Config.GroupPause).
const strokeGap = 250

// InkResult is what one ink message was read as and did.
type InkResult struct {
	// Marks index the message's strokes: Strokes always, and a scratch-out's
	// Erases list the message strokes it rubbed out (stored ink it erased is
	// simply gone from CellInk). Every non-empty message stroke is in exactly
	// one mark's Strokes or one scratch-out's Erases; strokes hwr skipped
	// (mostly outside the cell) form one extra Unknown mark.
	Marks []hwr.Mark
	Applied
}

// Ink reads one ink message for cell and applies it. strokes are flat
// x, y, t, p, … arrays in grid-local pixels for a grid of gridSize px. Ink
// on a clue cell is an error and changes nothing.
//
// The new strokes are recognised on their own, so writing over an old answer
// replaces it rather than merging with it. Stored ink only matters to
// scratch-outs: a first, geometry-only pass runs a over stored ++ new ink to
// find what each scratch-out covers; a second pass reads the remaining new
// strokes.
func (g *Game) Ink(a hwr.Analyzer, cell int, gridSize float32, strokes [][]float32, now time.Time) (InkResult, error) {
	if err := g.checkWhite(cell); err != nil {
		return InkResult{}, err
	}
	if gridSize <= 0 {
		gridSize = g.gridSize
	}
	if gridSize <= 0 {
		return InkResult{}, fmt.Errorf("gridSize unknown")
	}
	clean := make([][]float32, len(strokes))
	for i, s := range strokes {
		if len(s)%4 != 0 {
			return InkResult{}, fmt.Errorf("stroke %d: %d values, want x,y,t,p groups", i, len(s))
		}
		clean[i] = quantize(s)
	}
	g.SetGridSize(gridSize)
	rect := g.CellRect(cell)

	stored := g.ink[cell]
	n := len(stored)
	all := make([]hwr.Stroke, 0, n+len(clean))
	var t int64
	for _, r := range stored {
		s := toStroke(g.points[r.id], t)
		all = append(all, s)
		t = endTime(s, t) + strokeGap
	}
	for _, f := range clean {
		s := toStroke(f, t)
		all = append(all, s)
		t = endTime(s, t) + strokeGap
	}
	fresh := all[n:]

	// Pass 1: scratch-outs among the new strokes, and what they cover.
	geo := hwr.Analyzer{Classifier: noRead{}, Config: a.Config}
	var marks []hwr.Mark
	gone := make([]bool, len(clean)) // scratch strokes and strokes they rubbed out
	for _, m := range geo.Analyze(rect, all) {
		if m.Kind != hwr.ScratchOut || m.Strokes[0] < n {
			continue
		}
		m.Strokes = []int{m.Strokes[0] - n}
		gone[m.Strokes[0]] = true
		for _, e := range m.Erases {
			if e >= n {
				gone[e-n] = true
			}
		}
		marks = append(marks, m)
	}

	// Pass 2: read the rest of the new ink.
	rest := make([]hwr.Stroke, len(fresh))
	for i, s := range fresh {
		if !gone[i] {
			rest[i] = s
		}
	}
	used := append([]bool(nil), gone...)
	for _, m := range a.Analyze(rect, rest) {
		for _, i := range m.Strokes {
			used[i] = true
		}
		marks = append(marks, m)
	}
	var skipped []hwr.Stroke
	unk := hwr.Mark{Kind: hwr.Unknown}
	for i, s := range fresh {
		if !used[i] && len(s) > 0 {
			unk.Strokes = append(unk.Strokes, i)
			skipped = append(skipped, s)
		}
	}
	if len(skipped) > 0 {
		unk.BBox, _ = hwr.Bounds(skipped...)
		marks = append(marks, unk)
	}
	sort.SliceStable(marks, func(i, j int) bool { return marks[i].Strokes[0] < marks[j].Strokes[0] })

	res := InkResult{Applied: g.ApplyMarks(cell, clean, marks, now), Marks: marks}
	for k, m := range res.Marks {
		if m.Kind != hwr.ScratchOut {
			continue
		}
		var own []int
		for _, e := range m.Erases {
			if e >= n {
				own = append(own, e-n)
			}
		}
		res.Marks[k].Erases = own
	}
	return res, nil
}

// noRead is a classifier that reads nothing, for the geometry-only pass.
type noRead struct{}

func (noRead) Classify([]hwr.Stroke, float32) hwr.Result { return hwr.Result{} }

// toStroke converts a flat x, y, t, p array, shifting its times to start at
// t0.
func toStroke(flat []float32, t0 int64) hwr.Stroke {
	if len(flat) < 4 {
		return nil
	}
	s := make(hwr.Stroke, len(flat)/4)
	first := flat[2]
	for i := range s {
		p := flat[4*i:]
		s[i] = hwr.Point{X: p[0], Y: p[1], T: t0 + int64(p[2]-first), P: p[3]}
	}
	return s
}

func endTime(s hwr.Stroke, t0 int64) int64 {
	if len(s) == 0 {
		return t0
	}
	return max(t0, s[len(s)-1].T)
}

// quantize copies a stroke, rounding x, y to 0.1 px, t to 1 ms and p to 0.01:
// plenty for display and recognition, and it halves the JSON.
func quantize(s []float32) []float32 {
	out := make([]float32, len(s))
	for i := 0; i+3 < len(s); i += 4 {
		out[i] = round(s[i], 10)
		out[i+1] = round(s[i+1], 10)
		out[i+2] = round(s[i+2], 1)
		out[i+3] = round(s[i+3], 100)
	}
	return out
}

func round(v, scale float32) float32 {
	return float32(math.Round(float64(v)*float64(scale)) / float64(scale))
}
