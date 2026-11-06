package hwr

import (
	"math"
	"sort"
)

// Config holds the tunable thresholds of Analyze. Lengths are fractions of
// the cell height unless noted. Start from DefaultConfig and override.
type Config struct {
	// CellMargin grows the cell on every side when deciding which strokes
	// belong to it (digits overshoot grid lines). Default 0.12.
	CellMargin float32
	// MinInside is the fraction of a stroke's points that must lie in the
	// grown cell for it to be analysed. Default 0.6.
	MinInside float32

	// ScratchMinReversals is the number of sharp direction reversals a
	// single stroke needs to be a scratch-out. Default 3 (a 4-leg zig-zag).
	ScratchMinReversals int
	// ScratchReversalDeg is the turning angle (degrees) that counts as a
	// reversal, measured over a window of ~8% of the stroke's bbox diagonal
	// after arc-length resampling (so digitiser rate does not matter).
	// Default 120.
	ScratchReversalDeg float32
	// ScratchMinLengthRatio is the minimum path length / bbox diagonal.
	// Default 3.0.
	ScratchMinLengthRatio float32
	// ScratchMinSize is the minimum bbox diagonal of a scratch-out.
	// Default 0.18.
	ScratchMinSize float32
	// ScratchCover is the fraction of an earlier stroke's ink that must lie
	// inside the scratch-out's bbox (grown by 15%) for it to be erased.
	// Default 0.5.
	ScratchCover float32

	// GroupGap: two groups written in one go (pen-up pause <= GroupPause)
	// merge when the closest ink distance between them is <= GroupGap x the
	// larger group's size (max of bbox w, h). Default 0.22.
	GroupGap float32
	// GroupGapLate is the same limit after a longer pause: only touching /
	// crossing ink merges. Default 0.06.
	GroupGapLate float32
	// GroupPause is the pen-up pause in ms that separates "one go" from a
	// later addition. Default 1200.
	GroupPause int64
	// GroupMaxAspect refuses merges whose union is wider than this times
	// its height (keeps side-by-side pencil marks apart). Default 1.6.
	GroupMaxAspect float32
	// SatelliteRatio / SatelliteGap: after grouping, a group no bigger than
	// SatelliteRatio x a neighbour's size, written in the same go and within
	// SatelliteGap x that neighbour's size, joins it -- i-dots, accents,
	// the point of '!' -- so the classifier judges the whole glyph instead of
	// seeing a bare stem that looks like a 1. Defaults 0.5 and 0.5.
	SatelliteRatio float32
	SatelliteGap   float32

	// AnswerMinHeight: a mark at least this tall is an Answer. Default 0.40.
	AnswerMinHeight float32
	// CandidateMaxHeight: a mark at most this tall is a Candidate.
	// Default 0.33. Between the two, CenterTol decides.
	CandidateMaxHeight float32
	// CenterTol, CenterTolY: an in-between mark whose centre is within
	// CenterTol of the cell centre across and CenterTolY up or down is an
	// Answer, otherwise a Candidate. Defaults 0.18 and 0.28: answers are
	// centred across but often sit low (a short 1 that blocked a solve sat
	// 0.24 below the middle, 0.03 across), pencil marks go to the corners.
	CenterTol  float32
	CenterTolY float32
	// MinMarkSize: ink whose bbox max(w,h) is below this is Unknown and not
	// classified (specks). Default 0.06 (~8 px, under 1 mm, in a 131 px
	// cell). Real pencil marks go down to ~14 px; at the earlier 0.12 every
	// mark under ~16 px was dropped. The classifier is size-invariant, its
	// reject threshold handles ticks, and the frontend filters taps.
	MinMarkSize float32

	// AcceptProb is the classifier accept threshold; 0 uses the model's
	// calibrated Threshold.
	AcceptProb float32

	// Lone-1 rule: in a sudoku cell a single straight, near-vertical stroke
	// is a 1, but the CNN often scores a bare line just under its accept
	// threshold. When the classifier rejects a one-stroke mark (after
	// grouping, so a 4's or 7's pieces and i-dot satellites never qualify;
	// scratch-outs are removed before grouping) that is at least
	// LoneOneMinHeight tall, leans at most LoneOneMaxSlant degrees from
	// vertical (end-to-end), strays from the straight line between its ends
	// by at most LoneOneMaxBow x that line's length and is at most
	// (1+LoneOneMaxBow) x as long, it is read as 1 with Confidence
	// LoneOneConfidence. An accepted read is never overridden.
	// LoneOneMaxSlant <= 0 disables the rule.
	//
	// LoneOneMinHeight: fraction of the cell height; default 0.15 (~20 px in
	// a 131 px cell: dots and ticks stay Unknown, a ~30 px pencil-mark 1
	// qualifies). It applies on top of MinMarkSize.
	LoneOneMinHeight float32
	// LoneOneMaxSlant: degrees from vertical; default 25.
	LoneOneMaxSlant float32
	// LoneOneMaxBow: straightness tolerance; default 0.12 (a 1 with a flag
	// or a curved stroke goes to the classifier's verdict).
	LoneOneMaxBow float32
	// LoneOneConfidence is the Confidence reported for a lone-1 read;
	// default 0.99.
	LoneOneConfidence float32
}

