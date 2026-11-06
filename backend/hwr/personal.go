package hwr

import (
	"math"
	"sync"
)

// Personal adapts recognition to one player's handwriting, on the tablet,
// without retraining: it keeps the player's labelled samples (calibration
// page + corrections) and blends them with the CNN's prediction. It
// implements Classifier, so it drops into Analyzer.
//
// Each sample is stored as its Embed vector (EmbedDim float32s) and label;
// strokes are not kept. A glyph is read with one CNN pass that yields both
// the softmax p and the embedding e. For every label c the player has
// samples of, s_c is the highest cosine between e and those samples, and
// the log-probability of output c gets a bonus
//
//	PersonalBonus * (s_c - PersonalSimFloor) / (1 - PersonalSimFloor)   when s_c > PersonalSimFloor
//
// (label 0, "not a digit", boosts the reject output). The renormalised
// result goes through the usual accept rule, but at PersonalThreshold,
// which is stricter than the model's own: ink that looks like the player's
// own digits is lifted above it, ink that looks like nothing they wrote has
// to clear a higher bar than before (only for digits the player has given
// samples of; see PersonalFullAt). A confident misread can flip once the
// player has corrected similar ink (it first turns Unknown, then into the
// corrected digit as the evidence grows).
//
// The constants were tuned on writers the model never saw (hwrtrain "dev"
// writers) and checked on others ("test"); see TestPersonalSimulation. On
// the test writers a 27-digit calibration page took accuracy from 95.1% to
// 98.1% and 54 samples to 98.9%, with fewer misreads and fewer false
// accepts on letters than the plain model.
//
// Cost per read: the plain model's Classify plus one EmbedDim dot product
// per stored sample. Each label keeps its newest PersonalMaxPerLabel
// samples. Personal is safe for concurrent use.
type Personal struct {
	model *Model
	tune  personalTuning
	out   [10]int // model output index per personal label (0 -> reject), -1 if absent

	mu     sync.RWMutex
	emb    []float32 // len(labels) x EmbedDim unit vectors, oldest first
	labels []int8    // 0 = not a digit, 1..9
}

// Personalization constants, tuned on held-out writers (see Personal).
const (
	// PersonalSimFloor is the cosine similarity above which a stored sample
	// starts to vote for its label.
	PersonalSimFloor = 0.6
	// PersonalBonus is the log-probability bonus of an exact match.
	PersonalBonus = 20
	// PersonalThreshold is the accept threshold for the blended
	// probabilities of a digit the player has at least PersonalFullAt
	// samples of (when the caller passes threshold <= 0). With fewer it
	// is interpolated from the model's own Threshold.
	PersonalThreshold = 0.995
	// PersonalFullAt is the per-digit sample count from which the full
	// PersonalThreshold applies (a calibration page has 3 of each).
	PersonalFullAt = 3
	// PersonalMaxPerLabel bounds memory and per-read cost: adding a sample
	// to a label that already has this many drops that label's oldest.
	PersonalMaxPerLabel = 24
)

var _ Classifier = (*Personal)(nil)

type personalTuning struct {
	floor, bonus, threshold float32
}

// NewPersonal builds a personal classifier from labelled samples
// (Sample.Label is the true digit). It runs the CNN once per sample.
func NewPersonal(m *Model, samples []Sample) *Personal {
	p := &Personal{model: m, tune: personalTuning{PersonalSimFloor, PersonalBonus, PersonalThreshold}}
	for d := range p.out {
		want := byte('0' + d)
		if d == 0 {
			want = '?'
		}
		p.out[d] = -1
		for i, c := range m.Labels {
			if c == want {
				p.out[d] = i
			}
		}
	}
	for _, s := range samples {
		p.Add(s)
	}
	return p
}

// Add learns one more labelled sample: Label 1..9 is the digit the ink
// really is, 0 means "not a digit". Samples without ink or with another
// label are ignored.
func (p *Personal) Add(s Sample) {
	if !usable(s) {
		return
	}
	e := p.model.Embed(s.StrokesOf())
	p.mu.Lock()
	p.add(e, s.Label)
	p.mu.Unlock()
}

func (p *Personal) add(e []float32, label int) {
	dim := len(e)
	n, oldest := 0, -1
	for i, l := range p.labels {
		if int(l) == label {
			if oldest < 0 {
				oldest = i
			}
			n++
		}
	}
	if n >= PersonalMaxPerLabel {
		p.labels = append(p.labels[:oldest], p.labels[oldest+1:]...)
		p.emb = append(p.emb[:oldest*dim], p.emb[(oldest+1)*dim:]...)
	}
	p.emb = append(p.emb, e...)
	p.labels = append(p.labels, int8(label))
}

// usable reports whether Add learns s.
func usable(s Sample) bool { return s.Label >= 0 && s.Label <= 9 && len(s.Strokes) > 0 }

// Len reports how many samples back the personalization (after dropping
// unusable ones and the per-label cap).
func (p *Personal) Len() int {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return len(p.labels)
}

// PersonalLen is the Len of a Personal built from samples, worked out
// without running the CNN (for while one is being built).
func PersonalLen(samples []Sample) int {
	var per [10]int
	for _, s := range samples {
		if usable(s) {
			per[s.Label] = min(per[s.Label]+1, PersonalMaxPerLabel)
		}
	}
	n := 0
	for _, c := range per {
		n += c
	}
	return n
}

