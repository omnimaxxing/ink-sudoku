package hwr

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"testing"
)

// TestScratchCalibration prints scratch-out feature distributions over real
// pen digits/letters and synthetic zig-zags. Only runs with
// HWR_CALIB=<hwrtrain/data/cache/calib_strokes.jsonl>.
func TestScratchCalibration(t *testing.T) {
	path := os.Getenv("HWR_CALIB")
	if path == "" {
		t.Skip("set HWR_CALIB to run")
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cfg := DefaultConfig()
	hit := map[string]int{}
	hist := map[string][][6]int{}
	tot := map[string]int{}
	var worst []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var r struct {
			Src     string        `json:"src"`
			Label   string        `json:"label"`
			Strokes [][][]float32 `json:"strokes"`
		}
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		cls := "letter/symbol"
		if r.Label == "#" {
			cls = "scratch"
		} else if r.Label >= "0" && r.Label <= "9" && len(r.Label) == 1 {
			cls = "digit"
		}
		tot[cls]++
		any := false
		br, bq := 0, float32(0)
		for _, s := range r.Strokes {
			st := make(Stroke, len(s))
			for i, p := range s {
				st[i] = Point{X: p[0], Y: p[1]}
			}
			rev, ratio, _ := ScratchFeatures(st, cfg.ScratchReversalDeg)
			if rev > br || (rev == br && ratio > bq) {
				br, bq = rev, ratio
			}
			if rev >= cfg.ScratchMinReversals && ratio >= cfg.ScratchMinLengthRatio {
				any = true
				if cls != "scratch" {
					worst = append(worst, fmt.Sprintf("%s %q rev=%d ratio=%.2f", r.Src, r.Label, rev, ratio))
				}
			}
		}
		if any {
			hit[cls]++
		}
		if hist[cls] == nil {
			hist[cls] = make([][6]int, 9)
		}
		col := 0
		for _, edge := range []float32{2, 2.5, 3, 3.5, 4} {
			if bq >= edge {
				col++
			}
		}
		hist[cls][min(br, 8)][col]++
	}
	t.Log("2D histogram (max over strokes): rows=reversals 0..8+, cols=ratio <2,2-2.5,2.5-3,3-3.5,3.5-4,>=4")
	for _, c := range []string{"digit", "scratch"} {
		for r := 0; r <= 8; r++ {
			t.Logf("%-8s rev=%d %v", c, r, hist[c][r])
		}
	}
	for _, c := range []string{"digit", "letter/symbol", "scratch"} {
		t.Logf("%-14s flagged as scratch: %d / %d", c, hit[c], tot[c])
	}
	sort.Strings(worst)
	for i, w := range worst {
		if i < 40 {
			t.Log(w)
		}
	}
}
