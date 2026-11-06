package hwr

// Point is one digitizer sample in screen pixels (1404x1872, y down,
// ~229 dpi). P is pen pressure 0..1 and T a millisecond timestamp.
// Recognition uses X/Y for shape and T for grouping; P is ignored.
type Point struct {
	X, Y, P float32
	T       int64
}

// Stroke is the ordered samples between one pen-down and the next pen-up.
type Stroke []Point

// Rect is an axis-aligned box in screen pixels; X0<=X1, Y0<=Y1.
type Rect struct {
	X0, Y0, X1, Y1 float32
}

func (r Rect) W() float32 { return r.X1 - r.X0 }
func (r Rect) H() float32 { return r.Y1 - r.Y0 }

// Center returns the midpoint of r.
func (r Rect) Center() (x, y float32) { return (r.X0 + r.X1) / 2, (r.Y0 + r.Y1) / 2 }

// Union returns the smallest Rect containing r and o.
func (r Rect) Union(o Rect) Rect {
	return Rect{min(r.X0, o.X0), min(r.Y0, o.Y0), max(r.X1, o.X1), max(r.Y1, o.Y1)}
}

// Intersect returns the overlap of r and o (zero Rect if disjoint).
func (r Rect) Intersect(o Rect) Rect {
	x := Rect{max(r.X0, o.X0), max(r.Y0, o.Y0), min(r.X1, o.X1), min(r.Y1, o.Y1)}
	if x.X1 < x.X0 || x.Y1 < x.Y0 {
		return Rect{}
	}
	return x
}

// Area returns W*H.
func (r Rect) Area() float32 { return r.W() * r.H() }

// Gap is the Euclidean distance between the closest points of r and o
// (0 when they touch or overlap).
func (r Rect) Gap(o Rect) float32 {
	dx := max(0, max(o.X0-r.X1, r.X0-o.X1))
	dy := max(0, max(o.Y0-r.Y1, r.Y0-o.Y1))
	return sqrt32(dx*dx + dy*dy)
}

// Bounds is the bounding box of all points in s. ok is false when s has no
// points.
func Bounds(s ...Stroke) (r Rect, ok bool) {
	for _, st := range s {
		for _, p := range st {
			if !ok {
				r = Rect{p.X, p.Y, p.X, p.Y}
				ok = true
				continue
			}
			r.X0 = min(r.X0, p.X)
			r.Y0 = min(r.Y0, p.Y)
			r.X1 = max(r.X1, p.X)
			r.Y1 = max(r.Y1, p.Y)
		}
	}
	return r, ok
}

// MarkKind is what a group of ink in a cell means.
type MarkKind int

const (
	// Unknown is ink that is not a confidently recognised digit (or is too
	// small / oddly placed to classify). The UI should leave it as ink.
	Unknown MarkKind = iota
	// Answer is a big digit: the player's value for the cell.
	Answer
	// Candidate is a small pencil-mark digit.
	Candidate
	// ScratchOut is a zig-zag over existing ink: erase what it covers.
	ScratchOut
)

func (k MarkKind) String() string {
	switch k {
	case Answer:
		return "answer"
	case Candidate:
		return "candidate"
	case ScratchOut:
		return "scratchout"
	}
	return "unknown"
}

// Guess is one entry of a classifier ranking.
type Guess struct {
	Digit int     // 1..9
	Prob  float32 // softmax probability
}

// Mark is one group of strokes inside a cell and what it means.
type Mark struct {
	Kind MarkKind
	// Digit is 1..9 for Answer/Candidate; 0 for Unknown and ScratchOut.
	Digit int
	// Confidence is the classifier probability of Digit (for Unknown, of
	// the best rejected guess; for ScratchOut, 1).
	Confidence float32
	// Top3 are the three most likely digits 1..9 (zero for ScratchOut).
	Top3 [3]Guess
	// BBox is the ink bounding box in screen px.
	BBox Rect
	// Strokes are indices into the slice passed to Analyze, in time order.
	Strokes []int
	// Erases (ScratchOut only) are the indices of earlier non-scratch
	// strokes the scratch-out covers.
	Erases []int
}
