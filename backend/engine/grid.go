// Package engine generates, solves, grades and explains Sudoku puzzles.
//
// Cells are indexed 0..80 in row-major order. Candidate sets are uint16
// bitmasks where bit d-1 stands for digit d (see CandidateBit).
package engine

import (
	"errors"
	"fmt"
	"math/bits"
	"strconv"
	"strings"
)

// Grid is a Sudoku board in row-major order; 0 marks an empty cell.
type Grid [81]uint8

const allDigits uint16 = 0x1FF

// CandidateBit returns the candidate-mask bit for digit d (1..9).
func CandidateBit(d uint8) uint16 { return 1 << (d - 1) }

// House is a row (0-8), column (9-17) or box (18-26). Boxes are numbered
// left to right, top to bottom.
type House uint8

func (h House) String() string {
	switch {
	case h < 9:
		return fmt.Sprintf("row %d", h+1)
	case h < 18:
		return fmt.Sprintf("column %d", h-8)
	default:
		return fmt.Sprintf("box %d", h-17)
	}
}

// MarshalJSON keeps houses numeric; as a uint8 slice, []House would
// otherwise encode as a base64 string.
func (h House) MarshalJSON() ([]byte, error) { return strconv.AppendUint(nil, uint64(h), 10), nil }

// DigitList marshals as a JSON array of numbers rather than base64.
type DigitList []uint8

func (d DigitList) MarshalJSON() ([]byte, error) {
	out := []byte{'['}
	for i, v := range d {
		if i > 0 {
			out = append(out, ',')
		}
		out = strconv.AppendUint(out, uint64(v), 10)
	}
	return append(out, ']'), nil
}

// CellName returns the r<row>c<col> name of cell c, 1-based.
func CellName(c int) string { return fmt.Sprintf("r%dc%d", c/9+1, c%9+1) }

var (
	cellRow, cellCol, cellBox [81]uint8
	houseCells                [27][9]uint8
	peers                     [81][20]uint8
	peerSet                   [81]cellSet
	houseSet                  [27]cellSet
)

func init() {
	for c := 0; c < 81; c++ {
		r, col := c/9, c%9
		b := (r/3)*3 + col/3
		cellRow[c], cellCol[c], cellBox[c] = uint8(r), uint8(col), uint8(b)
		houseCells[r][col] = uint8(c)
		houseCells[9+col][r] = uint8(c)
		houseCells[18+b][(r%3)*3+col%3] = uint8(c)
	}
	for h := 0; h < 27; h++ {
		for _, c := range houseCells[h] {
			houseSet[h].add(int(c))
		}
	}
	for c := 0; c < 81; c++ {
		s := houseSet[cellRow[c]].or(houseSet[9+cellCol[c]]).or(houseSet[18+cellBox[c]])
		s.remove(c)
		peerSet[c] = s
		n := 0
		s.each(func(p int) { peers[c][n] = uint8(p); n++ })
	}
}

// cellSet is a set of cells, bit c of the 128-bit value.
type cellSet [2]uint64

func (s *cellSet) add(c int)            { s[c>>6] |= 1 << (c & 63) }
func (s *cellSet) remove(c int)         { s[c>>6] &^= 1 << (c & 63) }
func (s cellSet) has(c int) bool        { return s[c>>6]&(1<<(c&63)) != 0 }
func (s cellSet) and(t cellSet) cellSet { return cellSet{s[0] & t[0], s[1] & t[1]} }
func (s cellSet) or(t cellSet) cellSet  { return cellSet{s[0] | t[0], s[1] | t[1]} }
func (s cellSet) andNot(t cellSet) cellSet {
	return cellSet{s[0] &^ t[0], s[1] &^ t[1]}
}
func (s cellSet) empty() bool { return s[0]|s[1] == 0 }
func (s cellSet) count() int  { return bits.OnesCount64(s[0]) + bits.OnesCount64(s[1]) }

