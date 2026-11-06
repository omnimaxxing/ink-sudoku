package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"runtime/debug"
	"slices"
	"sync"
	"time"

	"sudoku/appload"
	"sudoku/hwr"
	"sudoku/kakuro"
	"sudoku/kgame"
	"sudoku/pen"
)

// Message types, as in PROTOCOL.md. Kakuro has no calibration messages
// (7–9, 109–111): it reads the samples Ink Sudoku's calibration collected.
const (
	msgState  int32 = 1
	msgMarks  int32 = 2
	msgHint   int32 = 3
	msgPen    int32 = 4
	msgError  int32 = 5
	msgSolved int32 = 6

	msgHello           int32 = 100
	msgNewGame         int32 = 101
	msgInk             int32 = 102
	msgSetCell         int32 = 103
	msgToggleCandidate int32 = 104
	msgHintRequest     int32 = 105
	msgUndo            int32 = 106
	msgSettings        int32 = 107
	msgLog             int32 = 108
)

// server handles one frontend. handle runs on the socket goroutine; the
// only other goroutine that touches game state is the one that starts the
// first game when hello finds no puzzle ready, and mu makes the two take
// turns. The pen reader only sends (send must be safe for that, as
// appload.Conn.Send is). The personal recognizer and today's daily are
// built on goroutines of their own and handed over on channels.
type server struct {
	mu sync.Mutex

	send     func(int32, string)
	store    kgame.Store
	gen      kgame.GenFunc
	queue    *kgame.Queue
	analyzer hwr.Analyzer
	model    *hwr.Model
	now      func() time.Time
	openPen  func() (*pen.Device, error)

	// personal is the recognizer (analyzer.Classifier in production),
	// learning from samples: Ink Sudoku's calibration.jsonl + samples.jsonl,
	// then Kakuro's samples.jsonl, then corrections made this session. It
	// is built from the samples loaded at startup (samples[:loaded]) in the
	// background, one CNN pass each, and swapped in by handle once it arrives
	// on personalReady; until then it is nil and the plain model reads ink.
	personal      *hwr.Personal
	personalReady chan *hwr.Personal
	loaded        int
	samples       []hwr.Sample

	// Today's daily puzzle (for dailyDate), generated in the background at
	// startup: a Sunday expert takes a while on the tablet.
	dailyDate  string
	dailyReady chan dailyPuzzle
	daily      *dailyPuzzle

	g         *kgame.Game
	stats     kgame.Stats
	connected bool
	gridSize  float32
	preparing bool // a goroutine is waiting for the first game's puzzle

	penMu  sync.Mutex
	screen [2]float32
	dev    *pen.Device
}

func newServer(store kgame.Store, send func(int32, string), gen kgame.GenFunc) *server {
	m := hwr.DefaultModel()
	sudoku, own, err := store.LoadSamples()
	if err != nil {
		log.Printf("samples: %v", err)
	}
	log.Printf("samples: %d from Ink Sudoku (%s), %d from Kakuro", len(sudoku), store.SudokuDir, len(own))
	// Clipped, so learn's appends copy rather than share the builder's array.
	samples := slices.Clip(append(slices.Clip(sudoku), own...))
	ready := make(chan *hwr.Personal, 1)
	go func() {
		start := time.Now()
		p := hwr.NewPersonal(m, samples)
		log.Printf("personalized with %d samples in %v", p.Len(), time.Since(start).Round(time.Millisecond))
		ready <- p
	}()
	s := &server{
		send:          send,
		store:         store,
		gen:           gen,
		queue:         kgame.NewQueue(gen),
		analyzer:      hwr.Analyzer{Classifier: m, Config: hwr.DefaultConfig()},
		model:         m,
		now:           time.Now,
		openPen:       pen.Open,
		personalReady: ready,
		dailyReady:    make(chan dailyPuzzle, 1),
		loaded:        len(samples),
		samples:       samples,
		screen:        [2]float32{1404, 1872},
	}
	date, seed, level := kgame.Daily(time.Now())
	s.dailyDate = date
	go func() {
		start := time.Now()
		p, err := kgame.Generate(gen, seed, level)
		if err != nil {
			log.Printf("daily: %v", err)
		} else {
			log.Printf("daily %s (%v) generated in %v", date, level, time.Since(start).Round(time.Millisecond))
		}
		s.dailyReady <- dailyPuzzle{date, p, err}
	}()
	if s.stats, err = store.LoadStats(); err != nil {
		log.Printf("stats: %v", err)
	}
	g, err := store.Load()
	switch {
	case err == nil:
		s.g = g
		log.Printf("restored game: seed %d %v", g.Seed(), g.Level())
	case errors.Is(err, fs.ErrNotExist):
	default:
		log.Printf("load: %v", err)
	}
	return s
}

