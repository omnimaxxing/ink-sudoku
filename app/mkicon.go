//go:build ignore

// Command mkicon draws Ink Sudoku's icon: a 3×3 box grid with a handwritten 7.
// Run from this folder: pixi run go run mkicon.go icon.png
package main

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
)

const n = 128

func main() {
	img := image.NewGray(image.Rect(0, 0, n, n))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	// Box lines: heavy outside, lighter inside.
	const lo, hi = 10, 118
	for k := 0; k <= 3; k++ {
		p := lo + k*(hi-lo)/3
		w := 3
		if k == 0 || k == 3 {
			w = 6
		}
		for t := -w / 2; t < w-w/2; t++ {
			for i := lo - 3; i < hi+3; i++ {
				img.SetGray(p+t, i, color.Gray{0})
				img.SetGray(i, p+t, color.Gray{0})
			}
		}
	}
	// A pen-drawn 7 across the middle box, anti-aliased.
	seven := [][2]float64{{47, 52}, {63, 50.5}, {80, 48.5}, {74, 57}, {68, 67}, {63, 77}, {59, 88}}
	ink(img, seven, 3.2)

	f, err := os.Create(os.Args[1])
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		panic(err)
	}
}

// ink darkens every pixel within radius r of the polyline, with a 1 px soft edge.
func ink(img *image.Gray, pts [][2]float64, r float64) {
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

func segDist(px, py float64, a, b [2]float64) float64 {
	dx, dy := b[0]-a[0], b[1]-a[1]
	t := ((px-a[0])*dx + (py-a[1])*dy) / (dx*dx + dy*dy)
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(px-a[0]-t*dx, py-a[1]-t*dy)
}
