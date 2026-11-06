package hwr

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"runtime"
	"sort"
	"sync"
	"testing"
)

type simSample struct {
	split, src, writer string
	digit              int  // 1..9, else 0
	neg, look          bool // non-digit: other / lookalike (category())
	prob, emb          []float32
}

// loadSim reads personal_strokes.jsonl (hwrtrain calib.py) and runs the
// model once per sample.
func loadSim(t *testing.T, path string) []simSample {
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []simSample
	var ink [][]Stroke
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var r struct {
			Split, Src, Writer, Label string
			Strokes                   [][][]float32
		}
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			t.Fatal(err)
		}
		var st []Stroke
		for _, s := range r.Strokes {
			x := make(Stroke, len(s))
			for i, p := range s {
				x[i] = Point{X: p[0], Y: p[1], P: 0.5}
			}
			st = append(st, x)
		}
		s := simSample{split: r.Split, src: r.Src, writer: r.Writer}
		switch {
		case len(r.Label) == 1 && r.Label[0] >= '1' && r.Label[0] <= '9':
			s.digit = int(r.Label[0] - '0')
		case r.Label == "0":
		case category(r.Label) == "lookalike":
			s.look = true
		default:
			s.neg = true
		}
		out = append(out, s)
		ink = append(ink, st)
	}
	m := DefaultModel()
	var wg sync.WaitGroup
	ch := make(chan int)
	for w := 0; w < runtime.NumCPU(); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range ch {
				img := Rasterize(ink[i])
				e := make([]float32, m.EmbedDim())
				out[i].prob = softmax(m.forward(&img, e))
				normalize(e)
				out[i].emb = e
			}
		}()
	}
	for i := range out {
		ch <- i
	}
	close(ch)
	wg.Wait()
	return out
}

type simCount struct{ n, ok, unk, mis, nneg, fa, nlook, falook int }

func (c *simCount) tally(s simSample, got int) {
	switch {
	case s.digit != 0:
		c.n++
		if got == s.digit {
			c.ok++
		} else if got == 0 {
			c.unk++
		} else {
			c.mis++
		}
	case s.neg:
		c.nneg++
		if got != 0 {
			c.fa++
		}
	case s.look:
		c.nlook++
		if got != 0 {
			c.falook++
		}
	}
}

func (c simCount) String() string {
	s := fmt.Sprintf("acc %.4f unk %.4f mis %.4f", frac(c.ok, c.n), frac(c.unk, c.n), frac(c.mis, c.n))
	if c.nneg > 0 {
		s += fmt.Sprintf("  FAR %.4f lookalike %.3f", frac(c.fa, c.nneg), frac(c.falook, c.nlook))
	}
	return s
}

