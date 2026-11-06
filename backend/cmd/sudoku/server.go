package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"time"

	"sudoku/appload"
	"sudoku/engine"
	"sudoku/game"
	"sudoku/hwr"
	"sudoku/pen"
)

// Message types, as in PROTOCOL.md.
const (
	msgState  int32 = 1
	msgMarks  int32 = 2
	msgHint   int32 = 3
	msgPen    int32 = 4
	msgError  int32 = 5
	msgSolved int32 = 6

	msgCalibration     int32 = 7
	msgCalibrationRead int32 = 8
	msgPersonalized    int32 = 9

	msgHello           int32 = 100
	msgNewGame         int32 = 101
	msgInk             int32 = 102
	msgSetCell         int32 = 103
	msgToggleCandidate int32 = 104
	msgHintRequest     int32 = 105
	msgUndo            int32 = 106
	msgSettings        int32 = 107
	msgLog             int32 = 108

	msgCalibrationStart int32 = 109
	msgCalibrationInk   int32 = 110
	msgCalibrationDone  int32 = 111
	msgView             int32 = 112
)

// server handles one frontend. handle runs on the socket goroutine and
// display on the display watcher's; mu makes them take turns. The pen
// reader only sends (send must be safe for that, as appload.Conn.Send is).
// The personal recognizer is built on a goroutine of its own, which shares
// nothing with handle but the channel it's delivered on.
type server struct {
	mu sync.Mutex

	send     func(int32, string)
	store    game.Store
	queue    *game.Queue
	analyzer hwr.Analyzer
	model    *hwr.Model
	now      func() time.Time
	openPen  func() (*pen.Device, error)

	// personal is the recognizer (analyzer.Classifier in production),
	// learning from samples: calibration.jsonl + samples.jsonl so far. It is
	// built from the samples loaded at startup (samples[:loaded]) in the
	// background, one CNN pass each, and swapped in by handle once it arrives
	// on personalReady; until then it is nil and the plain model reads ink.
	personal      *hwr.Personal
	personalReady chan *hwr.Personal

	// Today's daily puzzle, generated in the background at startup: a
	// Sunday expert can take seconds on the tablet.
	daily      *dailyPuzzle
	dailyReady chan dailyPuzzle
	loaded     int
	samples    []hwr.Sample
	calib      game.Calibration

	g         *game.Game
	stats     game.Stats
	connected bool
	offBoard  bool // the frontend shows another page (`view`)
	asleep    bool // the display sleeps (display.Watch)
	gridSize  float32

	penMu  sync.Mutex
	screen [2]float32
	dev    *pen.Device
}

func newServer(store game.Store, send func(int32, string)) *server {
	m := hwr.DefaultModel()
	samples, err := store.LoadSamples()
	if err != nil {
		log.Printf("samples: %v", err)
	}
	samples = slices.Clip(samples) // so learn's appends copy rather than share the builder's array
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
		queue:         game.NewQueue(),
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
	go func() {
		date, seed, level := game.Daily(time.Now())
		s.dailyReady <- dailyPuzzle{date, engine.Generate(seed, level)}
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
	if s.asleep && typ >= msgHello && typ != msgLog {
		s.asleep = false // the player is here: a missed wake
		s.setAway()
	}
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
	case msgCalibrationStart:
		s.calib = game.Calibration{}
		s.sendCalibration()
	case msgCalibrationInk:
		s.calibrationInk(body)
	case msgCalibrationDone:
		s.calibrationDone()
	case msgView:
		var req struct {
			Board bool `json:"board"`
		}
		if s.decode(body, &req) {
			s.offBoard = !req.Board
			s.setAway()
		}
	default:
		s.fail(0, "unknown message type %d", typ)
	}
}

// close stops the timer and pen stream and saves; call before exiting.
func (s *server) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setPenStream(false)
	s.disconnect()
}

// display notes the display waking or going to sleep: the timer only runs
// while it's awake.
func (s *server) display(awake bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	log.Printf("display awake: %v", awake)
	s.asleep = !awake
	s.setAway()
}

// setAway pauses the timer while the board is off screen.
func (s *server) setAway() {
	if s.g != nil {
		s.g.SetAway(s.offBoard || s.asleep, s.now())
	}
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
		Board    *bool      `json:"board"`
	}
	if !s.decode(body, &req) {
		return
	}
	if req.Board != nil {
		s.offBoard = !*req.Board
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
		s.start(game.New(s.queue.Take(engine.Easy)))
		return
	}
	s.g.SetGridSize(s.gridSize)
	s.setAway()
	s.g.Connect(s.now())
	s.sendState()
	s.save()
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
		date, seed, level := game.Daily(s.now())
		if s.g != nil && s.g.Daily() == date {
			s.sendState() // already playing today's puzzle: keep the progress
			return
		}
		p, ok := s.takeDaily(date)
		if !ok {
			p = engine.Generate(seed, level)
		}
		s.start(game.NewDaily(p, date))
		return
	}
	level, err := engine.ParseLevel(req.Level)
	if err != nil || level > engine.Expert {
		s.fail(0, "unknown level %q", req.Level)
		return
	}
	var p engine.Puzzle
	if req.Seed != 0 {
		p = engine.Generate(req.Seed, level)
	} else {
		p = s.queue.Take(level)
	}
	s.start(game.New(p))
}