// DefaultConfig returns the documented defaults.
func DefaultConfig() Config {
	return Config{
		CellMargin:            0.12,
		MinInside:             0.6,
		ScratchMinReversals:   3,
		ScratchReversalDeg:    120,
		ScratchMinLengthRatio: 3.0,
		ScratchMinSize:        0.18,
		ScratchCover:          0.5,
		GroupGap:              0.22,
		GroupGapLate:          0.06,
		GroupPause:            1200,
		GroupMaxAspect:        1.6,
		SatelliteRatio:        0.5,
		SatelliteGap:          0.5,
		AnswerMinHeight:       0.40,
		CandidateMaxHeight:    0.33,
		CenterTol:             0.18,
		CenterTolY:            0.28,
		MinMarkSize:           0.06,
		LoneOneMinHeight:      0.15,
		LoneOneMaxSlant:       25,
		LoneOneMaxBow:         0.12,
		LoneOneConfidence:     0.99,
	}
}

// Classifier is what Analyze needs from a recogniser; *Model implements it.
type Classifier interface {
	Classify(strokes []Stroke, threshold float32) Result
}

// Analyzer couples a classifier with thresholds.
type Analyzer struct {
	Classifier Classifier
	Config     Config
}

// Analyze uses the embedded model and DefaultConfig.
func Analyze(cell Rect, strokes []Stroke) []Mark {
	return Analyzer{DefaultModel(), DefaultConfig()}.Analyze(cell, strokes)
}

type pt struct{ x, y float32 }

type strokeInfo struct {
	idx    int // index in the caller's slice
	bbox   Rect
	pts    []pt // arc-length resampled
	t0, t1 int64
}

type group struct {
	members []int // positions in the infos slice
	bbox    Rect
	t0, t1  int64
}

