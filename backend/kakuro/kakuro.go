// Package kakuro models, generates, solves, grades and explains Kakuro
// puzzles. See KAKURO.md for the contract shared with the game server and
// frontend.
//
// A puzzle is an N×N grid in row-major order (index r*N + c). Row 0 and
// column 0 are always black, so every white cell has a clue cell to its
// left and above it somewhere. Hint text names cells by their grid row and
// column, "row 3, column 5", which is what the screen shows when rows and
// columns are numbered from the clue row as 0: the first row and column
// that can hold white cells are row 1 and column 1.
//
// Candidate sets are uint16 masks where bit d-1 stands for digit d.
package kakuro

import (
	"fmt"
	"strings"
)

type Level int

const (
	Easy Level = iota
	Medium
	Hard
	Expert
)

var levelNames = [...]string{"easy", "medium", "hard", "expert"}

func (l Level) String() string {
	if l >= 0 && int(l) < len(levelNames) {
		return levelNames[l]
	}
	return fmt.Sprintf("Level(%d)", int(l))
}

// ParseLevel parses a level name, case-insensitively.
func ParseLevel(s string) (Level, error) {
	for l := Easy; l <= Expert; l++ {
		if strings.EqualFold(l.String(), s) {
			return l, nil
		}
	}
	return 0, fmt.Errorf("kakuro: unknown level %q", s)
}

// Size is the grid size N of a level, clue row and column included.
func (l Level) Size() int { return specs[l].n }

// Cell is one grid square. Black cells may carry the clue for the down run
// below them and/or the across run to their right (0 = none).
type Cell struct {
	Black  bool
	Down   uint8
	Across uint8
}

// Puzzle is an N×N grid, row-major (index r*N + c); row 0 and column 0 are
// always black.
type Puzzle struct {
	N          int
	Cells      []Cell
	Solution   []uint8 // 0 on black cells
	Seed       uint64
	Level      Level
	Hardest    string         // hardest technique the logical solve needs (Technique.String)
	Techniques map[string]int // technique name -> times used in the logical solve
}

// Cand is one candidate digit in one cell.
type Cand struct {
	Cell  int `json:"cell"`
	Digit int `json:"digit"`
}

// Step is one logical deduction, ready to show as a hint.
type Step struct {
	// Technique is a Technique name, or "Mistake" / "Reveal".
	Technique string `json:"technique"`
	// Title is the idea in plain words; Text explains it in a few short
	// sentences, naming cells "row 3, column 5".
	Title string `json:"title"`
	Text  string `json:"text"`
	// Cells are the cells the reasoning is about: the run (or crossing runs)
	// for most steps, the subset for pairs and triples, the offending cells
	// for a Mistake.
	Cells  []int `json:"cells,omitempty"`
	Digits []int `json:"digits,omitempty"`
	// Placement is set for steps that fill a cell, including Reveal.
	Placement    *Cand  `json:"placement,omitempty"`
	Eliminations []Cand `json:"eliminations,omitempty"`
}
