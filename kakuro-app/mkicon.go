//go:build ignore

// Command mkicon draws Ink Kakuro's icon: the corner of a Kakuro grid (gray clue cells
// split by a diagonal, sums in the triangles) with a handwritten answer in one white cell.
// The sums are consistent: answers 1 2 / 3 1 make across 3, 4 and down 4, 3.
// Run from this folder: pixi run go run mkicon.go icon.png
package main

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
)

const (
	n      = 128
	lo, hi = 10, 118
	cell   = (hi - lo) / 3
)

type pt = [2]float64

func main() {
	img := image.NewGray(image.Rect(0, 0, n, n))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	// Clue cells light gray, the empty corner darker.
	fill(img, 0, 0, 178)
	for _, rc := range [][2]int{{0, 1}, {0, 2}, {1, 0}, {2, 0}} {
		fill(img, rc[0], rc[1], 214)
	}
	// Lines: heavy outside, thin inside.
	for k := 0; k <= 3; k++ {
		p := lo + k*cell
		w := 2
		if k == 0 || k == 3 {
			w = 5
		}
		for t := -w / 2; t < w-w/2; t++ {
			for i := lo - 2; i < hi+3; i++ {
				img.SetGray(p+t, i, color.Gray{0})
				img.SetGray(i, p+t, color.Gray{0})
			}
		}
	}
	// Diagonals in the clue cells.
	for _, rc := range [][2]int{{0, 1}, {0, 2}, {1, 0}, {2, 0}} {
		x, y := float64(lo+rc[1]*cell), float64(lo+rc[0]*cell)
		ink(img, []pt{{x, y}, {x + cell, y + cell}}, 0.9)
	}
	// Sums: down in the lower-left triangle, across in the upper-right.
	at := func(r, c int, fx, fy float64) (float64, float64) {
		return float64(lo+c*cell) + fx*cell, float64(lo+r*cell) + fy*cell
	}
	x, y := at(0, 1, 0.3, 0.7)
	digit(img, 4, x, y, 12, 1.25)
	x, y = at(0, 2, 0.3, 0.7)
	digit(img, 3, x, y, 12, 1.25)
	x, y = at(1, 0, 0.7, 0.3)
	digit(img, 3, x, y, 12, 1.25)
	x, y = at(2, 0, 0.7, 0.3)
	digit(img, 4, x, y, 12, 1.25)
	// The handwritten answer: a 3, slanted, in the bottom-left white cell.
	x, y = at(2, 1, 0.5, 0.52)
	digit(img, 3, x, y, 25, 2.1)

	f, err := os.Create(os.Args[1])
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		panic(err)
	}
}

func fill(img *image.Gray, r, c int, v uint8) {
	for y := lo + r*cell; y < lo+(r+1)*cell; y++ {
		for x := lo + c*cell; x < lo+(c+1)*cell; x++ {
			img.SetGray(x, y, color.Gray{v})
		}
	}
}

// Single-stroke digits in a unit box (x 0–0.75, y 0–1), as in the dev stub's handwriting font.
var font = map[int][][]pt{
	3: {{{0.06, 0.12}, {0.34, 0.0}, {0.6, 0.1}, {0.58, 0.3}, {0.3, 0.46}},
		{{0.3, 0.46}, {0.62, 0.55}, {0.68, 0.8}, {0.45, 0.98}, {0.15, 0.97}, {0.0, 0.84}}},
	4: {{{0.5, 0.0}, {0.03, 0.66}, {0.76, 0.63}}, {{0.56, 0.28}, {0.54, 0.62}, {0.5, 1.02}}},
}

// digit draws d with glyph height h centred on (cx, cy), slanted a little.
func digit(img *image.Gray, d int, cx, cy, h, r float64) {
	for i, ctl := range font[d] {
		var pts []pt
		if d == 4 && i == 0 {
			pts = ctl
		} else {
			pts = curve(ctl)
		}
		out := make([]pt, len(pts))
		for k, p := range pts {
			x, y := (p[0]-0.36)*h, (p[1]-0.5)*h
			out[k] = pt{cx + x - 0.12*y, cy + y}
		}
		ink(img, out, r)
	}
}

// curve samples a Catmull-Rom spline through the points.
func curve(p []pt) []pt {
	var out []pt
	for i := 0; i+1 < len(p); i++ {
		p0, p1, p2, p3 := p[max(0, i-1)], p[i], p[i+1], p[min(len(p)-1, i+2)]
		for k := 0; k <= 12; k++ {
			if i > 0 && k == 0 {
				continue
			}
			t := float64(k) / 12
			t2, t3 := t*t, t*t*t
			var q pt
			for j := 0; j < 2; j++ {
				q[j] = 0.5 * (2*p1[j] + (-p0[j]+p2[j])*t + (2*p0[j]-5*p1[j]+4*p2[j]-p3[j])*t2 +
					(-p0[j]+3*p1[j]-3*p2[j]+p3[j])*t3)
			}
			out = append(out, q)
		}
	}
	return out
}

// ink darkens every pixel within radius r of the polyline, with a 1 px soft edge.
func ink(img *image.Gray, pts []pt, r float64) {
	for y := 0; y < n; y++ {
		for x := 0; x < n; x++ {
			px, py := float64(x)+0.5, float64(y)+0.5
			d := math.Inf(1)
			for i := 0; i+1 < len(pts); i++ {
				d = math.Min(d, segDist(px, py, pts[i], pts[i+1]))
			}
			cover := math.Max(0, math.Min(1, r+0.5-d))
			if cover > 0 {
				v := float64(img.GrayAt(x, y).Y) * (1 - cover)
				img.SetGray(x, y, color.Gray{uint8(v)})
			}
		}
	}
}

func segDist(px, py float64, a, b pt) float64 {
	dx, dy := b[0]-a[0], b[1]-a[1]
	l := dx*dx + dy*dy
	if l == 0 {
		return math.Hypot(px-a[0], py-a[1])
	}
	t := math.Max(0, math.Min(1, ((px-a[0])*dx+(py-a[1])*dy)/l))
	return math.Hypot(px-a[0]-t*dx, py-a[1]-t*dy)
}
