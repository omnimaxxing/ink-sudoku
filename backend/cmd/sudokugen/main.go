// Command sudokugen generates graded puzzles and prints timing and
// technique statistics per level, for tuning the engine.
package main

import (
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"sudoku/engine"
)

func main() {
	levelFlag := flag.String("level", "all", "easy, medium, hard, expert or all")
	seed := flag.Uint64("seed", 1, "first seed; puzzle i uses seed+i")
	n := flag.Int("n", 5, "puzzles per level")
	quiet := flag.Bool("q", false, "print statistics only")
	flag.Parse()

	var levels []engine.Level
	if strings.EqualFold(*levelFlag, "all") {
		levels = []engine.Level{engine.Easy, engine.Medium, engine.Hard, engine.Expert}
	} else {
		l, err := engine.ParseLevel(*levelFlag)
		if err != nil || l > engine.Expert {
			fmt.Fprintln(os.Stderr, "sudokugen: -level must be easy, medium, hard, expert or all")
			os.Exit(2)
		}
		levels = []engine.Level{l}
	}

	for _, l := range levels {
		run(l, *seed, *n, *quiet)
	}
}

func run(level engine.Level, seed uint64, n int, quiet bool) {
	var (
		times            []time.Duration
		attempts, givens []int
		steps            int
		hardest, uses    [engine.NumTechniques]int
		narrowest        = map[int]int{}
		failures         int
	)
	for i := 0; i < n; i++ {
		s := seed + uint64(i)
		t0 := time.Now()
		p := engine.Generate(s, level)
		dt := time.Since(t0)

		times = append(times, dt)
		attempts = append(attempts, p.Attempts)
		givens = append(givens, p.Givens.Count())
		steps += p.Grade.Steps
		hardest[p.Grade.Hardest]++
		for _, t := range p.Techniques {
			uses[t]++
		}
		narrowest[min(p.Grade.Narrowest, 5)]++
		if msg := verify(p); msg != "" {
			failures++
			fmt.Printf("VERIFY FAILED seed=%d: %s\n", s, msg)
		}

		if !quiet {
			fmt.Printf("seed=%d level=%s givens=%d hardest=%q steps=%d attempts=%d time=%s\n",
				s, level, p.Givens.Count(), p.Grade.Hardest, p.Grade.Steps, p.Attempts, dt.Round(10*time.Microsecond))
			fmt.Println(p.Givens)
			fmt.Println(p.Givens.Pretty())
		}
	}

	fmt.Printf("== %s: %d puzzles, seeds %d..%d\n", level, n, seed, seed+uint64(n)-1)
	fmt.Printf("time      mean %-8s p50 %-8s p95 %-8s max %s\n",
		mean(times), pct(times, 50), pct(times, 95), slices.Max(times).Round(time.Microsecond))
	fmt.Printf("attempts  mean %.1f  max %d\n", avg(attempts), slices.Max(attempts))
	fmt.Printf("givens    mean %.1f  min %d  max %d\n", avg(givens), slices.Min(givens), slices.Max(givens))
	fmt.Printf("steps     mean %.1f\n", float64(steps)/float64(n))
	fmt.Printf("narrowest")
	for k := 0; k <= 5; k++ {
		if narrowest[k] > 0 {
			label := fmt.Sprint(k)
			if k == 5 {
				label = "5+"
			}
			fmt.Printf("  %s:%d", label, narrowest[k])
		}
	}
	fmt.Println()
	fmt.Println("technique                       hardest   used in")
	for t := 0; t < engine.NumTechniques; t++ {
		if uses[t] == 0 {
			continue
		}
		fmt.Printf("  %-29s %7d   %7d\n", engine.Technique(t), hardest[t], uses[t])
	}
	fmt.Printf("verify failures: %d\n\n", failures)
}

// verify re-checks a puzzle independently of the generator's own gates.
func verify(p engine.Puzzle) string {
	g := p.Givens
	for i := 0; i < 81; i++ {
		if (g[i] == 0) != (g[80-i] == 0) {
			return "not symmetric"
		}
		if g[i] != 0 && g[i] != p.Solution[i] {
			return "given disagrees with solution"
		}
	}
	if engine.CountSolutions(g, 2) != 1 {
		return "not unique"
	}
	if sol, _ := engine.Solve(g); sol != p.Solution {
		return "solver disagrees with stored solution"
	}
	if engine.Grade(g).Level != p.Level {
		return "regrade gives a different level"
	}
	return ""
}

func mean(ds []time.Duration) time.Duration {
	var sum time.Duration
	for _, d := range ds {
		sum += d
	}
	return (sum / time.Duration(len(ds))).Round(time.Microsecond)
}

func pct(ds []time.Duration, p int) time.Duration {
	s := slices.Clone(ds)
	slices.Sort(s)
	i := (len(s)*p + 99) / 100
	return s[max(i-1, 0)].Round(time.Microsecond)
}

func avg(xs []int) float64 {
	sum := 0
	for _, x := range xs {
		sum += x
	}
	return float64(sum) / float64(len(xs))
}
