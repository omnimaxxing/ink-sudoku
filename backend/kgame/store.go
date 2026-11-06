package kgame

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sudoku/hwr"
	"sudoku/kakuro"
)

// DefaultDir is $INK_KAKURO_DIR, else the app's data dir on the tablet.
func DefaultDir() string {
	if d := os.Getenv("INK_KAKURO_DIR"); d != "" {
		return d
	}
	return "/home/root/.local/share/ink-kakuro"
}

// DefaultSudokuDir is Ink Sudoku's data dir ($INK_SUDOKU_DIR, as Ink Sudoku
// itself reads it), where the player's handwriting samples live.
func DefaultSudokuDir() string {
	if d := os.Getenv("INK_SUDOKU_DIR"); d != "" {
		return d
	}
	return "/home/root/.local/share/ink-sudoku"
}

// Store keeps the saved game (state.json), the player's stats (stats.json)
// and Kakuro's own hwr correction log (samples.jsonl) in Dir. It also reads
// the player's samples from Ink Sudoku's SudokuDir, which it never writes.
type Store struct {
	Dir       string
	SudokuDir string // "" reads no Ink Sudoku samples
}

func (s Store) statePath() string { return filepath.Join(s.Dir, "state.json") }

// stateVersion is state.json's format version.
const stateVersion = 1

// file is state.json. The whole puzzle is kept, so a restore never has to
// generate. Strokes are stored once and referenced by id from the board and
// from every undo snapshot.
type file struct {
	V         int               `json:"v"`
	Puzzle    filePuzzle        `json:"puzzle"`
	Daily     string            `json:"daily,omitempty"`
	Recorded  bool              `json:"recorded,omitempty"`
	GridSize  float32           `json:"gridSize"`
	ElapsedMs int64             `json:"elapsedMs"`
	Mistakes  int               `json:"mistakes"`
	Hints     int               `json:"hints"`
	Board     fileBoard         `json:"board"`
	Undo      []fileBoard       `json:"undo,omitempty"`
	Strokes   map[int][]float32 `json:"strokes,omitempty"`
}

type filePuzzle struct {
	N          int            `json:"n"`
	Seed       uint64         `json:"seed"`
	Level      string         `json:"level"`
	Hardest    string         `json:"hardest"`
	Techniques map[string]int `json:"techniques,omitempty"`
	Cells      [][3]int       `json:"cells"`    // [black 0|1, down clue, across clue]
	Solution   string         `json:"solution"` // a digit per cell, '.' on black
}

type fileBoard struct {
	Entries    string   `json:"entries"` // a digit per cell, '.' for none
	Candidates []uint16 `json:"candidates,omitempty"`
	// Ink maps cell -> strokes as [id, 0 answer | 1 candidate, digit].
	Ink map[int][][3]int `json:"ink,omitempty"`
}

func digits(v []uint8) string {
	var b strings.Builder
	for _, d := range v {
		if d == 0 {
			b.WriteByte('.')
		} else {
			b.WriteByte('0' + d)
		}
	}
	return b.String()
}

func parseDigits(s string, n int) ([]uint8, error) {
	if len(s) != n {
		return nil, fmt.Errorf("%d digits, want %d", len(s), n)
	}
	out := make([]uint8, n)
	for i := range n {
		switch c := s[i]; {
		case c == '.':
		case c >= '1' && c <= '9':
			out[i] = c - '0'
		default:
			return nil, fmt.Errorf("bad digit %q at %d", c, i)
		}
	}
	return out, nil
}