// Analyze splits the ink in one cell into marks.
//
// strokes may contain ink from anywhere; only strokes with >= MinInside of
// their points inside cell (grown by CellMargin) are considered, and
// Mark.Strokes index into the slice as given. Strokes are taken in time
// order (first-point T, ties by index), so pass T where available.
//
// Pipeline: (1) every stroke that is a zig-zag (>= ScratchMinReversals
// sharp reversals and path length >= ScratchMinLengthRatio x bbox
// diagonal) becomes a ScratchOut mark, and earlier strokes it covers are
// listed in its Erases and dropped from further analysis; (2) the remaining
// strokes are grouped agglomeratively by closest-ink distance (GroupGap /
// GroupGapLate), so multi-stroke digits form one mark and separate pencil
// marks stay apart, and small satellites (dots, accents) join their
// neighbour; (3) each group is Answer or Candidate by its height
// relative to the cell (and centredness when in between), then classified;
// a rejected one-stroke group that is a straight near-vertical line is a 1
// (Config.LoneOneMaxSlant); any other rejected or too-small group is
// Unknown.
//
// Marks are returned in the time order of their first stroke.
func (a Analyzer) Analyze(cell Rect, strokes []Stroke) []Mark {
	cfg := a.Config
	ch := cell.H()
	if ch <= 0 {
		return nil
	}
	m := cfg.CellMargin * ch
	grown := Rect{cell.X0 - m, cell.Y0 - m, cell.X1 + m, cell.Y1 + m}

	var infos []strokeInfo
	for i, s := range strokes {
		if len(s) == 0 {
			continue
		}
		in := 0
		for _, p := range s {
			if p.X >= grown.X0 && p.X <= grown.X1 && p.Y >= grown.Y0 && p.Y <= grown.Y1 {
				in++
			}
		}
		if float32(in) < cfg.MinInside*float32(len(s)) {
			continue
		}
		b, _ := Bounds(s)
		infos = append(infos, strokeInfo{idx: i, bbox: b, pts: resample(s, max(1.5, ch/80)), t0: s[0].T, t1: s[len(s)-1].T})
	}
	sort.SliceStable(infos, func(i, j int) bool { return infos[i].t0 < infos[j].t0 })

	var marks []Mark
	erased := make([]bool, len(infos))
	scratch := make([]bool, len(infos))
	for i := range infos {
		if isScratch(strokes[infos[i].idx], cfg, ch) {
			scratch[i] = true
		}
	}
	for i := range infos {
		if !scratch[i] {
			continue
		}
		sb := infos[i].bbox
		gx, gy := 0.15*sb.W(), 0.15*sb.H()
		cover := Rect{sb.X0 - gx, sb.Y0 - gy, sb.X1 + gx, sb.Y1 + gy}
		mk := Mark{Kind: ScratchOut, Confidence: 1, BBox: sb, Strokes: []int{infos[i].idx}}
		for j := 0; j < i; j++ {
			if scratch[j] || erased[j] {
				continue
			}
			in := 0
			for _, p := range infos[j].pts {
				if p.x >= cover.X0 && p.x <= cover.X1 && p.y >= cover.Y0 && p.y <= cover.Y1 {
					in++
				}
			}
			if float32(in) >= cfg.ScratchCover*float32(len(infos[j].pts)) {
				erased[j] = true
				mk.Erases = append(mk.Erases, infos[j].idx)
			}
		}
		marks = append(marks, mk)
	}

	var groups []*group
	for i := range infos {
		if scratch[i] || erased[i] {
			continue
		}
		groups = append(groups, &group{members: []int{i}, bbox: infos[i].bbox, t0: infos[i].t0, t1: infos[i].t1})
	}
	for len(groups) > 1 {
		bi, bj, best := -1, -1, float32(math.Inf(1))
		for i := 0; i < len(groups); i++ {
			for j := i + 1; j < len(groups); j++ {
				gi, gj := groups[i], groups[j]
				size := max(gi.bbox.W(), gi.bbox.H(), gj.bbox.W(), gj.bbox.H(), 1)
				limit := cfg.GroupGap
				pause := max(gj.t0-gi.t1, gi.t0-gj.t1, 0)
				if pause > cfg.GroupPause {
					limit = cfg.GroupGapLate
				}
				if gi.bbox.Gap(gj.bbox) > limit*size {
					continue
				}
				u := gi.bbox.Union(gj.bbox)
				if u.W() > cfg.GroupMaxAspect*max(u.H(), 1) {
					continue
				}
				d := inkDistance(infos, gi, gj) / size
				if d <= limit && d < best {
					bi, bj, best = i, j, d
				}
			}
		}
		if bi < 0 {
			break
		}
		gi, gj := groups[bi], groups[bj]
		gi.members = append(gi.members, gj.members...)
		gi.bbox = gi.bbox.Union(gj.bbox)
		gi.t0, gi.t1 = min(gi.t0, gj.t0), max(gi.t1, gj.t1)
		groups = append(groups[:bj], groups[bj+1:]...)
	}
	gsize := func(g *group) float32 { return max(g.bbox.W(), g.bbox.H(), 1) }
	for merged := true; merged && len(groups) > 1; {
		merged = false
		for i, g := range groups {
			bj, best := -1, float32(math.Inf(1))
			for j, h := range groups {
				if j == i || gsize(g) > cfg.SatelliteRatio*gsize(h) ||
					max(h.t0-g.t1, g.t0-h.t1, 0) > cfg.GroupPause ||
					g.bbox.Gap(h.bbox) > cfg.SatelliteGap*gsize(h) {
					continue
				}
				if d := inkDistance(infos, g, h); d <= cfg.SatelliteGap*gsize(h) && d < best {
					bj, best = j, d
				}
			}
			if bj >= 0 {
				h := groups[bj]
				h.members = append(h.members, g.members...)
				h.bbox = h.bbox.Union(g.bbox)
				h.t0, h.t1 = min(h.t0, g.t0), max(h.t1, g.t1)
				groups = append(groups[:i], groups[i+1:]...)
				merged = true
				break
			}
		}
	}

	cx, cy := cell.Center()
	for _, g := range groups {
		sort.Ints(g.members)
		mk := Mark{BBox: g.bbox}
		ink := make([]Stroke, 0, len(g.members))
		for _, k := range g.members {
			mk.Strokes = append(mk.Strokes, infos[k].idx)
			ink = append(ink, strokes[infos[k].idx])
		}
		if max(g.bbox.W(), g.bbox.H()) >= cfg.MinMarkSize*ch {
			hr := g.bbox.H() / ch
			gx, gy := g.bbox.Center()
			dx, dy := abs32(gx-cx)/ch, abs32(gy-cy)/ch
			kind := Candidate
			if hr >= cfg.AnswerMinHeight || (hr > cfg.CandidateMaxHeight && dx <= cfg.CenterTol && dy <= cfg.CenterTolY) {
				kind = Answer
			}
			r := a.Classifier.Classify(ink, cfg.AcceptProb)
			if r.Digit == 0 && len(g.members) == 1 && loneOne(infos[g.members[0]], cfg, ch) {
				r = asOne(r, cfg.LoneOneConfidence)
			}
			mk.Top3 = r.Top3
			mk.Confidence = r.Confidence
			if r.Digit != 0 {
				mk.Kind, mk.Digit = kind, r.Digit
			}
		}
		marks = append(marks, mk)
	}
	first := func(mk Mark) int {
		for k := range infos {
			if infos[k].idx == mk.Strokes[0] {
				return k
			}
		}
		return 0
	}
	sort.SliceStable(marks, func(i, j int) bool { return first(marks[i]) < first(marks[j]) })
	return marks
}

