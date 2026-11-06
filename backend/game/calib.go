package game

import (
	"fmt"
	"time"

	"sudoku/hwr"
)

// calibrationOrder is the digit prompted at each step of "teach it your
// hand": 1–9 in order, then two fixed shuffles (no digit twice in a row).
var calibrationOrder = [...]int{
	1, 2, 3, 4, 5, 6, 7, 8, 9,
	3, 7, 1, 9, 5, 2, 8, 4, 6,
	8, 2, 6, 4, 9, 1, 5, 3, 7,
}

// CalibrationTotal is the number of prompts in a calibration run.
const CalibrationTotal = len(calibrationOrder)

// Calibration is the progress through one calibration run. The zero value
// is a fresh run.
type Calibration struct{ Done int }

// Next is the digit to prompt next, 0 once the run is finished.
func (c Calibration) Next() int {
	if c.Done >= CalibrationTotal {
		return 0
	}
	return calibrationOrder[c.Done]
}

// Advance counts one more sample; past the end it stays finished.
func (c *Calibration) Advance() { c.Done = min(c.Done+1, CalibrationTotal) }

// ParseStrokes converts protocol strokes (flat x, y, t, p, … arrays, t from
// each stroke's start) into hwr strokes on one clock, written in one go.
// Points are rounded as stored ink is; empty strokes are dropped.
func ParseStrokes(flat [][]float32) ([]hwr.Stroke, error) {
	var out []hwr.Stroke
	var t int64
	for i, f := range flat {
		if len(f)%4 != 0 {
			return nil, fmt.Errorf("stroke %d: %d values, want x,y,t,p groups", i, len(f))
		}
		if len(f) == 0 {
			continue
		}
		s := toStroke(quantize(f), t)
		out = append(out, s)
		t = endTime(s, t) + strokeGap
	}
	return out, nil
}

// CalibrationSample reads strokes, written for digit in a size×size box
// (box-local px), with c, and returns the read and the labelled sample to
// log: Pred/Conf are what c read, Label is digit.
func CalibrationSample(c hwr.Classifier, m *hwr.Model, threshold, size float32, strokes []hwr.Stroke, digit int, now time.Time) (hwr.Result, hwr.Sample) {
	r := c.Classify(strokes, threshold)
	mark := hwr.Mark{Kind: hwr.Answer, Digit: r.Digit, Confidence: r.Confidence, Top3: r.Top3}
	if r.Digit == 0 {
		mark.Kind = hwr.Unknown
	}
	mark.BBox, _ = hwr.Bounds(strokes...)
	for i := range strokes {
		mark.Strokes = append(mark.Strokes, i)
	}
	box := hwr.Rect{X1: size, Y1: size}
	return r, hwr.NewSample(m, box, strokes, mark, digit, "calibration", now)
}
