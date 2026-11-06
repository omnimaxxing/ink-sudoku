package hwr

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"time"
)

// Sample is one line of the self-improvement log (JSONL, one object per
// line). The app appends one when the player corrects a misread; hwrtrain's
// finetune.py consumes the file to adapt the model to that player:
//
//	cd hwrtrain && .venv/bin/python -I -m hwrtrain.finetune corrections.jsonl
//
// Example line:
//
//	{"v":1,"ts":1793212458355,"model":"9f3a12bc","source":"correction",
//	 "kind":"candidate","label":7,"pred":1,"conf":0.93,
//	 "top3":[[1,0.93],[7,0.05],[4,0.01]],"cell":[100,100,240,240],
//	 "strokes":[[[x,y,p,t],[x,y,p,t],...],...]}
//
// Fields:
//
//	v       format version (1)
//	ts      unix ms when logged
//	model   hex CRC32 of the model.bin that predicted (Model.ID)
//	source  "correction": player changed a digit we read wrongly;
//	        "reject": we said Unknown and the player supplied the digit;
//	        "false-accept": we read a digit but it was not one (label 0);
//	        "confirmed": optional, an accepted read the player kept
//	kind    the mark kind we assigned ("answer", "candidate", "unknown")
//	label   the TRUE digit 1..9, or 0 = "not a digit" (scribble, letter)
//	pred    the digit we produced, 0 if we rejected it
//	conf    our confidence; top3 our ranking [[digit, prob], ...]
//	cell    the cell rect [x0,y0,x1,y1] in screen px
//	strokes only this mark's strokes, screen px: [x, y, pressure, t_ms]
type Sample struct {
	V       int            `json:"v"`
	TS      int64          `json:"ts"`
	Model   string         `json:"model"`
	Source  string         `json:"source"`
	Kind    string         `json:"kind"`
	Label   int            `json:"label"`
	Pred    int            `json:"pred"`
	Conf    float32        `json:"conf"`
	Top3    [][2]float64   `json:"top3"`
	Cell    [4]float32     `json:"cell"`
	Strokes [][][4]float64 `json:"strokes"`
}

// NewSample builds a log record for mark (as returned by Analyze over
// strokes) whose true value is label (1..9, or 0 for "not a digit").
func NewSample(m *Model, cell Rect, strokes []Stroke, mark Mark, label int, source string, now time.Time) Sample {
	s := Sample{
		V: 1, TS: now.UnixMilli(), Source: source, Kind: mark.Kind.String(),
		Label: label, Pred: mark.Digit, Conf: mark.Confidence,
		Cell: [4]float32{cell.X0, cell.Y0, cell.X1, cell.Y1},
	}
	if m != nil {
		s.Model = fmt.Sprintf("%08x", m.ID)
	}
	for _, g := range mark.Top3 {
		if g.Digit != 0 {
			s.Top3 = append(s.Top3, [2]float64{float64(g.Digit), float64(g.Prob)})
		}
	}
	for _, i := range mark.Strokes {
		if i < 0 || i >= len(strokes) {
			continue
		}
		st := make([][4]float64, len(strokes[i]))
		for k, p := range strokes[i] {
			st[k] = [4]float64{float64(p.X), float64(p.Y), float64(p.P), float64(p.T)}
		}
		s.Strokes = append(s.Strokes, st)
	}
	return s
}

// AppendSample writes s as one JSON line.
func AppendSample(w io.Writer, s Sample) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}

// ReadSamples parses a JSONL log, skipping blank lines. A line that doesn't
// parse (say, torn by a power loss mid-append) is skipped too, so it costs
// only itself; the first such error is returned along with the samples.
func ReadSamples(r io.Reader) ([]Sample, error) {
	var out []Sample
	var bad error
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	for n := 1; sc.Scan(); n++ {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var s Sample
		if err := json.Unmarshal(line, &s); err != nil {
			if bad == nil {
				bad = fmt.Errorf("hwr: sample line %d: %w", n, err)
			}
			continue
		}
		out = append(out, s)
	}
	if err := sc.Err(); err != nil {
		return out, err
	}
	return out, bad
}

// StrokesOf converts a logged sample back to strokes.
func (s Sample) StrokesOf() []Stroke {
	out := make([]Stroke, len(s.Strokes))
	for i, st := range s.Strokes {
		out[i] = make(Stroke, len(st))
		for k, p := range st {
			out[i][k] = Point{X: float32(p[0]), Y: float32(p[1]), P: float32(p[2]), T: int64(p[3])}
		}
	}
	return out
}
