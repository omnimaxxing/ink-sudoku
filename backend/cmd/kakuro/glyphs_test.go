package main

import "math"

// Synthetic handwriting for tests, copied from cmd/sudoku/glyphs_test.go:
// the single-stroke digit font of the Mac stub
// (dev/stubs/net/asivery/AppLoad/inksudoku.js), without its jitter.
// Strokes come out as the frontend sends them: flat x, y, t, p in grid px,
// x and y rounded to 0.1, t in whole ms from the stroke's start, p 1.

type glyphPiece struct {
	curve bool // Catmull-Rom through pts; else a polyline
	pts   [][2]float64
}

// glyphFont: unit-box digit strokes (x 0–0.75, y 0–1), each a list of pieces.
var glyphFont = map[int][][]glyphPiece{
	1: {{{true, [][2]float64{{0.16, 0.22}, {0.42, 0.0}}}, {false, [][2]float64{{0.42, 0.0}, {0.4, 0.5}, {0.37, 1.0}}}}},
	2: {{{true, [][2]float64{{0.04, 0.26}, {0.2, 0.05}, {0.42, 0.0}, {0.62, 0.12}, {0.6, 0.36}, {0.36, 0.64}, {0.02, 0.98}}},
		{true, [][2]float64{{0.02, 0.98}, {0.36, 0.93}, {0.74, 0.97}}}}},
	3: {{{true, [][2]float64{{0.06, 0.12}, {0.34, 0.0}, {0.6, 0.1}, {0.58, 0.3}, {0.3, 0.46}}},
		{true, [][2]float64{{0.3, 0.46}, {0.62, 0.55}, {0.68, 0.8}, {0.45, 0.98}, {0.15, 0.97}, {0.0, 0.84}}}}},
	4: {{{false, [][2]float64{{0.5, 0.0}, {0.03, 0.66}, {0.76, 0.63}}}}, {{true, [][2]float64{{0.56, 0.28}, {0.54, 0.62}, {0.5, 1.02}}}}},
	5: {{{false, [][2]float64{{0.68, 0.02}, {0.18, 0.03}, {0.12, 0.45}}},
		{true, [][2]float64{{0.12, 0.45}, {0.4, 0.36}, {0.66, 0.48}, {0.7, 0.75}, {0.48, 0.97}, {0.2, 0.98}, {0.02, 0.86}}}}},
	6: {{{true, [][2]float64{{0.62, 0.0}, {0.34, 0.18}, {0.12, 0.5}, {0.12, 0.82}, {0.35, 1.0}, {0.62, 0.9}, {0.66, 0.65},
		{0.42, 0.52}, {0.18, 0.62}, {0.12, 0.76}}}}},
	7: {{{false, [][2]float64{{0.02, 0.05}, {0.72, 0.03}}}, {true, [][2]float64{{0.72, 0.03}, {0.48, 0.42}, {0.36, 0.72}, {0.3, 1.0}}}}},
	8: {{{true, [][2]float64{{0.6, 0.12}, {0.38, 0.0}, {0.12, 0.12}, {0.15, 0.32}, {0.45, 0.5}, {0.7, 0.7}, {0.6, 0.95},
		{0.32, 1.0}, {0.1, 0.85}, {0.15, 0.65}, {0.45, 0.48}, {0.62, 0.3}, {0.6, 0.12}}}}},
	9: {{{true, [][2]float64{{0.66, 0.18}, {0.42, 0.02}, {0.15, 0.1}, {0.12, 0.35}, {0.38, 0.46}, {0.62, 0.33}, {0.66, 0.18}}},
		{false, [][2]float64{{0.66, 0.18}, {0.6, 0.6}, {0.52, 1.0}}}}},
}

func catmullRom(p [][2]float64, step float64) [][2]float64 {
	var out [][2]float64
	for i := 0; i < len(p)-1; i++ {
		p0, p1, p2, p3 := p[max(0, i-1)], p[i], p[i+1], p[min(len(p)-1, i+2)]
		n := max(2, int(math.Ceil(math.Hypot(p2[0]-p1[0], p2[1]-p1[1])/step)))
		k0 := 1
		if i == 0 {
			k0 = 0
		}
		for k := k0; k <= n; k++ {
			t := float64(k) / float64(n)
			t2, t3 := t*t, t*t*t
			f := func(j int) float64 {
				return 0.5 * (2*p1[j] + (-p0[j]+p2[j])*t + (2*p0[j]-5*p1[j]+4*p2[j]-p3[j])*t2 + (-p0[j]+3*p1[j]-3*p2[j]+p3[j])*t3)
			}
			out = append(out, [2]float64{f(0), f(1)})
		}
	}
	return out
}

func polyline(p [][2]float64, step float64) [][2]float64 {
	out := [][2]float64{p[0]}
	for i := 0; i < len(p)-1; i++ {
		n := max(1, int(math.Ceil(math.Hypot(p[i+1][0]-p[i][0], p[i+1][1]-p[i][1])/step)))
		for k := 1; k <= n; k++ {
			f := float64(k) / float64(n)
			out = append(out, [2]float64{p[i][0] + (p[i+1][0]-p[i][0])*f, p[i][1] + (p[i+1][1]-p[i][1])*f})
		}
	}
	return out
}

// glyph writes digit d with glyph height h centred on (cx, cy), in grid px.
func glyph(d int, cx, cy, h float64) [][]float64 {
	var out [][]float64
	for _, stroke := range glyphFont[d] {
		var pts [][2]float64
		for _, pc := range stroke {
			var s [][2]float64
			if pc.curve {
				s = catmullRom(pc.pts, 2.5/h)
			} else {
				s = polyline(pc.pts, 2.5/h)
			}
			if len(pts) > 0 {
				s = s[1:]
			}
			pts = append(pts, s...)
		}
		var flat [][2]float64
		for _, u := range pts {
			x, y := (u[0]-0.36)*h*0.92, (u[1]-0.5)*h
			x -= 0.12 * y
			flat = append(flat, [2]float64{cx + x, cy + y})
		}
		out = append(out, penStroke(flat))
	}
	return out
}

// penStroke times points at 0.3 px/ms, as a steady hand writes.
func penStroke(pts [][2]float64) []float64 {
	var out []float64
	t := 0.0
	for i, p := range pts {
		if i > 0 {
			t += math.Hypot(p[0]-pts[i-1][0], p[1]-pts[i-1][1]) / 0.3
		}
		out = append(out, math.Round(p[0]*10)/10, math.Round(p[1]*10)/10, math.Round(t), 1)
	}
	return out
}

// zigzag is a scratch-out: legs back and forth across (x0, y0)–(x1, y1).
func zigzag(x0, y0, x1, y1 float64, legs int) []float64 {
	var pts [][2]float64
	for i := 0; i <= legs; i++ {
		x := x0
		if i%2 == 1 {
			x = x1
		}
		pts = append(pts, [2]float64{x, y0 + (y1-y0)*float64(i)/float64(legs)})
	}
	return penStroke(polyline(pts, 3))
}

// ring is an "o": not a digit the recognizer accepts.
func ring(cx, cy, r float64) []float64 {
	var pts [][2]float64
	for i := 0; i <= 40; i++ {
		a := -math.Pi/2 + 2*math.Pi*float64(i)/38
		pts = append(pts, [2]float64{cx + r*0.8*math.Cos(a), cy + r*math.Sin(a)})
	}
	return penStroke(pts)
}