func (s *server) handle(typ int32, body string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer func() {
		if r := recover(); r != nil {
			log.Printf("panic handling message %d: %v\n%s", typ, r, debug.Stack())
			s.fail(0, "internal error: %v", r)
		}
	}()
	s.adoptPersonal()
	if s.g != nil && typ >= msgHello && typ != msgLog {
		s.g.Touch(s.now())
	}
	switch typ {
	case appload.MsgNewCoordinator:
		log.Print("frontend attached")
	case appload.MsgLostCoordinator:
		log.Print("frontend detached")
		s.disconnect()
	case msgHello:
		s.hello(body)
	case msgNewGame:
		s.newGame(body)
	case msgInk:
		s.ink(body)
	case msgSetCell, msgToggleCandidate:
		s.cellEdit(typ, body)
	case msgHintRequest:
		s.hint(body)
	case msgUndo:
		s.undo()
	case msgSettings:
		s.settings(body)
	case msgLog:
		var req struct {
			Message string `json:"message"`
		}
		if s.decode(body, &req) {
			log.Printf("frontend: %s", req.Message)
		}
	default:
		s.fail(0, "unknown message type %d", typ)
	}
}

// close stops the timer and pen stream and saves; call before exiting.
func (s *server) close() {
	s.setPenStream(false)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.disconnect()
}

func (s *server) disconnect() {
	s.connected = false
	if s.g != nil {
		s.g.Disconnect(s.now())
		s.save()
	}
}

func (s *server) hello(body string) {
	var req struct {
		Screen   [2]float32 `json:"screen"`
		GridSize float32    `json:"gridSize"`
	}
	if !s.decode(body, &req) {
		return
	}
	if req.Screen[0] > 0 && req.Screen[1] > 0 {
		s.penMu.Lock()
		s.screen = req.Screen
		s.penMu.Unlock()
	}
	if req.GridSize > 0 {
		s.gridSize = req.GridSize
	}
	s.connected = true
	if s.g == nil {
		s.firstGame()
		return
	}
	s.g.SetGridSize(s.gridSize)
	s.g.Connect(s.now())
	s.sendState()
	s.save()
}

// firstGame starts a new easy game for hello without ever waiting on the
// generator: if the queue has no easy puzzle ready, a goroutine waits for
// one and starts the game then, which sends its state. Repeated hellos
// meanwhile get nothing (the frontend repeats hello until a state comes).
func (s *server) firstGame() {
	if p, ok := s.queue.TryTake(kakuro.Easy); ok {
		s.start(kgame.New(p))
		return
	}
	if s.preparing {
		return
	}
	s.preparing = true
	log.Print("hello: first puzzle not ready yet")
	go func() {
		p, err := s.queue.Take(kakuro.Easy)
		s.mu.Lock()
		defer s.mu.Unlock()
		s.preparing = false
		switch {
		case err != nil:
			s.fail(0, "new game: %v", err)
		case s.g == nil: // unless a newGame got there first
			s.start(kgame.New(p))
		}
	}()
}

func (s *server) newGame(body string) {
	var req struct {
		Level string `json:"level"`
		Seed  uint64 `json:"seed"` // optional: replay a given puzzle
		Daily bool   `json:"daily"`
	}
	if !s.decode(body, &req) {
		return
	}
	if req.Daily {
		date, seed, level := kgame.Daily(s.now())
		if s.g != nil && s.g.Daily() == date {
			s.sendState() // already playing today's puzzle: keep the progress
			return
		}
		p, ok := s.takeDaily(date)
		if !ok {
			var err error
			if p, err = kgame.Generate(s.gen, seed, level); err != nil {
				s.fail(0, "daily: %v", err)
				return
			}
		}
		s.start(kgame.NewDaily(p, date))
		return
	}
	level, err := kakuro.ParseLevel(req.Level)
	if err != nil {
		s.fail(0, "unknown level %q", req.Level)
		return
	}
	var p kakuro.Puzzle
	if req.Seed != 0 {
		p, err = kgame.Generate(s.gen, req.Seed, level)
	} else {
		p, err = s.queue.Take(level)
	}
	if err != nil {
		s.fail(0, "new game: %v", err)
		return
	}
	s.start(kgame.New(p))
}

// start replaces the current game with g, counts it in the stats and
// sends its state.
func (s *server) start(g *kgame.Game) {
	s.stats.Start(s.g, g.Level())
	s.saveStats()
	s.g = g
	s.g.SetGridSize(s.gridSize)
	if s.connected {
		s.g.Connect(s.now())
	}
	s.sendState()
	s.save()
}

