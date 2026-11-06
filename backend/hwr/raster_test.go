package hwr

import (
	"math"
	"testing"
)

func poly(pts ...[2]float32) Stroke {
	s := make(Stroke, len(pts))
	for i, p := range pts {
		s[i] = Point{X: p[0], Y: p[1], P: 0.5, T: int64(i * 4)}
	}
	return s
}

// densePoly interpolates so segments are ~1 px, like a real digitiser.
func densePoly(pts ...[2]float32) Stroke {
	var s Stroke
	for i := 0; i+1 < len(pts); i++ {
		a, b := pts[i], pts[i+1]
		n := int(math.Ceil(math.Hypot(float64(b[0]-a[0]), float64(b[1]-a[1])))) + 1
		for k := 0; k < n; k++ {
			t := float32(k) / float32(n)
			s = append(s, Point{X: a[0] + (b[0]-a[0])*t, Y: a[1] + (b[1]-a[1])*t, P: 0.5, T: int64(len(s) * 3)})
		}
	}
	last := pts[len(pts)-1]
	return append(s, Point{X: last[0], Y: last[1], T: int64(len(s) * 3)})
}

func com(img *[RasterSize * RasterSize]float32) (x, y float64) {
	var tot float64
	for r := 0; r < RasterSize; r++ {
		for c := 0; c < RasterSize; c++ {
			v := float64(img[r*RasterSize+c])
			tot += v
			x += v * (float64(c) + 0.5)
			y += v * (float64(r) + 0.5)
		}
	}
	return x / tot, y / tot
}

// inkSpan returns the number of rows and columns containing ink > thr.
func inkSpan(img *[RasterSize * RasterSize]float32, thr float32) (rows, cols int) {
	r0, r1, c0, c1 := RasterSize, -1, RasterSize, -1
	for r := 0; r < RasterSize; r++ {
		for c := 0; c < RasterSize; c++ {
			if img[r*RasterSize+c] > thr {
				r0, r1, c0, c1 = min(r0, r), max(r1, r), min(c0, c), max(c1, c)
			}
		}
	}
	return r1 - r0 + 1, c1 - c0 + 1
}

func TestRasterCentering(t *testing.T) {
	// A lopsided "7": most mass top-right, so bbox centring alone would not
	// put the centre of mass at the MNIST position.
	seven := []Stroke{densePoly([2]float32{500, 300}, [2]float32{560, 300}, [2]float32{520, 390})}
	img := Rasterize(seven)
	x, y := com(&img)
	if math.Abs(x-RasterCOM) > 0.1 || math.Abs(y-RasterCOM) > 0.1 {
		t.Errorf("centre of mass (%.3f, %.3f), want (%.1f, %.1f)", x, y, RasterCOM, RasterCOM)
	}
}

func TestRasterAspect(t *testing.T) {
	// 2:1 tall rectangle outline: the long side fills the 20 px box, the
	// short side keeps the aspect ratio.
	rect := []Stroke{densePoly([2]float32{0, 0}, [2]float32{40, 0}, [2]float32{40, 80}, [2]float32{0, 80}, [2]float32{0, 0})}
	img := Rasterize(rect)
	rows, cols := inkSpan(&img, 0.05)
	if rows < 19 || rows > 21 {
		t.Errorf("tall glyph spans %d rows, want ~20", rows)
	}
	foot := float64(2 * (RasterRadius + RasterSoft))
	got := (float64(cols) - foot) / (float64(rows) - foot)
	if math.Abs(got-0.5) > 0.1 {
		t.Errorf("centre-line aspect %.2f, want 0.5 (rows %d cols %d)", got, rows, cols)
	}
	// Wide glyph: the same in the other axis.
	wide := []Stroke{densePoly([2]float32{0, 0}, [2]float32{80, 0}, [2]float32{80, 40}, [2]float32{0, 40}, [2]float32{0, 0})}
	img = Rasterize(wide)
	rows, cols = inkSpan(&img, 0.05)
	if cols < 19 || cols > 21 || rows >= cols {
		t.Errorf("wide glyph spans %d rows x %d cols", rows, cols)
	}
	// A thin vertical "1" stays thin: no stretching to fill the box.
	one := []Stroke{densePoly([2]float32{100, 100}, [2]float32{103, 190})}
	img = Rasterize(one)
	rows, cols = inkSpan(&img, 0.05)
	if rows < 19 || cols > 5 {
		t.Errorf("'1' spans %d rows x %d cols, want tall and narrow", rows, cols)
	}
}

func TestRasterScaleTranslationInvariant(t *testing.T) {
	base := []Stroke{
		densePoly([2]float32{10, 10}, [2]float32{30, 5}, [2]float32{25, 40}),
		densePoly([2]float32{12, 25}, [2]float32{35, 26}),
	}
	moved := make([]Stroke, len(base))
	for i, s := range base {
		moved[i] = make(Stroke, len(s))
		for k, p := range s {
			moved[i][k] = Point{X: p.X*3 + 700, Y: p.Y*3 + 1200}
		}
	}
	a, b := Rasterize(base), Rasterize(moved)
	for i := range a {
		if d := math.Abs(float64(a[i] - b[i])); d > 1e-4 {
			t.Fatalf("pixel %d differs by %g after scale+translate", i, d)
		}
	}
}

func TestRasterDegenerate(t *testing.T) {
	if img := Rasterize(nil); img != ([RasterSize * RasterSize]float32{}) {
		t.Error("empty input should give a blank image")
	}
	dot := Rasterize([]Stroke{{{X: 5, Y: 5}}})
	x, y := com(&dot)
	if math.Abs(x-RasterCOM) > 0.05 || math.Abs(y-RasterCOM) > 0.05 {
		t.Errorf("dot centred at (%.2f, %.2f)", x, y)
	}
}
