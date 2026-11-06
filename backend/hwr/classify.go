package hwr

import "math"

// Result is the classifier's verdict on one glyph.
type Result struct {
	// Digit is 1..9 when accepted, 0 when rejected.
	Digit int
	// Confidence is the softmax probability of the arg-max output (for a
	// rejected glyph this may belong to '0' or the reject class).
	Confidence float32
	// Top3 ranks the digits 1..9 only, regardless of acceptance.
	Top3 [3]Guess
	// Class is the arg-max output label: '1'..'9', '0', or '?' (reject).
	Class byte
}

// Classify rasterises strokes and classifies them. threshold <= 0 uses the
// model's calibrated Threshold.
//
// Accept rule: the arg-max over ALL outputs (digits 0-9 and the explicit
// reject class) must be one of 1..9 and its probability >= threshold.
// Anything else (a '0', the reject class, or a low-confidence digit) is a
// reject: Digit = 0.
func (m *Model) Classify(strokes []Stroke, threshold float32) Result {
	img := Rasterize(strokes)
	return m.ClassifyImage(&img, threshold)
}

// ClassifyImage classifies an already rasterised 28x28 image.
func (m *Model) ClassifyImage(img *[RasterSize * RasterSize]float32, threshold float32) Result {
	return m.decide(softmax(m.Logits(img)), threshold)
}

// decide applies the accept rule to softmax probabilities p (one per label)
// and ranks the digits 1..9.
func (m *Model) decide(p []float32, threshold float32) Result {
	if threshold <= 0 {
		threshold = m.Threshold
	}
	best := 0
	for i := range p {
		if p[i] > p[best] {
			best = i
		}
	}
	r := Result{Confidence: p[best], Class: m.Labels[best]}
	if c := m.Labels[best]; c >= '1' && c <= '9' && p[best] >= threshold {
		r.Digit = int(c - '0')
	}
	n := 0
	used := make([]bool, len(p))
	for n < 3 {
		k := -1
		for i, c := range m.Labels {
			if c >= '1' && c <= '9' && !used[i] && (k < 0 || p[i] > p[k]) {
				k = i
			}
		}
		if k < 0 {
			break
		}
		used[k] = true
		r.Top3[n] = Guess{Digit: int(m.Labels[k] - '0'), Prob: p[k]}
		n++
	}
	return r
}

func softmax(z []float32) []float32 {
	mx := z[0]
	for _, v := range z {
		mx = max(mx, v)
	}
	out := make([]float32, len(z))
	var s float64
	for i, v := range z {
		e := math.Exp(float64(v - mx))
		out[i] = float32(e)
		s += e
	}
	for i := range out {
		out[i] = float32(float64(out[i]) / s)
	}
	return out
}