// loneOne reports whether one stroke is a straight, near-vertical line of
// reasonable length (see Config.LoneOneMaxSlant).
func loneOne(st strokeInfo, cfg Config, cellH float32) bool {
	if cfg.LoneOneMaxSlant <= 0 || st.bbox.H() < cfg.LoneOneMinHeight*cellH || len(st.pts) < 2 {
		return false
	}
	a, b := st.pts[0], st.pts[len(st.pts)-1]
	dx, dy := b.x-a.x, b.y-a.y
	chord := sqrt32(dx*dx + dy*dy)
	if chord <= 0 || float64(abs32(dx)) > math.Tan(float64(cfg.LoneOneMaxSlant)*math.Pi/180)*float64(abs32(dy)) {
		return false
	}
	var path float32
	for i, p := range st.pts {
		if abs32((p.x-a.x)*dy-(p.y-a.y)*dx)/chord > cfg.LoneOneMaxBow*chord {
			return false
		}
		if i > 0 {
			q := st.pts[i-1]
			path += sqrt32((p.x-q.x)*(p.x-q.x) + (p.y-q.y)*(p.y-q.y))
		}
	}
	return path <= (1+cfg.LoneOneMaxBow)*chord
}

// asOne turns a rejected classifier result into a confident 1, keeping the
// classifier's other guesses as runners-up.
func asOne(r Result, conf float32) Result {
	out := Result{Digit: 1, Confidence: conf, Class: '1', Top3: [3]Guess{{Digit: 1, Prob: conf}}}
	n := 1
	for _, g := range r.Top3 {
		if g.Digit != 1 && g.Digit != 0 && n < 3 {
			out.Top3[n] = g
			n++
		}
	}
	return out
}