// TestPersonalSimulation measures Personal on writers the model never saw.
// Each writer plays "the player": N of their digits (N/9 per digit, like a
// calibration page) back a Personal, and their other digits are scored;
// false accepts are counted on non-digit UJI ink (the writer's own for UJI
// writers, the split's whole UJI pool for pendigits writers, who wrote
// only digits). "N=1 (a 7)" is a player who skipped calibration and
// corrected one 7. "+corr" starts from 27 samples and streams the writer's
// remaining digits in random order, adding every misread or reject as a
// correction, as the game does. Report-only; needs
// HWR_PERSONAL=<hwrtrain/data/cache/personal_strokes.jsonl> (calib.py).
// HWR_SPLIT=dev|test (default test; tune on dev), HWR_TUNE=floor,bonus,thr.
func TestPersonalSimulation(t *testing.T) {
	path := os.Getenv("HWR_PERSONAL")
	if path == "" {
		t.Skip("set HWR_PERSONAL to run")
	}
	split := os.Getenv("HWR_SPLIT")
	if split == "" {
		split = "test"
	}
	tune := personalTuning{PersonalSimFloor, PersonalBonus, PersonalThreshold}
	if s := os.Getenv("HWR_TUNE"); s != "" {
		if _, err := fmt.Sscanf(s, "%g,%g,%g", &tune.floor, &tune.bonus, &tune.threshold); err != nil {
			t.Fatal(err)
		}
	}
	all := loadSim(t, path)
	m := DefaultModel()
	byW := map[string][]int{}
	var pool []int
	for i, s := range all {
		if s.split != split {
			continue
		}
		byW[s.writer] = append(byW[s.writer], i)
		if s.src == "uji2" && s.digit == 0 {
			pool = append(pool, i)
		}
	}
	var writers []string
	for w := range byW {
		writers = append(writers, w)
	}
	sort.Strings(writers)
	t.Logf("split %s, %d writers, tuning %+v", split, len(writers), tune)
	for _, src := range []string{"pendigits", "uji2"} {
		for _, row := range []struct {
			name   string
			k      int  // samples per digit
			only   int  // if set, only this digit gets samples
			stream bool // add every misread/reject as a correction
		}{{"N=1 (a 7)", 1, 7, false}, {"N=9", 1, 0, false}, {"N=27", 3, 0, false}, {"N=54", 6, 0, false}, {"N=27+corr", 3, 0, true}} {
			var base, pers simCount
			worstB, worstP := 1.0, 1.0
			var nw, goodB, goodP int
			for _, w := range writers {
				idx := byW[w]
				if all[idx[0]].src != src {
					continue
				}
				perDigit := map[int][]int{}
				negs := pool
				if src == "uji2" {
					negs = nil
				}
				for _, i := range idx {
					if d := all[i].digit; d != 0 {
						perDigit[d] = append(perDigit[d], i)
					} else if src == "uji2" {
						negs = append(negs, i)
					}
				}
				k := row.k
				if len(perDigit[5]) < k+1 {
					continue
				}
				seeds := 5
				if src == "uji2" {
					seeds = 3
				}
				var wb, wp simCount
				for seed := 0; seed < seeds; seed++ {
					rng := rand.New(rand.NewSource(int64(seed)*7919 + int64(len(w))))
					p := NewPersonal(m, nil)
					p.tune = tune
					var rest []int
					for d := 1; d <= 9; d++ {
						c := append([]int(nil), perDigit[d]...)
						rng.Shuffle(len(c), func(a, b int) { c[a], c[b] = c[b], c[a] })
						kk := k
						if row.only != 0 && d != row.only {
							kk = 0
						}
						for _, i := range c[:kk] {
							p.add(all[i].emb, d)
						}
						rest = append(rest, c[kk:]...)
					}
					rng.Shuffle(len(rest), func(a, b int) { rest[a], rest[b] = rest[b], rest[a] })
					read := func(i int) int {
						ev := p.similarity(all[i].emb, -1)
						return p.decide(all[i].prob, &ev, 0).Digit
					}
					for _, i := range rest {
						s := all[i]
						wb.tally(s, m.decide(s.prob, 0).Digit)
						got := read(i)
						wp.tally(s, got)
						if row.stream && got != s.digit {
							p.add(s.emb, s.digit)
						}
					}
					if row.stream {
						continue // false accepts are measured with the fixed sets
					}
					for _, i := range negs {
						wb.tally(all[i], m.decide(all[i].prob, 0).Digit)
						wp.tally(all[i], read(i))
					}
				}
				worstB = min(worstB, frac(wb.ok, wb.n))
				worstP = min(worstP, frac(wp.ok, wp.n))
				nw++
				if frac(wb.ok, wb.n) >= 0.995 {
					goodB++
				}
				if frac(wp.ok, wp.n) >= 0.995 {
					goodP++
				}
				for _, x := range []struct{ to, from *simCount }{{&base, &wb}, {&pers, &wp}} {
					x.to.n += x.from.n
					x.to.ok += x.from.ok
					x.to.unk += x.from.unk
					x.to.mis += x.from.mis
					x.to.nneg += x.from.nneg
					x.to.fa += x.from.fa
					x.to.nlook += x.from.nlook
					x.to.falook += x.from.falook
				}
			}
			if base.n == 0 {
				continue
			}
			name := row.name
			t.Logf("%-9s %-11s base     %v  worst writer %.3f, %d/%d writers >= 99.5%%", src, name, base, worstB, goodB, nw)
			t.Logf("%-9s %-11s personal %v  worst writer %.3f, %d/%d writers >= 99.5%%", src, name, pers, worstP, goodP, nw)
		}
	}
}