func (s *server) ink(body string) {
	var req struct {
		ID       int64       `json:"id"`
		Cell     int         `json:"cell"`
		GridSize float32     `json:"gridSize"`
		Strokes  [][]float32 `json:"strokes"`
	}
	if !s.decode(body, &req) || !s.haveGame(req.ID) {
		return
	}
	if req.GridSize > 0 {
		s.gridSize = req.GridSize
	}
	now := s.now()
	was := s.g.Solved()
	res, err := s.g.Ink(s.analyzer, req.Cell, s.gridSize, req.Strokes, now)
	if err != nil {
		s.fail(req.ID, "ink: %v", err)
		return
	}
	reply := marksMsg{
		ID: req.ID, Cell: req.Cell, Marks: make([]markJSON, 0, len(res.Marks)),
		CellState: cellStateJSON{
			Entry: s.g.Entry(req.Cell), Candidates: s.g.CellCandidates(req.Cell), Ink: s.g.CellInk(req.Cell),
		},
		Conflicts: s.g.Conflicts(),
		Unknown:   res.Unknown,
		CanUndo:   s.g.CanUndo(),
	}
	for _, m := range res.Marks {
		reply.Marks = append(reply.Marks, toMarkJSON(m))
	}
	solved := s.checkSolved(was)
	s.reply(msgMarks, reply)
	s.afterChange(res.Changed, solved)
	if res.Correction != nil {
		sm := res.Correction.Sample(s.model, now)
		if err := s.store.AppendSample(sm); err != nil {
			log.Printf("samples: %v", err)
		}
		s.learn(sm)
	}
}

func (s *server) cellEdit(typ int32, body string) {
	var req struct {
		Cell  int `json:"cell"`
		Digit int `json:"digit"`
	}
	if !s.decode(body, &req) || !s.haveGame(0) {
		return
	}
	was := s.g.Solved()
	var changed bool
	var err error
	if typ == msgSetCell {
		changed, err = s.g.SetCell(req.Cell, req.Digit, s.now())
	} else {
		changed, err = s.g.ToggleCandidate(req.Cell, req.Digit, s.now())
	}
	if err != nil {
		s.fail(0, "%v", err)
		return
	}
	solved := s.checkSolved(was)
	s.sendState()
	s.afterChange(changed, solved)
}

// hint replies with the next step. {"reveal": false} is a nudge (the
// frontend shows only the title) and isn't counted; true or absent counts.
func (s *server) hint(body string) {
	var req struct {
		Reveal *bool `json:"reveal"`
	}
	if !s.decode(body, &req) || !s.haveGame(0) {
		return
	}
	reveal := req.Reveal == nil || *req.Reveal
	st, ok := s.g.Hint(reveal)
	if !ok {
		s.fail(0, "no hint: the puzzle is solved")
		return
	}
	s.reply(msgHint, st)
	if reveal {
		s.save()
	}
}

func (s *server) undo() {
	if !s.haveGame(0) {
		return
	}
	was := s.g.Solved()
	changed := s.g.Undo(s.now())
	solved := s.checkSolved(was)
	s.sendState()
	s.afterChange(changed, solved)
}

// checkSolved records a fresh solve in the stats, before the reply that
// shows it is sent, and returns the solved message to follow that reply.
func (s *server) checkSolved(wasSolved bool) *solvedMsg {
	if wasSolved || !s.g.Solved() {
		return nil
	}
	now := s.now()
	best, streak := s.stats.Solve(s.g, now)
	s.saveStats()
	return &solvedMsg{
		ElapsedMs: s.g.Elapsed(now).Milliseconds(), Mistakes: s.g.Mistakes(), Hints: s.g.Hints(),
		Best: best, Streak: streak,
	}
}

// afterChange announces a fresh solve and saves.
func (s *server) afterChange(changed bool, solved *solvedMsg) {
	if solved != nil {
		s.reply(msgSolved, solved)
	}
	if changed {
		s.save()
	}
}

// learn adds a labelled sample to the personal recognizer (or, while that
// is being built, keeps it for adoptPersonal).
func (s *server) learn(sm hwr.Sample) {
	if s.personal != nil {
		s.personal.Add(sm)
	}
	s.samples = append(s.samples, sm)
}

// adoptPersonal swaps in the personal recognizer once it's built, with the
// samples learned meanwhile.
func (s *server) adoptPersonal() {
	select {
	case p := <-s.personalReady:
		s.personalReady = nil
		for _, sm := range s.samples[s.loaded:] {
			p.Add(sm)
		}
		s.personal = p
		if s.analyzer.Classifier == hwr.Classifier(s.model) { // unless a test put in its own
			s.analyzer.Classifier = p
		}
	default:
	}
}