// Save writes the game atomically (temp file + rename).
func (s Store) Save(g *Game, now time.Time) error {
	p := g.puzzle
	f := file{
		V: stateVersion,
		Puzzle: filePuzzle{N: p.N, Seed: p.Seed, Level: p.Level.String(), Hardest: p.Hardest, Techniques: p.Techniques,
			Solution: digits(p.Solution)},
		Daily: g.daily, Recorded: g.recorded, GridSize: g.gridSize,
		ElapsedMs: g.Elapsed(now).Milliseconds(), Mistakes: g.mistakes, Hints: g.hints,
		Strokes: map[int][]float32{},
	}
	for _, c := range p.Cells {
		black := 0
		if c.Black {
			black = 1
		}
		f.Puzzle.Cells = append(f.Puzzle.Cells, [3]int{black, int(c.Down), int(c.Across)})
	}
	enc := func(b *board) fileBoard {
		fb := fileBoard{Entries: digits(b.entries), Candidates: b.candidates}
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
// errors.Is(err, fs.ErrNotExist); an unreadable one (including another
// format version) is moved aside to state.json.bad so the next save doesn't
// destroy it.
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
	if f.V != stateVersion {
		return nil, fmt.Errorf("unknown version %d", f.V)
	}
	fp := f.Puzzle
	level, err := kakuro.ParseLevel(fp.Level)
	if err != nil {
		return nil, err
	}
	p := kakuro.Puzzle{N: fp.N, Seed: fp.Seed, Level: level, Hardest: fp.Hardest, Techniques: fp.Techniques}
	cells := fp.N * fp.N
	if fp.N < 2 || len(fp.Cells) != cells {
		return nil, fmt.Errorf("puzzle: N %d with %d cells", fp.N, len(fp.Cells))
	}
	for _, c := range fp.Cells {
		if c[1] < 0 || c[1] > 45 || c[2] < 0 || c[2] > 45 {
			return nil, fmt.Errorf("puzzle: bad cell %v", c)
		}
		p.Cells = append(p.Cells, kakuro.Cell{Black: c[0] == 1, Down: uint8(c[1]), Across: uint8(c[2])})
	}
	if p.Solution, err = parseDigits(fp.Solution, cells); err != nil {
		return nil, fmt.Errorf("solution: %w", err)
	}
	if err := Validate(p); err != nil {
		return nil, err
	}
	g := New(p)
	g.daily, g.recorded, g.gridSize = f.Daily, f.Recorded, f.GridSize
	g.elapsed = time.Duration(f.ElapsedMs) * time.Millisecond
	g.mistakes, g.hints = f.Mistakes, f.Hints
	for id, pts := range f.Strokes {
		if len(pts) < 4 || len(pts)%4 != 0 {
			return nil, fmt.Errorf("stroke %d: %d values", id, len(pts))
		}
		g.points[id] = pts
		g.nextID = max(g.nextID, id)
	}
	dec := func(fb fileBoard) (board, error) {
		bd := newBoard(cells)
		if fb.Entries != "" {
			e, err := parseDigits(fb.Entries, cells)
			if err != nil {
				return bd, fmt.Errorf("entries: %w", err)
			}
			bd.entries = e
		}
		if fb.Candidates != nil {
			if len(fb.Candidates) != cells {
				return bd, fmt.Errorf("%d candidates, want %d", len(fb.Candidates), cells)
			}
			copy(bd.candidates, fb.Candidates)
		}
		for c, v := range bd.entries {
			if p.Cells[c].Black && (v != 0 || bd.candidates[c] != 0) {
				return bd, fmt.Errorf("entry in clue cell %d", c)
			}
		}
		for c, refs := range fb.Ink {
			if c < 0 || c >= cells || p.Cells[c].Black {
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

// samplesFile is the hwr correction log, in both apps' dirs; Ink Sudoku also
// keeps its calibration page's samples in calibrationFile.
const (
	samplesFile     = "samples.jsonl"
	calibrationFile = "calibration.jsonl"
)

// AppendSample appends one correction record to Dir/samples.jsonl (never to
// Ink Sudoku's).
func (s Store) AppendSample(sm hwr.Sample) error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(filepath.Join(s.Dir, samplesFile), os.O_APPEND|os.O_CREATE|os.O_RDWR, 0o644)
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

// LoadSamples reads the player's labelled samples: Ink Sudoku's
// calibration.jsonl and samples.jsonl, then Kakuro's own samples.jsonl
// (last, so hwr.Personal's per-digit cap drops the oldest Sudoku ones
// first). Missing files and a missing SudokuDir are empty; a bad line is
// skipped and reported.
func (s Store) LoadSamples() (sudoku, own []hwr.Sample, err error) {
	var errs []error
	read := func(dir, name string) []hwr.Sample {
		f, err := os.Open(filepath.Join(dir, name))
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		if err != nil {
			errs = append(errs, err)
			return nil
		}
		defer f.Close()
		got, err := hwr.ReadSamples(f)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", filepath.Join(dir, name), err))
		}
		return got
	}
	if s.SudokuDir != "" && filepath.Clean(s.SudokuDir) != filepath.Clean(s.Dir) {
		sudoku = append(read(s.SudokuDir, calibrationFile), read(s.SudokuDir, samplesFile)...)
	}
	own = read(s.Dir, samplesFile)
	return sudoku, own, errors.Join(errs...)
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
