package hwr

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
)

// TestEndToEnd runs Analyze on held-out real pen samples (pendigits test
// writers, UJI2 tst writers) written into a 140 px cell, at answer size and
// at candidate size, and reports grouping/kind/recognition rates and letter
// false accepts. Each writer's ink is scaled so their median digit is 80 px
// (answer) or 30 px (candidate); letters keep their size relative to the
// writer's digits. Report-only; runs with HWR_E2E=<e2e_strokes.jsonl>.
func TestEndToEnd(t *testing.T) {
	path := os.Getenv("HWR_E2E")
	if path == "" {
		t.Skip("set HWR_E2E to run")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	type rec struct {
		Src, Writer, Label string
		Strokes            [][][]float32
	}
	var recs []rec
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var r rec
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		recs = append(recs, r)
	}
	isDigit := func(l string) bool { return len(l) == 1 && l[0] >= '1' && l[0] <= '9' }
	heights := map[string][]float32{}
	toStrokes := func(r rec) []Stroke {
		var out []Stroke
		var tm int64
		for _, s := range r.Strokes {
			st := make(Stroke, len(s))
			for i, p := range s {
				st[i] = Point{X: p[0], Y: p[1], P: 0.5, T: tm}
				tm += 4
			}
			tm += 250
			out = append(out, st)
		}
		return out
	}
	for _, r := range recs {
		if isDigit(r.Label) {
			b, _ := Bounds(toStrokes(r)...)
			heights[r.Writer] = append(heights[r.Writer], b.H())
		}
	}
	median := map[string]float32{}
	for w, h := range heights {
		sort.Slice(h, func(i, j int) bool { return h[i] < h[j] })
		median[w] = h[len(h)/2]
	}
	m := DefaultModel()
	a := Analyzer{m, DefaultConfig()}
	cell := Rect{600, 800, 740, 940}
	for _, mode := range []struct {
		name   string
		size   float32
		cx, cy float32
		want   MarkKind
	}{{"answer", 80, 670, 870, Answer}, {"candidate", 30, 625, 825, Candidate}} {
		var nd, oneMark, kindOK, correct, misread int
		nl, fa := map[string]int{}, map[string]int{}
		leaks := map[string]int{}
		confusions := map[string]int{}
		for _, r := range recs {
			med, ok := median[r.Writer]
			if !ok || r.Label == "0" {
				continue
			}
			st := toStrokes(r)
			b, _ := Bounds(st...)
			s := mode.size / med
			bx, by := b.Center()
			for _, x := range st {
				for i := range x {
					x[i].X = (x[i].X-bx)*s + mode.cx
					x[i].Y = (x[i].Y-by)*s + mode.cy
				}
			}
			marks := a.Analyze(cell, st)
			if isDigit(r.Label) {
				nd++
				if len(marks) == 1 {
					oneMark++
					if marks[0].Kind == mode.want {
						kindOK++
					}
				}
				var big *Mark
				for i := range marks {
					if big == nil || marks[i].BBox.Area() > big.BBox.Area() {
						big = &marks[i]
					}
				}
				want := int(r.Label[0] - '0')
				if big != nil && big.Digit == want {
					correct++
				} else if big != nil && big.Digit != 0 {
					misread++
					confusions[fmt.Sprintf("%d->%d", want, big.Digit)]++
				}
				continue
			}
			if r.Src != "uji2" {
				continue
			}
			cat := category(r.Label)
			nl[cat]++
			for _, mk := range marks {
				if mk.Digit != 0 {
					fa[cat]++
					leaks[fmt.Sprintf("%s:%s->%d", cat[:3], r.Label, mk.Digit)]++
					break
				}
			}
		}
		t.Logf("%s size: digits n=%d  one-mark %.4f  kind-ok %.4f  correct %.4f  misread %.4f",
			mode.name, nd, frac(oneMark, nd), frac(kindOK, nd), frac(correct, nd), frac(misread, nd))
		for _, c := range []string{"letter", "lookalike", "symbol"} {
			t.Logf("  UJI %-9s n=%4d false-accept %.4f", c, nl[c], frac(fa[c], nl[c]))
		}
		t.Logf("  misreads %v", top(confusions, 8))
		t.Logf("  leaks %v", top(leaks, 16))
	}
}

// category splits UJI2 non-digits: lookalike = the classes excluded from
// reject training (build.LOOKALIKE), letter = other letters incl. accented,
// symbol = punctuation and other symbols.
func category(l string) string {
	const look = "IlSsZzGgqbB'\",()!¡$|"
	for _, c := range look {
		if l == string(c) {
			return "lookalike"
		}
	}
	r := []rune(l)
	if len(r) == 1 && (r[0] >= 'a' && r[0] <= 'z' || r[0] >= 'A' && r[0] <= 'Z' || strings.ContainsRune("ñÑáéíóúÁÉÍÓÚüÜ", r[0])) {
		return "letter"
	}
	return "symbol"
}

func frac(a, b int) float64 { return float64(a) / float64(max(b, 1)) }

func top(m map[string]int, n int) []string {
	type kv struct {
		k string
		v int
	}
	var s []kv
	for k, v := range m {
		s = append(s, kv{k, v})
	}
	sort.Slice(s, func(i, j int) bool { return s[i].v > s[j].v || (s[i].v == s[j].v && s[i].k < s[j].k) })
	var out []string
	for i := 0; i < len(s) && i < n; i++ {
		out = append(out, fmt.Sprintf("%s:%d", s[i].k, s[i].v))
	}
	return out
}
