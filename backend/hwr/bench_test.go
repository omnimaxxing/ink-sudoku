package hwr

import (
	"math"
	"testing"
)

// tabletDense resamples a parity sample to ~2 px point spacing, roughly
// what a few-hundred-Hz digitiser produces at writing speed.
func tabletDense(strokes []Stroke) []Stroke {
	out := make([]Stroke, len(strokes))
	for i, s := range strokes {
		for k := 0; k+1 < len(s); k++ {
			a, b := s[k], s[k+1]
			n := max(1, int(math.Hypot(float64(b.X-a.X), float64(b.Y-a.Y))/2))
			for j := 0; j < n; j++ {
				t := float32(j) / float32(n)
				out[i] = append(out[i], Point{X: a.X + (b.X-a.X)*t, Y: a.Y + (b.Y-a.Y)*t})
			}
		}
		out[i] = append(out[i], s[len(s)-1])
	}
	return out
}

func benchGlyphs(b *testing.B) [][]Stroke {
	samples, _, _ := readParity(b)
	var gs [][]Stroke
	for _, s := range samples {
		if s.strokes != nil && s.label >= 1 && s.label <= 9 {
			gs = append(gs, tabletDense(s.strokes))
		}
		if len(gs) == 64 {
			break
		}
	}
	return gs
}

func BenchmarkRasterize(b *testing.B) {
	gs := benchGlyphs(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Rasterize(gs[i%len(gs)])
	}
}

func BenchmarkLogits(b *testing.B) {
	m := DefaultModel()
	img := Rasterize(benchGlyphs(b)[0])
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Logits(&img)
	}
}

// BenchmarkClassify is the per-digit cost: rasterise + CNN + softmax.
func BenchmarkClassify(b *testing.B) {
	m := DefaultModel()
	gs := benchGlyphs(b)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.Classify(gs[i%len(gs)], 0)
	}
}

// BenchmarkAnalyzeCell: one answer plus three pencil marks.
func BenchmarkAnalyzeCell(b *testing.B) {
	var strokes []Stroke
	strokes = append(strokes, glyph(106, 105, 30, 0, unitOne)...)
	strokes = append(strokes, glyph(206, 106, 30, 900, unitTwo)...)
	strokes = append(strokes, glyph(108, 200, 32, 1800, unitSeven)...)
	strokes = append(strokes, glyph(140, 120, 90, 10000, fourA, fourB)...)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		Analyze(cell, strokes)
	}
}

// BenchmarkPersonalClassify is the per-digit cost with personalization at
// its cap: nine digits x PersonalMaxPerLabel samples.
func BenchmarkPersonalClassify(b *testing.B) {
	m := DefaultModel()
	gs := benchGlyphs(b)
	p := NewPersonal(m, nil)
	for i := 0; i < 9*PersonalMaxPerLabel; i++ {
		p.add(m.Embed(gs[i%len(gs)]), 1+i%9)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		p.Classify(gs[i%len(gs)], 0)
	}
}
