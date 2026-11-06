package game

import (
	"math/rand/v2"
	"sync"

	"sudoku/engine"
)

// MaxSeed bounds random seeds, so the seed doubles as a short puzzle number
// (and stays exact as a JavaScript number).
const MaxSeed = 999_999

// Queue keeps one pre-generated puzzle per level so a new game starts
// instantly. It is safe for concurrent use.
type Queue struct {
	gen  func(seed uint64, level engine.Level) engine.Puzzle
	seed func() uint64

	mu    sync.Mutex
	ready [engine.Expert + 1]*engine.Puzzle
	busy  [engine.Expert + 1]bool
}

// NewQueue starts filling the queue in the background, one level at a time
// (easiest first) so it never takes both cores.
func NewQueue() *Queue {
	return newQueue(engine.Generate, func() uint64 { return 1 + rand.Uint64N(MaxSeed) })
}

func newQueue(gen func(uint64, engine.Level) engine.Puzzle, seed func() uint64) *Queue {
	q := &Queue{gen: gen, seed: seed}
	go func() {
		for l := engine.Easy; l <= engine.Expert; l++ {
			q.fill(l)
		}
	}()
	return q
}

// Take returns a fresh puzzle at level (Easy..Expert): the pre-generated one
// if it is ready, else one generated now. Either way a replacement is
// generated in the background.
func (q *Queue) Take(level engine.Level) engine.Puzzle {
	q.mu.Lock()
	p := q.ready[level]
	q.ready[level] = nil
	q.mu.Unlock()
	if p == nil {
		gp := q.gen(q.seed(), level)
		p = &gp
	}
	go q.fill(level)
	return *p
}

// fill generates a puzzle for level unless one is ready or on its way.
func (q *Queue) fill(level engine.Level) {
	q.mu.Lock()
	if q.ready[level] != nil || q.busy[level] {
		q.mu.Unlock()
		return
	}
	q.busy[level] = true
	q.mu.Unlock()
	p := q.gen(q.seed(), level)
	q.mu.Lock()
	q.ready[level] = &p
	q.busy[level] = false
	q.mu.Unlock()
}