func abs32(v float32) float32 { return float32(math.Abs(float64(v))) }

// inkDistance is the smallest distance between resampled points of two
// groups (resampling spacing bounds the error).
func inkDistance(infos []strokeInfo, a, b *group) float32 {
	best := float32(math.Inf(1))
	for _, i := range a.members {
		for _, j := range b.members {
			for _, p := range infos[i].pts {
				for _, q := range infos[j].pts {
					dx, dy := p.x-q.x, p.y-q.y
					best = min(best, dx*dx+dy*dy)
				}
			}
		}
	}
	return sqrt32(best)
}

// resample walks the polyline and emits points every step pixels of arc
// length (always keeping both ends).
func resample(s Stroke, step float32) []pt {
	if len(s) == 0 {
		return nil
	}
	out := []pt{{s[0].X, s[0].Y}}
	var acc float32
	prev := pt{s[0].X, s[0].Y}
	for _, p := range s[1:] {
		cur := pt{p.X, p.Y}
		dx, dy := cur.x-prev.x, cur.y-prev.y
		seg := sqrt32(dx*dx + dy*dy)
		for seg > 0 && acc+seg >= step {
			t := (step - acc) / seg
			prev = pt{prev.x + dx*t, prev.y + dy*t}
			out = append(out, prev)
			dx, dy = cur.x-prev.x, cur.y-prev.y
			seg = sqrt32(dx*dx + dy*dy)
			acc = 0
		}
		acc += seg
		prev = cur
	}
	if last := (pt{s[len(s)-1].X, s[len(s)-1].Y}); out[len(out)-1] != last {
		out = append(out, last)
	}
	return out
}

// ScratchFeatures measures one stroke for scratch-out detection: the
// number of sharp reversals (turn >= reversalDeg over a ~8%-of-diagonal
// window) and path length / bbox diagonal. Exposed for calibration tools.
func ScratchFeatures(s Stroke, reversalDeg float32) (reversals int, lengthRatio, diag float32) {
	b, ok := Bounds(s)
	if !ok {
		return 0, 0, 0
	}
	diag = sqrt32(b.W()*b.W() + b.H()*b.H())
	if diag <= 0 {
		return 0, 0, 0
	}
	p := resample(s, diag*0.04)
	var length float32
	for i := 1; i < len(p); i++ {
		dx, dy := p[i].x-p[i-1].x, p[i].y-p[i-1].y
		length += sqrt32(dx*dx + dy*dy)
	}
	const k = 2
	cosThr := float32(math.Cos(float64(reversalDeg) * math.Pi / 180))
	turn := make([]float32, len(p)) // cosine of the turn; lower = sharper
	for i := range turn {
		turn[i] = 1
	}
	for i := k; i+k < len(p); i++ {
		ax, ay := p[i].x-p[i-k].x, p[i].y-p[i-k].y
		bx, by := p[i+k].x-p[i].x, p[i+k].y-p[i].y
		na, nb := sqrt32(ax*ax+ay*ay), sqrt32(bx*bx+by*by)
		if na > 0 && nb > 0 {
			turn[i] = (ax*bx + ay*by) / (na * nb)
		}
	}
	for i := k; i+k < len(p); i++ {
		if turn[i] > cosThr {
			continue
		}
		peak := true
		for j := max(0, i-k); j <= min(len(p)-1, i+k); j++ {
			if turn[j] < turn[i] || (turn[j] == turn[i] && j < i) {
				peak = false
				break
			}
		}
		if peak {
			reversals++
		}
	}
	return reversals, length / diag, diag
}

func isScratch(s Stroke, cfg Config, cellH float32) bool {
	rev, ratio, diag := ScratchFeatures(s, cfg.ScratchReversalDeg)
	return rev >= cfg.ScratchMinReversals && ratio >= cfg.ScratchMinLengthRatio && diag >= cfg.ScratchMinSize*cellH
}
