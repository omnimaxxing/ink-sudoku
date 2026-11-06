package game

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sudoku/engine"
	"sudoku/hwr"
)

// DefaultDir is $INK_SUDOKU_DIR, else the app's data dir on the tablet.
func DefaultDir() string {
	if d := os.Getenv("INK_SUDOKU_DIR"); d != "" {
		return d
	}
	return "/home/root/.local/share/ink-sudoku"
}

// Store keeps the saved game (state.json), the player's stats (stats.json)
// and the hwr sample logs (samples.jsonl, calibration.jsonl) in Dir.
// Shared lists other games' sample logs (e.g. Ink Kakuro's corrections) that
// LoadSamples also reads, so every game learns the same hand; they are never
// written.
type Store struct {
	Dir    string
	Shared []string
}

// KakuroSamples is Ink Kakuro's correction log ($INK_KAKURO_DIR, else its
// data dir on the tablet).
func KakuroSamples() string {
	d := os.Getenv("INK_KAKURO_DIR")
	if d == "" {
		d = "/home/root/.local/share/ink-kakuro"
	}
	return filepath.Join(d, "samples.jsonl")
}

func (s Store) statePath() string { return filepath.Join(s.Dir, "state.json") }

// file is state.json. Strokes are stored once and referenced by id from the
// board and from every undo snapshot.
type file struct {
	V         int               `json:"v"`
	Seed      uint64            `json:"seed"`
	Level     string            `json:"level"`
	Hardest   string            `json:"hardest"`
	Daily     string            `json:"daily,omitempty"`
	Recorded  bool              `json:"recorded,omitempty"`
	ByNumber  bool              `json:"byNumber,omitempty"`
	Givens    string            `json:"givens"`
	Solution  string            `json:"solution"`
	GridSize  float32           `json:"gridSize"`
	ElapsedMs int64             `json:"elapsedMs"`
	Mistakes  int               `json:"mistakes"`
	Hints     int               `json:"hints"`
	RuledOut  map[int]uint16    `json:"ruledOut,omitempty"` // cell -> digits revealed hints eliminated
	Board     fileBoard         `json:"board"`
	Undo      []fileBoard       `json:"undo"`
	Strokes   map[int][]float32 `json:"strokes"`
}

type fileBoard struct {
	Entries    string     `json:"entries"`
	Candidates [81]uint16 `json:"candidates"`
	// Ink maps cell -> strokes as [id, 0 answer | 1 candidate, digit].
	Ink map[int][][3]int `json:"ink,omitempty"`
}

// Save writes the game atomically (temp file + rename).
func (s Store) Save(g *Game, now time.Time) error {
	f := file{
		V: 1, Seed: g.seed, Level: strings.ToLower(g.level.String()), Hardest: g.hardest,
		Daily: g.daily, Recorded: g.recorded, ByNumber: g.byNumber,
		Givens: g.givens.String(), Solution: g.solution.String(), GridSize: g.gridSize,
		ElapsedMs: g.Elapsed(now).Milliseconds(), Mistakes: g.mistakes, Hints: g.hints,
		Strokes: map[int][]float32{},
	}
	for c, m := range g.ruledOut {
		if m != 0 {
			if f.RuledOut == nil {
				f.RuledOut = map[int]uint16{}
			}
			f.RuledOut[c] = m
		}
	}
	enc := func(b *board) fileBoard {
		fb := fileBoard{Entries: b.entries.String(), Candidates: b.candidates}
		for c, refs := range b.ink {
			for _, r := range refs {
				if fb.Ink == nil {
					fb.Ink = map[int][][3]int{}
				}
				role := 0
				if r.cand {
					role = 1
				}
				fb.Ink[c] = append(fb.Ink[c], [3]int{r.id, role, int(r.digit)})
				f.Strokes[r.id] = g.points[r.id]
			}
		}
		return fb
	}
	f.Board = enc(&g.board)
	for i := range g.undo {
		f.Undo = append(f.Undo, enc(&g.undo[i]))
	}
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	return s.writeAtomic("state.json", b)
}

// writeAtomic replaces Dir/name with b (temp file + rename).
func (s Store) writeAtomic(name string, b []byte) error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.Dir, "."+name+"-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op once renamed
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), filepath.Join(s.Dir, name))
}

// Load reads the saved game. A missing file returns an error satisfying
// errors.Is(err, fs.ErrNotExist); an unreadable one is moved aside to
// state.json.bad so the next save doesn't destroy it.
func (s Store) Load() (*Game, error) {
	b, err := os.ReadFile(s.statePath())
	if err != nil {
		return nil, err
	}
	g, err := decode(b)
	if err != nil {
		os.Rename(s.statePath(), s.statePath()+".bad")
		return nil, fmt.Errorf("state.json: %w (moved to state.json.bad)", err)
	}
	return g, nil
}

