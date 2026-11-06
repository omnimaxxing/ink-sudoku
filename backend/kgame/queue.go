package kgame

import (
	"fmt"
	"math/rand/v2"
	"sync"

	"sudoku/kakuro"
)

// MaxSeed bounds random seeds, so the seed doubles as a short puzzle number
// (and stays exact as a JavaScript number).
const MaxSeed = 999_999

// GenFunc makes a puzzle; kakuro.Generate in production.
type GenFunc func(seed uint64, level kakuro.Level) kakuro.Puzzle

// Generate runs gen, turning a panic or a puzzle that fails Validate into an
// error, so a generator bug costs one game rather than the backend.
func Generate(gen GenFunc, seed uint64, level kakuro.Level) (p kakuro.Puzzle, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("generate %v seed %d: %v", level, seed, r)
		}
	}()
	p = gen(seed, level)
	if err := Validate(p); err != nil {
		return kakuro.Puzzle{}, fmt.Errorf("generate %v seed %d: %w", level, seed, err)
	}
	return p, nil
}

const levels = int(kakuro.Expert) + 1

// Queue keeps one pre-generated puzzle per level so a new game starts
// instantly. It is safe for concurrent use.
type Queue struct {
	gen  GenFunc
	seed func() uint64

	mu    sync.Mutex
	done  *sync.Cond // a background generation finished
	ready [levels]*kakuro.Puzzle
	busy  [levels]bool
}

// NewQueue starts filling the queue in the background with gen, one level at
// a time (easiest first) so it never takes both cores.
func NewQueue(gen GenFunc) *Queue {
	return newQueue(gen, func() uint64 { return 1 + rand.Uint64N(MaxSeed) })
}

func newQueue(gen GenFunc, seed func() uint64) *Queue {
	q := &Queue{gen: gen, seed: seed}
	q.done = sync.NewCond(&q.mu)
	go func() {
		for l := kakuro.Easy; l <= kakuro.Expert; l++ {
			q.fill(l)
		}
	}()
	return q
}

// TryTake returns the pre-generated puzzle at level without waiting, if it
// is ready (and then generates a replacement in the background).
func (q *Queue) TryTake(level kakuro.Level) (kakuro.Puzzle, bool) {
	q.mu.Lock()
	p := q.ready[level]
	q.ready[level] = nil
	q.mu.Unlock()
	if p == nil {
		return kakuro.Puzzle{}, false
	}
	go q.fill(level)
	return *p, true
}

// Take returns a fresh puzzle at level: the pre-generated one if it is
// ready, else the one being generated (waiting for it), else one generated
// now. Either way a replacement is generated in the background.
func (q *Queue) Take(level kakuro.Level) (kakuro.Puzzle, error) {
	q.mu.Lock()
	for q.ready[level] == nil && q.busy[level] {
		q.done.Wait()
	}
	p := q.ready[level]
	q.ready[level] = nil
	q.mu.Unlock()
	if p == nil {
		gp, err := Generate(q.gen, q.seed(), level)
		if err != nil {
			return kakuro.Puzzle{}, err
		}
		p = &gp
	}
	go q.fill(level)
	return *p, nil
}

// fill generates a puzzle for level unless one is ready or on its way. A
// failure leaves the level empty, so the next Take generates (and reports).
func (q *Queue) fill(level kakuro.Level) {
	q.mu.Lock()
	if q.ready[level] != nil || q.busy[level] {
		q.mu.Unlock()
		return
	}
	q.busy[level] = true
	q.mu.Unlock()
	p, err := Generate(q.gen, q.seed(), level)
	q.mu.Lock()
	if err == nil {
		q.ready[level] = &p
	}
	q.busy[level] = false
	q.done.Broadcast()
	q.mu.Unlock()
}
