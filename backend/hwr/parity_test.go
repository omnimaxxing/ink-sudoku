package hwr

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"io"
	"math"
	"os"
	"testing"
)

type paritySample struct {
	label   int
	strokes []Stroke // nil for bitmap samples
	bitmap  []byte
}

func readParity(t testing.TB) (samples []paritySample, logits [][]float32, rasters [][]float32) {
	f, err := os.Open("testdata/parity.bin.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	r := bytes.NewReader(raw)
	rd := func(v any) {
		if err := binary.Read(r, binary.LittleEndian, v); err != nil {
			t.Fatal(err)
		}
	}
	var magic [4]byte
	var hdr [5]uint32
	rd(&magic)
	rd(&hdr)
	if string(magic[:]) != "HWRP" || hdr[0] != 1 {
		t.Fatalf("bad parity header %q %v", magic, hdr)
	}
	nStroke, nBitmap, nRaster, nClass := int(hdr[1]), int(hdr[2]), int(hdr[3]), int(hdr[4])
	for i := 0; i < nStroke; i++ {
		var lab uint8
		var ns uint16
		rd(&lab)
		rd(&ns)
		s := paritySample{label: int(lab)}
		for j := 0; j < int(ns); j++ {
			var np uint16
			rd(&np)
			xy := make([]float32, 2*int(np))
			rd(xy)
			st := make(Stroke, np)
			for k := range st {
				st[k] = Point{X: xy[2*k], Y: xy[2*k+1], P: 0.5, T: int64(k * 4)}
			}
			s.strokes = append(s.strokes, st)
		}
		samples = append(samples, s)
	}
	for i := 0; i < nBitmap; i++ {
		var lab uint8
		rd(&lab)
		bm := make([]byte, RasterSize*RasterSize)
		rd(bm)
		samples = append(samples, paritySample{label: int(lab), bitmap: bm})
	}
	for range samples {
		l := make([]float32, nClass)
		rd(l)
		logits = append(logits, l)
	}
	for i := 0; i < nRaster; i++ {
		rr := make([]float32, RasterSize*RasterSize)
		rd(rr)
		rasters = append(rasters, rr)
	}
	return
}

func argmax(v []float32) int {
	b := 0
	for i := range v {
		if v[i] > v[b] {
			b = i
		}
	}
	return b
}

// TestParity: Go rasteriser + Go inference vs the Python rasteriser +
// PyTorch on the dequantised model, on a held-out exported file.
func TestParity(t *testing.T) {
	samples, ref, rasters := readParity(t)
	m := DefaultModel()
	var maxRaster, maxLogit float64
	agree := 0
	for i, s := range samples {
		var img [RasterSize * RasterSize]float32
		if s.strokes != nil {
			img = Rasterize(s.strokes)
			if i < len(rasters) {
				for k, v := range rasters[i] {
					maxRaster = math.Max(maxRaster, math.Abs(float64(v-img[k])))
				}
			}
		} else {
			for k, b := range s.bitmap {
				img[k] = float32(b) / 255
			}
		}
		got := m.Logits(&img)
		for k := range got {
			maxLogit = math.Max(maxLogit, math.Abs(float64(got[k]-ref[i][k])))
		}
		if argmax(got) == argmax(ref[i]) {
			agree++
		}
	}
	rate := float64(agree) / float64(len(samples))
	t.Logf("parity: %d samples, top-1 agreement %.4f, max |raster diff| %.2g (%d rasters), max |logit diff| %.2g",
		len(samples), rate, maxRaster, len(rasters), maxLogit)
	if rate < 0.999 {
		t.Errorf("top-1 agreement %.4f < 0.999", rate)
	}
	if maxRaster > 1e-3 {
		t.Errorf("raster drift %.3g: raster.go and raster.py disagree", maxRaster)
	}
}
