// Command kakurogen generates graded Kakuro puzzles and prints them with
// timing and technique statistics per level, for tuning the engine.
package main

import (
	"flag"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"sudoku/kakuro"
)

func main() {
	levelFlag := flag.String("level", "all", "easy, medium, hard, expert or all")
	seed := flag.Uint64("seed", 1, "first seed; puzzle i uses seed+i")
	n := flag.Int("n", 5, "puzzles per level")
	quiet := flag.Bool("q", false, "print statistics only")
	solution := flag.Bool("solution", false, "print each puzzle's solution too")
	flag.Parse()

	var levels []kakuro.Level
	if strings.EqualFold(*levelFlag, "all") {
		levels = []kakuro.Level{kakuro.Easy, kakuro.Medium, kakuro.Hard, kakuro.Expert}
	} else {
		l, err := kakuro.ParseLevel(*levelFlag)
		if err != nil {
			fmt.Fprintln(os.Stderr, "kakurogen: -level must be easy, medium, hard, expert or all")
			os.Exit(2)
		}
		levels = []kakuro.Level{l}
	}
	if *n < 1 {
		fmt.Fprintln(os.Stderr, "kakurogen: -n must be at least 1")
		os.Exit(2)
	}
	for _, l := range levels {
		run(l, *seed, *n, *quiet, *solution)
	}
}

func run(level kakuro.Level, seed uint64, n int, quiet, solution bool) {
	var (
		times           []time.Duration
		whites, steps   []int
		hardest, usedIn [kakuro.NumTechniques]int
		failures        int
	)
	for i := 0; i < n; i++ {
		s := seed + uint64(i)
		t0 := time.Now()
		p := kakuro.Generate(s, level)
		dt := time.Since(t0)

		r := kakuro.Grade(p)
		w := 0
		for _, c := range p.Cells {
			if !c.Black {
				w++
			}
		}
		times = append(times, dt)
		whites = append(whites, w)
		steps = append(steps, r.Steps)
		hardest[r.Hardest]++
		for t, k := range r.Counts {
			if k > 0 {
				usedIn[t]++
			}
		}
		if msg := verify(p, r); msg != "" {
			failures++
			fmt.Printf("VERIFY FAILED seed=%d: %s\n", s, msg)
		}
		if !quiet {
			fmt.Printf("seed=%d level=%s n=%d white=%d hardest=%q steps=%d time=%s\n",
				s, level, p.N, w, p.Hardest, r.Steps, dt.Round(10*time.Microsecond))
			fmt.Println(kakuro.Format(p, nil))
			if solution {
				fmt.Println(kakuro.Format(p, p.Solution))
			}
		}
	}

	fmt.Printf("== %s (N=%d): %d puzzles, seeds %d..%d\n", level, level.Size(), n, seed, seed+uint64(n)-1)
	fmt.Printf("time      mean %-8s p50 %-8s p95 %-8s max %s\n",
		mean(times), pct(times, 50), pct(times, 95), slices.Max(times).Round(time.Microsecond))
	fmt.Printf("white     mean %.1f  min %d  max %d\n", avg(whites), slices.Min(whites), slices.Max(whites))
	fmt.Printf("steps     mean %.1f\n", avg(steps))
	fmt.Println("technique                    hardest   used in")
	for t := 0; t < kakuro.NumTechniques; t++ {
		if usedIn[t] == 0 {
			continue
		}
		fmt.Printf("  %-26s %7d   %7d\n", kakuro.Technique(t), hardest[t], usedIn[t])
	}
	fmt.Printf("verify failures: %d\n\n", failures)
}

// verify re-checks a puzzle independently of the generator's own gates.
func verify(p kakuro.Puzzle, r kakuro.Rating) string {
	if err := kakuro.Validate(p); err != nil {
		return err.Error()
	}
	n := p.N
	for i, c := range p.Cells {
		j := (n-i/n)*n + n - i%n // half-turn partner in the white area
		if i/n > 0 && i%n > 0 && c.Black != p.Cells[j].Black {
			return "layout not 180° symmetric"
		}
	}
	if kakuro.CountSolutions(p, 2) != 1 {
		return "not unique"
	}
	if sol, _ := kakuro.Solve(p); string(sol) != string(p.Solution) {
		return "solver disagrees with stored solution"
	}
	if !r.Solved || r.Level != p.Level || r.Hardest.String() != p.Hardest {
		return "regrade disagrees"
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