func (s cellSet) each(f func(c int)) {
	for w := 0; w < 2; w++ {
		for x := s[w]; x != 0; x &= x - 1 {
			f(w<<6 | bits.TrailingZeros64(x))
		}
	}
}

func (s cellSet) list() []int {
	out := make([]int, 0, s.count())
	s.each(func(c int) { out = append(out, c) })
	return out
}

// ParseGrid reads 81 cells from s. Digits 1-9 are givens; '0' and '.' are
// empty. Whitespace and the box-drawing characters | - + are ignored, so the
// output of Pretty parses back.
func ParseGrid(s string) (Grid, error) {
	var g Grid
	n := 0
	for _, r := range s {
		switch {
		case r >= '1' && r <= '9', r == '0', r == '.':
			if n == 81 {
				return Grid{}, errors.New("engine: more than 81 cells")
			}
			if r >= '1' && r <= '9' {
				g[n] = uint8(r - '0')
			}
			n++
		case r == ' ', r == '\t', r == '\n', r == '\r', r == '|', r == '-', r == '+':
		default:
			return Grid{}, fmt.Errorf("engine: unexpected character %q", r)
		}
	}
	if n != 81 {
		return Grid{}, fmt.Errorf("engine: got %d cells, want 81", n)
	}
	return g, nil
}

// String returns the 81-character form, '.' for empty cells.
func (g Grid) String() string {
	var b [81]byte
	for i, v := range g {
		if v == 0 {
			b[i] = '.'
		} else {
			b[i] = '0' + v
		}
	}
	return string(b[:])
}

// Pretty returns a 9-line grid with box separators.
func (g Grid) Pretty() string {
	var sb strings.Builder
	for r := 0; r < 9; r++ {
		if r == 3 || r == 6 {
			sb.WriteString("------+-------+------\n")
		}
		for c := 0; c < 9; c++ {
			if c == 3 || c == 6 {
				sb.WriteString("| ")
			}
			if v := g[r*9+c]; v == 0 {
				sb.WriteByte('.')
			} else {
				sb.WriteByte('0' + v)
			}
			if c < 8 {
				sb.WriteByte(' ')
			}
		}
		sb.WriteByte('\n')
	}
	return sb.String()
}

// Count returns the number of filled cells.
func (g Grid) Count() int {
	n := 0
	for _, v := range g {
		if v != 0 {
			n++
		}
	}
	return n
}

// Valid reports whether every value is 0..9 and no digit repeats in a house.
func (g Grid) Valid() bool {
	for h := 0; h < 27; h++ {
		var seen uint16
		for _, c := range houseCells[h] {
			v := g[c]
			if v == 0 {
				continue
			}
			if v > 9 || seen&CandidateBit(v) != 0 {
				return false
			}
			seen |= CandidateBit(v)
		}
	}
	return true
}

// AutoCandidates returns, for each empty cell, the digits not already used
// in its row, column or box. Filled cells get 0.
func AutoCandidates(g Grid) [81]uint16 {
	var used [27]uint16
	for c, v := range g {
		if v != 0 && v <= 9 {
			b := CandidateBit(v)
			used[cellRow[c]] |= b
			used[9+cellCol[c]] |= b
			used[18+cellBox[c]] |= b
		}
	}
	var out [81]uint16
	for c, v := range g {
		if v == 0 {
			out[c] = allDigits &^ (used[cellRow[c]] | used[9+cellCol[c]] | used[18+cellBox[c]])
		}
	}
	return out
}

// digitsOf lists the digits in mask in ascending order.
func digitsOf(mask uint16) []uint8 {
	out := make([]uint8, 0, bits.OnesCount16(mask))
	for m := mask; m != 0; m &= m - 1 {
		out = append(out, uint8(bits.TrailingZeros16(m))+1)
	}
	return out
}

func lowDigit(mask uint16) uint8 { return uint8(bits.TrailingZeros16(mask)) + 1 }