// start replaces the current game with g, counts it in the stats and
// sends its state.
func (s *server) start(g *game.Game) {
	s.stats.Start(s.g, g.Level())
	s.saveStats()
	s.g = g
	s.g.SetGridSize(s.gridSize)
	s.setAway()
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
	if !s.decode(body, &req) || !s.haveGame() {
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
	if !s.decode(body, &req) || !s.haveGame() {
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

func (s *server) hint(body string) {
	var req struct {
		Reveal *bool `json:"reveal"` // absent counts, as every hint did before nudges
	}
	if !s.decode(body, &req) || !s.haveGame() {
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
	if !s.haveGame() {
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

// calibrationInk reads one calibration sample, logs it with the prompted
// digit as its label and learns it. The game is never touched.
func (s *server) calibrationInk(body string) {
	var req struct {
		ID      int64       `json:"id"`
		Digit   int         `json:"digit"`
		Size    float32     `json:"size"`
		Strokes [][]float32 `json:"strokes"`
	}
	if !s.decode(body, &req) {
		return
	}
	if req.Digit < 1 || req.Digit > 9 || req.Size <= 0 {
		s.fail(req.ID, "calibrationInk: digit %d / size %g out of range", req.Digit, req.Size)
		return
	}
	strokes, err := game.ParseStrokes(req.Strokes)
	if err == nil && len(strokes) == 0 {
		err = errors.New("no ink")
	}
	if err != nil {
		s.fail(req.ID, "calibrationInk: %v", err)
		return
	}
	r, sm := game.CalibrationSample(s.analyzer.Classifier, s.model, s.analyzer.Config.AcceptProb, req.Size, strokes, req.Digit, s.now())
	if err := s.store.AppendCalibration(sm); err != nil {
		log.Printf("calibration: %v", err)
	}
	s.learn(sm)
	s.calib.Advance()
	s.reply(msgCalibrationRead, calibrationReadMsg{
		ID: req.ID, Digit: req.Digit, Read: r.Digit, Confidence: round3(r.Confidence),
		Done: s.calib.Done, Total: game.CalibrationTotal,
	})
	s.sendCalibration()
}

func (s *server) calibrationDone() {
	before, after := hwr.EvaluatePersonal(s.model, s.samples)
	log.Printf("personalized: %d samples, leave-one-out %.3f -> %.3f", len(s.samples), before, after)
	s.reply(msgPersonalized, personalizedMsg{Samples: s.personalLen(), Before: round(before, 1000), After: round(after, 1000)})
	s.calib = game.Calibration{}
	if s.g != nil {
		s.sendState()
	}
}

func (s *server) sendCalibration() {
	s.reply(msgCalibration, calibrationMsg{
		Total: game.CalibrationTotal, Done: s.calib.Done, Next: s.calib.Next(), Samples: s.personalLen(),
	})
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
// samples learned meanwhile. Only handle's goroutine touches personal and
// analyzer, so this needs no lock.
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

func (s *server) haveGame() bool {
	if s.g == nil {
		s.fail(0, "no game yet: send hello first")
		return false
	}
	return true
}

func (s *server) sendState() {
	g := s.g
	st := stateMsg{
		Puzzle: puzzleJSON{
			Number: g.Seed(), Seed: g.Seed(), Level: strings.ToLower(g.Level().String()),
			Hardest: g.Hardest(), Givens: g.Givens().String(), Daily: g.Daily(),
		},
		Entries:    g.Entries().String(),
		Candidates: g.Candidates(),
		Conflicts:  g.Conflicts(),
		Ink:        map[string][]game.Ink{},
		ElapsedMs:  g.Elapsed(s.now()).Milliseconds(),
		Solved:     g.Solved(),
		CanUndo:    g.CanUndo(),
		Stats:      s.stats.View(s.now()),

		PersonalSamples: s.personalLen(),
	}
	for c := 0; c < 81; c++ {
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
	p    engine.Puzzle
}

// takeDaily returns the pre-generated puzzle for date, if it is ready.
func (s *server) takeDaily(date string) (engine.Puzzle, bool) {
	select {
	case d := <-s.dailyReady:
		s.daily = &d
	default:
	}
	if s.daily == nil || s.daily.date != date {
		return engine.Puzzle{}, false
	}
	return s.daily.p, true
}