// Classify reads one glyph like Model.Classify, blended with the player's
// samples. threshold <= 0 means the personal accept threshold (see
// Personal); with no samples a Personal reads exactly like its model.
// Confidence and Top3 are the blended probabilities.
func (p *Personal) Classify(strokes []Stroke, threshold float32) Result {
	img := Rasterize(strokes)
	e := make([]float32, p.model.EmbedDim())
	prob := softmax(p.model.forward(&img, e))
	normalize(e)
	p.mu.RLock()
	ev := p.similarity(e, -1)
	p.mu.RUnlock()
	return p.decide(prob, &ev, threshold)
}

// evidence is what the player's samples say about one glyph.
type evidence struct {
	best  [10]float32 // highest cosine per personal label, -1 if none
	count [10]int     // samples per personal label
}

// similarity compares unit vector e with every sample except skip.
func (p *Personal) similarity(e []float32, skip int) evidence {
	var ev evidence
	for i := range ev.best {
		ev.best[i] = -1
	}
	dim := len(e)
	for i, l := range p.labels {
		if i == skip {
			continue
		}
		ev.best[l] = max(ev.best[l], dot(e, p.emb[i*dim:(i+1)*dim]))
		ev.count[l]++
	}
	return ev
}

// dot is the inner product of equal-length a and b, with four independent
// accumulators so the in-order A55 is not stalled on one add chain.
func dot(a, b []float32) float32 {
	b = b[:len(a)]
	var s0, s1, s2, s3 float32
	i := 0
	for ; i+4 <= len(a); i += 4 {
		s0 += a[i] * b[i]
		s1 += a[i+1] * b[i+1]
		s2 += a[i+2] * b[i+2]
		s3 += a[i+3] * b[i+3]
	}
	for ; i < len(a); i++ {
		s0 += a[i] * b[i]
	}
	return (s0 + s1) + (s2 + s3)
}

// decide blends CNN probabilities with the similarity evidence. With the
// default threshold, the winning digit d must reach the model's Threshold
// raised toward PersonalThreshold in proportion to how many samples of d
// the player has (fully from PersonalFullAt on): the stricter bar is only
// fair where the player has shown us their d, so a single correction does
// not make every other digit harder to accept.
func (p *Personal) decide(prob []float32, ev *evidence, threshold float32) Result {
	t := p.tune
	z := make([]float32, len(prob))
	for i, v := range prob {
		z[i] = float32(math.Log(float64(max(v, 1e-30))))
	}
	for c, s := range ev.best {
		if o := p.out[c]; o >= 0 && s > t.floor {
			z[o] += t.bonus * (s - t.floor) / (1 - t.floor)
		}
	}
	f := softmax(z)
	if threshold <= 0 {
		threshold = p.model.Threshold
		top := 0
		for i := range f {
			if f[i] > f[top] {
				top = i
			}
		}
		if c := p.model.Labels[top]; c >= '1' && c <= '9' && t.threshold > threshold {
			k := min(1, float32(ev.count[c-'0'])/PersonalFullAt)
			threshold += k * (t.threshold - threshold)
		}
	}
	return p.model.decide(f, threshold)
}

// EvaluatePersonal estimates, by leave-one-out over the player's own
// labelled samples, how often the plain model and the personalized
// classifier read them correctly (fractions 0..1). A reject counts as
// wrong for a digit and right for a label-0 ("not a digit") sample. Each
// sample is read by a Personal built from all the others (no per-label
// cap), at the default thresholds. Samples Add would ignore are skipped.
func EvaluatePersonal(m *Model, samples []Sample) (before, after float64) {
	p := NewPersonal(m, nil)
	dim := m.EmbedDim()
	var probs [][]float32
	var kept []Sample
	for _, s := range samples {
		if s.Label < 0 || s.Label > 9 || len(s.Strokes) == 0 {
			continue
		}
		kept = append(kept, s)
		img := Rasterize(s.StrokesOf())
		e := make([]float32, dim)
		probs = append(probs, softmax(m.forward(&img, e)))
		normalize(e)
		p.emb = append(p.emb, e...)
		p.labels = append(p.labels, int8(s.Label))
	}
	if len(probs) == 0 {
		return 0, 0
	}
	nb, na := 0, 0
	for i, l := range p.labels {
		if gameRead(kept[i], m.decide(probs[i], 0)).Digit == int(l) {
			nb++
		}
		ev := p.similarity(p.emb[i*dim:(i+1)*dim], i)
		if gameRead(kept[i], p.decide(probs[i], &ev, 0)).Digit == int(l) {
			na++
		}
	}
	return float64(nb) / float64(len(probs)), float64(na) / float64(len(probs))
}

// gameRead applies Analyze's rescue of a lone straight stroke as a 1, so
// EvaluatePersonal reports what the game would actually read.
func gameRead(s Sample, r Result) Result {
	cellH := s.Cell[3]
	if r.Digit != 0 || len(s.Strokes) != 1 || cellH <= 0 {
		return r
	}
	st := s.StrokesOf()[0]
	b, _ := Bounds(st)
	cfg := DefaultConfig()
	if loneOne(strokeInfo{bbox: b, pts: resample(st, max(1.5, cellH/80))}, cfg, cellH) {
		return asOne(r, cfg.LoneOneConfidence)
	}
	return r
}