// personalLen is personal.Len(), or while it's being built, what that will be.
func (s *server) personalLen() int {
	if s.personal != nil {
		return s.personal.Len()
	}
	return hwr.PersonalLen(s.samples)
}

func (s *server) settings(body string) {
	var req struct {
		PenStream *bool `json:"penStream"`
	}
	if !s.decode(body, &req) {
		return
	}
	if req.PenStream != nil {
		s.setPenStream(*req.PenStream)
	}
}

// setPenStream starts or stops forwarding raw pen events.
func (s *server) setPenStream(on bool) {
	s.penMu.Lock()
	defer s.penMu.Unlock()
	if !on {
		if s.dev != nil {
			s.dev.Close()
			s.dev = nil
		}
		return
	}
	if s.dev != nil {
		return
	}
	dev, err := s.openPen()
	if err != nil {
		log.Printf("pen: %v", err)
		s.fail(0, "pen stream: %v", err)
		return
	}
	s.dev = dev
	log.Printf("pen stream on: %q %s ranges %+v", dev.Name, dev.Path, dev.Ranges)
	go func() {
		err := dev.Run(func(e pen.Event) {
			s.penMu.Lock()
			w, h := s.screen[0], s.screen[1]
			s.penMu.Unlock()
			s.reply(msgPen, penMsg{
				K: e.Kind.String(), T: e.Tool.String(),
				SX: round1(dev.Ranges.X.Norm(e.X) * w), SY: round1(dev.Ranges.Y.Norm(e.Y) * h),
				X: e.X, Y: e.Y, P: round2(dev.Ranges.Pressure.Norm(e.Pressure)), TS: e.Time,
			})
		})
		log.Printf("pen stream stopped: %v", err)
		s.penMu.Lock()
		if s.dev == dev {
			s.dev = nil
		}
		s.penMu.Unlock()
	}()
}

// haveGame reports whether there is a game, replying error (naming the ink
// message id, if any) when there isn't.
func (s *server) haveGame(id int64) bool {
	if s.g == nil {
		s.fail(id, "no game yet: send hello first")
		return false
	}
	return true
}

func (s *server) sendState() {
	g := s.g
	p := g.Puzzle()
	st := stateMsg{
		Puzzle: puzzleJSON{
			Number: g.Seed(), Seed: g.Seed(), Level: g.Level().String(), Hardest: g.Hardest(), Daily: g.Daily(),
			N: p.N, Cells: make([]cellJSON, len(p.Cells)),
		},
		Entries:    make([]int, len(p.Cells)),
		Candidates: g.Candidates(),
		Conflicts:  g.Conflicts(),
		Ink:        map[string][]kgame.Ink{},
		ElapsedMs:  g.Elapsed(s.now()).Milliseconds(),
		Solved:     g.Solved(),
		CanUndo:    g.CanUndo(),
		Stats:      s.stats.View(s.now()),

		PersonalSamples: s.personalLen(),
	}
	for c, cell := range p.Cells {
		st.Puzzle.Cells[c] = cellJSON(cell)
		st.Entries[c] = g.Entry(c)
		if ink := g.CellInk(c); len(ink) > 0 {
			st.Ink[fmt.Sprint(c)] = ink
		}
	}
	s.reply(msgState, st)
}

func (s *server) save() {
	if s.g == nil {
		return
	}
	if err := s.store.Save(s.g, s.now()); err != nil {
		log.Printf("save: %v", err)
	}
}

func (s *server) saveStats() {
	if err := s.store.SaveStats(s.stats); err != nil {
		log.Printf("stats: %v", err)
	}
}

func (s *server) decode(body string, v any) bool {
	if body == "" {
		body = "{}"
	}
	if err := json.Unmarshal([]byte(body), v); err != nil {
		s.fail(0, "bad message: %v", err)
		return false
	}
	return true
}

func (s *server) reply(typ int32, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		log.Printf("marshal %d: %v", typ, err)
		return
	}
	s.send(typ, string(b))
}

// fail sends an error message; id echoes the ink message id when there is one.
func (s *server) fail(id int64, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	log.Printf("error: %s", msg)
	s.reply(msgError, errorMsg{Message: msg, ID: id})
}

type dailyPuzzle struct {
	date string
	p    kakuro.Puzzle
	err  error
}

// takeDaily returns the puzzle generated at startup if it is date's. If it
// is still being generated, it waits for it rather than start another.
func (s *server) takeDaily(date string) (kakuro.Puzzle, bool) {
	if s.dailyReady != nil && date == s.dailyDate {
		d := <-s.dailyReady
		s.dailyReady = nil
		s.daily = &d
	}
	if s.daily == nil || s.daily.date != date || s.daily.err != nil {
		return kakuro.Puzzle{}, false
	}
	return s.daily.p, true
}
