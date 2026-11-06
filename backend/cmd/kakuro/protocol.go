package main

import (
	"fmt"
	"math"

	"sudoku/hwr"
	"sudoku/kakuro"
	"sudoku/kgame"
)

// Wire shapes of the backend -> frontend messages (PROTOCOL.md, with
// KAKURO.md's differences).

// cellJSON is one grid square: {"b":0} for a white cell, {"b":1,"d":17,"a":0}
// for a clue cell (d and a always present, 0 = no clue).
type cellJSON kakuro.Cell

func (c cellJSON) MarshalJSON() ([]byte, error) {
	if !c.Black {
		return []byte(`{"b":0}`), nil
	}
	return fmt.Appendf(nil, `{"b":1,"d":%d,"a":%d}`, c.Down, c.Across), nil
}

type puzzleJSON struct {
	Number  uint64     `json:"number"`
	Seed    uint64     `json:"seed"`
	Level   string     `json:"level"`
	Hardest string     `json:"hardest"`
	Daily   string     `json:"daily"` // date of the daily puzzle, "" otherwise
	N       int        `json:"n"`
	Cells   []cellJSON `json:"cells"`
}

type stateMsg struct {
	Puzzle     puzzleJSON             `json:"puzzle"`
	Entries    []int                  `json:"entries"`    // n*n, 0 = empty or clue cell
	Candidates []uint16               `json:"candidates"` // n*n, bit d-1 = digit d
	Conflicts  []int                  `json:"conflicts"`
	Ink        map[string][]kgame.Ink `json:"ink"`
	ElapsedMs  int64                  `json:"elapsedMs"`
	Solved     bool                   `json:"solved"`
	CanUndo    bool                   `json:"canUndo"`
	Stats      kgame.Stats            `json:"stats"`
	// PersonalSamples is how many labelled samples back the recognizer
	// (Ink Sudoku's plus Kakuro's own).
	PersonalSamples int `json:"personalSamples"`
}

type markJSON struct {
	Kind       string       `json:"kind"`
	Digit      int          `json:"digit"`
	Confidence float32      `json:"confidence"`
	Top3       [][2]float32 `json:"top3"`
	BBox       [4]float32   `json:"bbox"` // x, y, w, h in grid px
	Strokes    []int        `json:"strokes"`
	Erases     []int        `json:"erases"`
}

type cellStateJSON struct {
	Entry      int         `json:"entry"`
	Candidates uint16      `json:"candidates"`
	Ink        []kgame.Ink `json:"ink"`
}

type marksMsg struct {
	ID        int64         `json:"id"`
	Cell      int           `json:"cell"`
	Marks     []markJSON    `json:"marks"`
	CellState cellStateJSON `json:"cellState"`
	Conflicts []int         `json:"conflicts"`
	Unknown   bool          `json:"unknown"`
	CanUndo   bool          `json:"canUndo"`
}

type solvedMsg struct {
	ElapsedMs int64 `json:"elapsedMs"`
	Mistakes  int   `json:"mistakes"`
	Hints     int   `json:"hints"`
	Best      bool  `json:"best"`   // new best time for the level
	Streak    int   `json:"streak"` // daily streak for a daily, else the level's
}

type penMsg struct {
	K  string  `json:"k"`
	T  string  `json:"t"`
	SX float32 `json:"sx"`
	SY float32 `json:"sy"`
	X  int32   `json:"x"`
	Y  int32   `json:"y"`
	P  float32 `json:"p"`
	TS int64   `json:"ts"`
}

type errorMsg struct {
	Message string `json:"message"`
	ID      int64  `json:"id,omitempty"`
}

func toMarkJSON(m hwr.Mark) markJSON {
	out := markJSON{
		Digit: m.Digit, Confidence: round3(m.Confidence), Top3: [][2]float32{},
		BBox:    [4]float32{round1(m.BBox.X0), round1(m.BBox.Y0), round1(m.BBox.W()), round1(m.BBox.H())},
		Strokes: m.Strokes, Erases: m.Erases,
	}
	switch m.Kind {
	case hwr.Answer:
		out.Kind = "answer"
	case hwr.Candidate:
		out.Kind = "candidate"
	case hwr.ScratchOut:
		out.Kind = "scratch"
	default:
		out.Kind = "unknown"
	}
	if m.Kind != hwr.ScratchOut {
		for _, g := range m.Top3 {
			if g.Digit != 0 {
				out.Top3 = append(out.Top3, [2]float32{float32(g.Digit), round3(g.Prob)})
			}
		}
	}
	if out.Strokes == nil {
		out.Strokes = []int{}
	}
	if out.Erases == nil {
		out.Erases = []int{}
	}
	return out
}

func roundTo(v, scale float32) float32 {
	return float32(math.Round(float64(v)*float64(scale)) / float64(scale))
}

func round1(v float32) float32 { return roundTo(v, 10) }
func round2(v float32) float32 { return roundTo(v, 100) }
func round3(v float32) float32 { return roundTo(v, 1000) }
