package game

import (
	"time"

	"sudoku/engine"
)

// LevelStats are the player's numbers at one level.
type LevelStats struct {
	Played int   `json:"played"` // games started
	Solved int   `json:"solved"`
	BestMs int64 `json:"bestMs"` // fastest solve; 0 before the first
	// Streak counts solved games in a row; starting a new game while one is
	// unsolved resets it.
	Streak int `json:"streak"`
}

// DailyStats track the daily puzzle.
type DailyStats struct {
	Streak int    `json:"streak"` // consecutive dates solved
	Last   string `json:"last"`   // date of the last daily solved, "" if none
}

// Stats are the player's records across games, in the protocol's
// state.stats shape (and stats.json's).
type Stats struct {
	Easy   LevelStats `json:"easy"`
	Medium LevelStats `json:"medium"`
	Hard   LevelStats `json:"hard"`
	Expert LevelStats `json:"expert"`
	Daily  DailyStats `json:"daily"`
}

// Level returns the stats for l.
func (s *Stats) Level(l engine.Level) *LevelStats {
	switch l {
	case engine.Easy:
		return &s.Easy
	case engine.Medium:
		return &s.Medium
	case engine.Hard:
		return &s.Hard
	}
	return &s.Expert
}

// Start records the new game next replacing prev (nil if none). Leaving
// prev unsolved breaks its level's streak. Games started by number don't
// count either way.
func (s *Stats) Start(prev, next *Game) {
	if prev != nil && !prev.recorded && !prev.byNumber && !prev.Solved() {
		s.Level(prev.level).Streak = 0
	}
	if !next.byNumber {
		s.Level(next.level).Played++
	}
}

// Solve records g as solved after g.Elapsed(now). It reports whether that is
// a new best time for the level (the first solve is one) and the streak the
// solve extended: the daily streak for a daily puzzle, else the level's.
// A game counts once; solving it again after an undo changes nothing.
func (s *Stats) Solve(g *Game, now time.Time) (best bool, streak int) {
	if g.byNumber {
		g.recorded = true
		return false, 0
	}
	ls := s.Level(g.level)
	if !g.recorded {
		g.recorded = true
		ms := g.Elapsed(now).Milliseconds()
		if best = ls.Solved == 0 || ms < ls.BestMs; best {
			ls.BestMs = ms
		}
		ls.Solved++
		ls.Streak++
		if d := &s.Daily; g.daily != "" && g.daily > d.Last {
			if d.Last != "" && g.daily == nextDay(d.Last) {
				d.Streak++
			} else {
				d.Streak = 1
			}
			d.Last = g.daily
		}
	}
	if g.daily != "" {
		return best, s.Daily.Streak
	}
	return best, ls.Streak
}

// View is s as of now: a daily streak whose last solve is older than
// yesterday reads 0.
func (s Stats) View(now time.Time) Stats {
	if d := s.Daily.Last; d != "" && nextDay(d) < now.Format(dateLayout) {
		s.Daily.Streak = 0
	}
	return s
}

const dateLayout = "2006-01-02"

// nextDay is the date after date ("2006-01-02"); "" if date doesn't parse.
func nextDay(date string) string {
	t, err := time.Parse(dateLayout, date)
	if err != nil {
		return ""
	}
	return t.AddDate(0, 0, 1).Format(dateLayout)
}

// Daily is the daily puzzle for now's calendar date (in now's location):
// the date, its seed (the date as YYYYMMDD) and its level by weekday: Monday
// and Tuesday easy, Wednesday and Thursday medium, Friday and Saturday hard,
// Sunday expert. engine.Generate is deterministic, so everyone gets the same
// puzzle all day.
func Daily(now time.Time) (date string, seed uint64, level engine.Level) {
	y, m, d := now.Date()
	seed = uint64(y)*10000 + uint64(m)*100 + uint64(d)
	switch now.Weekday() {
	case time.Monday, time.Tuesday:
		level = engine.Easy
	case time.Wednesday, time.Thursday:
		level = engine.Medium
	case time.Friday, time.Saturday:
		level = engine.Hard
	default:
		level = engine.Expert
	}
	return now.Format(dateLayout), seed, level
}
