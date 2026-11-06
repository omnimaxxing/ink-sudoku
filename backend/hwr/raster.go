package hwr

import "math"

// Rasterizer constants. They are mirrored in hwrtrain/src/hwrtrain/raster.py
// and must change together (the parity test catches drift).
const (
	RasterSize = 28   // output is RasterSize x RasterSize, row-major
	RasterBox  = 20.0 // the inked glyph is fitted into a RasterBox square
	// RasterRadius is the stroke half-width in output pixels. Being fixed in
	// output space makes stroke width scale with the glyph. 1.1 matches the
	// median MNIST stroke width.
	RasterRadius = 1.1
	// RasterSoft is the half-width of the anti-aliased edge ramp.
	RasterSoft = 0.5
	// RasterCOM is where the intensity centre of mass is placed
	// (continuous coords, pixel (r,c) centre at (c+.5, r+.5)); 14.5 is the
	// MNIST mean, i.e. pixel index 14.
	RasterCOM = 14.5
)

// Rasterize renders strokes into a 28x28 MNIST-style image (row-major,
// 0 = paper, 1 = ink):
//
//  1. scale uniformly so the inked bbox (centre-line bbox + stroke
//     footprint) fits 20x20, preserving aspect;
//  2. draw every segment as an anti-aliased capsule of fixed output radius
//     (ink combines with max);
//  3. translate (sub-pixel, by re-rendering) so the centre of mass lands at
//     the MNIST position.
//
// Timestamps and pressure are ignored. Empty input yields a blank image.
func Rasterize(strokes []Stroke) [RasterSize * RasterSize]float32 {
	return rasterize(strokes, RasterRadius, RasterSoft)
}

type seg struct{ x0, y0, x1, y1 float32 }

func rasterize(strokes []Stroke, radius, soft float32) (img [RasterSize * RasterSize]float32) {
	b, ok := Bounds(strokes...)
	if !ok {
		return img
	}
	n := 0
	for _, s := range strokes {
		if len(s) == 1 {
			n++
		} else if len(s) > 1 {
			n += len(s) - 1
		}
	}
	segs := make([]seg, 0, n)
	w, h := b.X1-b.X0, b.Y1-b.Y0
	ext := max(w, h)
	var scale float32
	if ext > 1e-6 {
		scale = float32(RasterBox-2*(radius+soft)) / ext
	}
	half := float32(RasterSize / 2)
	ox := half - w*scale/2
	oy := half - h*scale/2
	tx := func(p Point) (float32, float32) {
		return (p.X-b.X0)*scale + ox, (p.Y-b.Y0)*scale + oy
	}
	for _, s := range strokes {
		switch {
		case len(s) == 1:
			x, y := tx(s[0])
			segs = append(segs, seg{x, y, x, y})
		case len(s) > 1:
			px, py := tx(s[0])
			for _, p := range s[1:] {
				x, y := tx(p)
				segs = append(segs, seg{px, py, x, y})
				px, py = x, y
			}
		}
	}
	render(&img, segs, radius, soft)
	var tot, sx, sy float32
	for r := 0; r < RasterSize; r++ {
		for c := 0; c < RasterSize; c++ {
			v := img[r*RasterSize+c]
			tot += v
			sx += v * (float32(c) + 0.5)
			sy += v * (float32(r) + 0.5)
		}
	}
	if tot <= 0 {
		return img
	}
	dx := RasterCOM - sx/tot
	dy := RasterCOM - sy/tot
	for i := range segs {
		segs[i].x0 += dx
		segs[i].x1 += dx
		segs[i].y0 += dy
		segs[i].y1 += dy
	}
	img = [RasterSize * RasterSize]float32{}
	render(&img, segs, radius, soft)
	return img
}

// render draws capsules with coverage clamp((r+soft-d)/(2*soft), 0, 1).
// Only pixels within r+soft of a segment can be non-zero, so each segment
// visits just its expanded bbox.
func render(img *[RasterSize * RasterSize]float32, segs []seg, radius, soft float32) {
	reach := radius + soft
	ramp := 2 * soft
	for _, s := range segs {
		dx, dy := s.x1-s.x0, s.y1-s.y0
		ll := dx*dx + dy*dy
		c0 := max(0, int(math.Floor(float64(min(s.x0, s.x1)-reach-1))))
		c1 := min(RasterSize-1, int(math.Ceil(float64(max(s.x0, s.x1)+reach+1))))
		r0 := max(0, int(math.Floor(float64(min(s.y0, s.y1)-reach-1))))
		r1 := min(RasterSize-1, int(math.Ceil(float64(max(s.y0, s.y1)+reach+1))))
		for r := r0; r <= r1; r++ {
			py := float32(r) + 0.5 - s.y0
			row := img[r*RasterSize : (r+1)*RasterSize]
			for c := c0; c <= c1; c++ {
				px := float32(c) + 0.5 - s.x0
				var t float32
				if ll > 0 {
					t = (px*dx + py*dy) / ll
					t = min(max(t, 0), 1)
				}
				ex, ey := px-t*dx, py-t*dy
				d := sqrt32(ex*ex + ey*ey)
				v := (reach - d) / ramp
				if v > row[c] {
					row[c] = min(v, 1)
				}
			}
		}
	}
}

func sqrt32(x float32) float32 { return float32(math.Sqrt(float64(x))) }
