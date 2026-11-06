package game

import (
	"slices"
	"testing"
	"time"

	"sudoku/engine"
)

func TestDaily(t *testing.T) {
	mon := time.Date(2026, 10, 26, 0, 0, 1, 0, time.UTC)
	want := []engine.Level{engine.Easy, engine.Easy, engine.Medium, engine.Medium, engine.Hard, engine.Hard, engine.Expert}
	seeds := map[uint64]string{}
	for i := 0; i < 400; i++ {
		day := mon.AddDate(0, 0, i)
		date, seed, level := Daily(day)
		if d2, s2, l2 := Daily(day.Add(23*time.Hour + 59*time.Minute)); d2 != date || s2 != seed || l2 != level {
			t.Fatalf("%s: the daily changes during the day", date)
		}
		if prev, dup := seeds[seed]; dup {
			t.Fatalf("%s and %s share seed %d", prev, date, seed)
		}
		seeds[seed] = date
		if i < 7 && level != want[i] {
			t.Errorf("%s (%v): level %v, want %v", date, day.Weekday(), level, want[i])
		}
	}
	if date, seed, _ := Daily(mon.AddDate(0, 0, 3)); date != "2026-10-29" || seed != 20261029 {
		t.Errorf("Thursday: %s seed %d", date, seed)
	}
}

func TestStats(t *testing.T) {
	var s Stats
	unsolved := func(l engine.Level) *Game { return &Game{level: l, solution: engine.Grid{1}} }
	solved := func(l engine.Level, d time.Duration, daily string) *Game { // a zero board counts as solved
		return &Game{level: l, elapsed: d, daily: daily}
	}

	s.Start(nil, &Game{level: engine.Hard})
	g := solved(engine.Hard, 5*time.Minute, "")
	if best, streak := s.Solve(g, t0); !best || streak != 1 || s.Hard != (LevelStats{Played: 1, Solved: 1, BestMs: 300_000, Streak: 1}) {
		t.Fatalf("first solve: best %v streak %d, %+v", best, streak, s.Hard)
	}
	if best, _ := s.Solve(g, t0); best || s.Hard.Solved != 1 {
		t.Fatal("re-solving the same game counted again")
	}
	s.Start(g, &Game{level: engine.Hard})
	if best, streak := s.Solve(solved(engine.Hard, 6*time.Minute, ""), t0); best || streak != 2 || s.Hard.BestMs != 300_000 {
		t.Fatalf("slower solve: best %v streak %d, %+v", best, streak, s.Hard)
	}
	s.Start(nil, &Game{level: engine.Hard})
	if best, _ := s.Solve(solved(engine.Hard, 4*time.Minute, ""), t0); !best || s.Hard.BestMs != 240_000 {
		t.Fatalf("faster solve: best %v, %+v", best, s.Hard)
	}
	s.Start(unsolved(engine.Easy), &Game{level: engine.Hard}) // abandoning an easy game leaves the hard streak alone
	if s.Hard.Streak != 3 || s.Hard.Played != 4 {
		t.Fatalf("%+v", s.Hard)
	}
	s.Start(unsolved(engine.Hard), &Game{level: engine.Easy})
	if s.Hard.Streak != 0 || s.Easy.Played != 1 {
		t.Fatalf("abandoned hard game: hard %+v easy %+v", s.Hard, s.Easy)
	}

	// Daily: consecutive dates extend the streak; a gap restarts it.
	for i, c := range []struct {
		date   string
		streak int
	}{{"2026-10-28", 1}, {"2026-10-29", 2}, {"2026-10-29", 2}, {"2026-11-02", 1}} {
		if _, streak := s.Solve(solved(engine.Medium, time.Minute, c.date), t0); streak != c.streak || s.Daily.Last != c.date {
			t.Fatalf("daily %d (%s): streak %d last %q, want %d", i, c.date, streak, s.Daily.Last, c.streak)
		}
	}
	day := func(d int) time.Time { return time.Date(2026, 11, d, 22, 0, 0, 0, time.UTC) }
	if s.View(day(3)).Daily.Streak != 1 || s.View(day(4)).Daily.Streak != 0 {
		t.Fatal("a daily streak should last until the end of the next day")
	}

	st := Store{Dir: t.TempDir()}
	if err := st.SaveStats(s); err != nil {
		t.Fatal(err)
	}
	if got, err := st.LoadStats(); err != nil || got != s {
		t.Fatalf("stats round trip: %v\n got %+v\nwant %+v", err, got, s)
	}
}

func TestCalibrationOrder(t *testing.T) {
	var c Calibration
	var got []int
	for c.Next() != 0 {
		got = append(got, c.Next())
		c.Advance()
	}
	if len(got) != 27 {
		t.Fatalf("%d prompts", len(got))
	}
	for r := 0; r < 3; r++ {
		round := slices.Clone(got[9*r : 9*r+9])
		if r == 0 && !slices.IsSorted(round) || r > 0 && slices.IsSorted(round) {
			t.Errorf("round %d: %v", r+1, round)
		}
		slices.Sort(round)
		if !slices.Equal(round, []int{1, 2, 3, 4, 5, 6, 7, 8, 9}) {
			t.Errorf("round %d is not 1-9: %v", r+1, got[9*r:9*r+9])
		}
	}
	for i := 1; i < len(got); i++ {
		if got[i] == got[i-1] {
			t.Errorf("%d prompted twice in a row", got[i])
		}
	}
}

func TestStatsSkipGamesByNumber(t *testing.T) {
	var s Stats
	s.Start(nil, &Game{level: engine.Hard})
	s.Solve(&Game{level: engine.Hard, elapsed: 5 * time.Minute}, t0)
	before := s

	byNumber := &Game{level: engine.Hard, elapsed: time.Minute, byNumber: true}
	s.Start(nil, byNumber)
	if best, streak := s.Solve(byNumber, t0); best || streak != 0 || s != before || !byNumber.recorded {
		t.Fatalf("a replayed puzzle changed the stats: best %v streak %d, %+v", best, streak, s.Hard)
	}
	s.Start(&Game{level: engine.Hard, solution: engine.Grid{1}, byNumber: true}, &Game{level: engine.Easy, byNumber: true})
	if s.Hard.Streak != 1 || s.Easy.Played != 0 {
		t.Fatalf("leaving a replayed puzzle unsolved broke the streak: %+v", s)
	}
}