func decode(b []byte) (*Game, error) {
	var f file
	if err := json.Unmarshal(b, &f); err != nil {
		return nil, err
	}
	if f.V != 1 {
		return nil, fmt.Errorf("unknown version %d", f.V)
	}
	level, err := engine.ParseLevel(f.Level)
	if err != nil || level > engine.Expert {
		return nil, fmt.Errorf("bad level %q", f.Level)
	}
	g := &Game{seed: f.Seed, level: level, hardest: f.Hardest, daily: f.Daily, recorded: f.Recorded, byNumber: f.ByNumber, gridSize: f.GridSize,
		elapsed: time.Duration(f.ElapsedMs) * time.Millisecond, mistakes: f.Mistakes, hints: f.Hints,
		points: map[int][]float32{}}
	for c, m := range f.RuledOut {
		if !validCell(c) || m >= 1<<9 {
			return nil, fmt.Errorf("ruledOut: cell %d mask %#x", c, m)
		}
		g.ruledOut[c] = m
	}
	if g.givens, err = engine.ParseGrid(f.Givens); err != nil {
		return nil, fmt.Errorf("givens: %w", err)
	}
	if g.solution, err = engine.ParseGrid(f.Solution); err != nil || g.solution.Count() != 81 {
		return nil, fmt.Errorf("bad solution")
	}
	for id, pts := range f.Strokes {
		if len(pts) < 4 || len(pts)%4 != 0 {
			return nil, fmt.Errorf("stroke %d: %d values", id, len(pts))
		}
		g.points[id] = pts
		g.nextID = max(g.nextID, id)
	}
	dec := func(fb fileBoard) (board, error) {
		var bd board
		var err error
		if bd.entries, err = engine.ParseGrid(fb.Entries); err != nil {
			return bd, fmt.Errorf("entries: %w", err)
		}
		bd.candidates = fb.Candidates
		for c, refs := range fb.Ink {
			if !validCell(c) {
				return bd, fmt.Errorf("ink for cell %d", c)
			}
			for _, r := range refs {
				if _, ok := g.points[r[0]]; !ok || r[2] < 1 || r[2] > 9 {
					return bd, fmt.Errorf("cell %d: bad ink ref %v", c, r)
				}
				bd.ink[c] = append(bd.ink[c], ref{id: r[0], cand: r[1] == 1, digit: uint8(r[2])})
			}
		}
		return bd, nil
	}
	if g.board, err = dec(f.Board); err != nil {
		return nil, err
	}
	for _, fb := range f.Undo {
		bd, err := dec(fb)
		if err != nil {
			return nil, fmt.Errorf("undo: %w", err)
		}
		g.undo = append(g.undo, bd)
	}
	if len(g.undo) > UndoDepth {
		g.undo = g.undo[len(g.undo)-UndoDepth:]
	}
	return g, nil
}

// Sample logs: corrections from play, and labelled calibration samples.
const (
	correctionsFile = "samples.jsonl"
	calibrationFile = "calibration.jsonl"
)

// AppendSample appends one correction record to samples.jsonl.
func (s Store) AppendSample(sm hwr.Sample) error { return s.appendSample(correctionsFile, sm) }

// AppendCalibration appends one calibration sample to calibration.jsonl.
func (s Store) AppendCalibration(sm hwr.Sample) error { return s.appendSample(calibrationFile, sm) }

func (s Store) appendSample(name string, sm hwr.Sample) error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(s.Dir, name), os.O_APPEND|os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	// After a torn last line (power lost mid-append), start a new one so
	// this sample doesn't run into it.
	var last [1]byte
	if fi, err := f.Stat(); err == nil && fi.Size() > 0 {
		if _, err := f.ReadAt(last[:], fi.Size()-1); err == nil && last[0] != '\n' {
			f.Write([]byte{'\n'})
		}
	}
	if err := hwr.AppendSample(f, sm); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// LoadSamples reads the player's labelled samples: calibration.jsonl, then
// samples.jsonl, then the Shared logs. Missing files are empty; a bad line
// is skipped and reported.
func (s Store) LoadSamples() ([]hwr.Sample, error) {
	var out []hwr.Sample
	var errs []error
	paths := []string{filepath.Join(s.Dir, calibrationFile), filepath.Join(s.Dir, correctionsFile)}
	for _, path := range append(paths, s.Shared...) {
		name := filepath.Base(path)
		f, err := os.Open(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			errs = append(errs, err)
			continue
		}
		got, err := hwr.ReadSamples(f)
		f.Close()
		out = append(out, got...)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
		}
	}
	return out, errors.Join(errs...)
}

// LoadStats reads stats.json. A missing file gives zero stats; an
// unreadable one is moved aside to stats.json.bad and also gives zero stats,
// with the error.
func (s Store) LoadStats() (Stats, error) {
	path := filepath.Join(s.Dir, "stats.json")
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Stats{}, nil
	}
	if err != nil {
		return Stats{}, err
	}
	var st Stats
	if err := json.Unmarshal(b, &st); err != nil {
		os.Rename(path, path+".bad")
		return Stats{}, fmt.Errorf("stats.json: %w (moved to stats.json.bad)", err)
	}
	return st, nil
}

// SaveStats writes stats.json atomically.
func (s Store) SaveStats(st Stats) error {
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return s.writeAtomic("stats.json", b)
}
