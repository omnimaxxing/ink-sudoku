package hwr

import (
	"math"
	"testing"
	"time"
)

// line is one straight stroke of height h centred at (x, y), leaning deg
// degrees from vertical.
func line(x, y, h float32, deg float64) []Stroke {
	dx := float32(math.Tan(deg*math.Pi/180)) * h / 2
	return []Stroke{densePoly([2]float32{x - dx, y - h/2}, [2]float32{x + dx, y + h/2})}
}

func sampleOf(strokes []Stroke, label int) Sample {
	m := Mark{Strokes: make([]int, len(strokes))}
	for i := range m.Strokes {
		m.Strokes[i] = i
	}
	return NewSample(DefaultModel(), cell, strokes, m, label, "correction", time.UnixMilli(0))
}

func TestEmbed(t *testing.T) {
	m := DefaultModel()
	e := m.Embed(line(150, 150, 60, 10))
	if len(e) != m.EmbedDim() {
		t.Fatalf("len %d, want EmbedDim %d", len(e), m.EmbedDim())
	}
	var n float64
	for _, v := range e {
		n += float64(v) * float64(v)
	}
	if math.Abs(n-1) > 1e-4 {
		t.Errorf("|e|^2 = %v, want 1", n)
	}
}

func TestPersonalEmptyIsModel(t *testing.T) {
	m := DefaultModel()
	p := NewPersonal(m, nil)
	p.Add(Sample{Label: 3}) // no ink: ignored
	p.Add(sampleOf(line(150, 150, 60, 0), 12))
	if p.Len() != 0 {
		t.Fatalf("Len %d after unusable samples, want 0", p.Len())
	}
	for _, g := range [][]Stroke{line(150, 150, 60, 0), line(150, 150, 60, 10), glyph(140, 120, 90, 0, unitSeven)} {
		if got, want := p.Classify(g, 0), m.Classify(g, 0); got != want {
			t.Errorf("empty Personal %+v, model %+v", got, want)
		}
	}
}

// The player's own samples lift ink the model rejects, and a "not a digit"
// sample vetoes ink the model accepts.
func TestPersonalLiftAndVeto(t *testing.T) {
	m := DefaultModel()
	slanted := line(400, 300, 70, 10)
	if r := m.Classify(slanted, 0); r.Digit != 0 {
		t.Fatalf("precondition: model reads the slanted bar as %d, want a reject", r.Digit)
	}
	p := NewPersonal(m, []Sample{sampleOf(line(150, 150, 40, 8), 1), sampleOf(glyph(140, 120, 90, 0, unitSeven), 7)})
	if p.Len() != 2 {
		t.Fatalf("Len %d, want 2", p.Len())
	}
	if r := p.Classify(slanted, 0); r.Digit != 1 || r.Top3[0].Digit != 1 {
		t.Errorf("with a similar 1 on file: %+v, want 1", r)
	}

	upright := line(400, 300, 70, 0)
	if r := m.Classify(upright, 0); r.Digit != 1 {
		t.Fatalf("precondition: model reads the upright bar as %d, want 1", r.Digit)
	}
	p.Add(sampleOf(line(150, 150, 50, 0), 0))
	if r := p.Classify(upright, 0); r.Digit != 0 {
		t.Errorf("after a 'not a digit' sample of the same ink: %+v, want a reject", r)
	}
}

// One correction must not make the player's other digits harder to read:
// the stricter personal threshold only applies to digits they gave samples
// of.
func TestPersonalSparse(t *testing.T) {
	m := DefaultModel()
	upright := line(400, 300, 70, 0)
	base := m.Classify(upright, 0)
	if base.Digit != 1 || base.Confidence >= PersonalThreshold {
		t.Fatalf("precondition: model reads the upright bar as %d at %.3f, want 1 below %v", base.Digit, base.Confidence, PersonalThreshold)
	}
	p := NewPersonal(m, []Sample{sampleOf(glyph(140, 120, 90, 0, unitSeven), 7)})
	if r := p.Classify(upright, 0); r.Digit != 1 {
		t.Errorf("after one 7: %+v, want 1 as before", r)
	}
}

func TestPersonalCap(t *testing.T) {
	var samples []Sample
	for i := 0; i < PersonalMaxPerLabel+3; i++ {
		samples = append(samples, sampleOf(line(150, 150, 40+float32(i), 0), 4))
	}
	samples = append(samples, sampleOf(line(150, 150, 40, 0), 5), Sample{Label: 6}) // no ink: ignored
	p := NewPersonal(DefaultModel(), samples)
	if p.Len() != PersonalMaxPerLabel+1 || PersonalLen(samples) != p.Len() {
		t.Errorf("Len %d, PersonalLen %d, want %d", p.Len(), PersonalLen(samples), PersonalMaxPerLabel+1)
	}
}

// Leave-one-out: a lone sample cannot vouch for itself, a second copy can.
// The samples carry no cell, so the game's lone-stroke rescue can't apply.
func TestEvaluatePersonal(t *testing.T) {
	m := DefaultModel()
	one := sampleOf(line(150, 150, 60, 12), 1)
	if b, a := EvaluatePersonal(m, []Sample{one}); b != 1 || a != 1 {
		t.Errorf("straight 1 in a known cell: before %v after %v, want 1 1 (the game reads it as a 1)", b, a)
	}
	one.Cell = [4]float32{}
	if b, a := EvaluatePersonal(m, []Sample{one}); b != 0 || a != 0 {
		t.Errorf("single rejected sample: before %v after %v, want 0 0", b, a)
	}
	two := sampleOf(line(300, 300, 50, 11), 1)
	two.Cell = [4]float32{}
	if b, a := EvaluatePersonal(m, []Sample{one, two}); b != 0 || a != 1 {
		t.Errorf("two similar samples: before %v after %v, want 0 1", b, a)
	}
	if b, a := EvaluatePersonal(m, nil); b != 0 || a != 0 {
		t.Errorf("no samples: %v %v", b, a)
	}
}
