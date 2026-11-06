package kakuro

import (
	"fmt"
	"math/bits"
	"slices"
	"strings"
)

// cellPhrase names a cell the way people say it: "row 3, column 5".
func cellPhrase(n, c int) string { return fmt.Sprintf("row %d, column %d", c/n, c%n) }

// cellsPhrase names cells compactly: "row 4, columns 2 and 7" when they
// share a row, "column 3, rows 1 and 5" when they share a column, else row
// by row ("row 1, column 2; row 4, columns 5 and 6; and row 9, column 1"),
// eliding long lists.
func cellsPhrase(n int, cells []int) string {
	cs := slices.Clone(cells)
	slices.Sort(cs)
	cs = slices.Compact(cs)
	if len(cs) == 0 {
		return ""
	}
	if len(cs) > 1 && slices.IndexFunc(cs, func(c int) bool { return c%n != cs[0]%n }) < 0 {
		return fmt.Sprintf("column %d, %s", cs[0]%n, numbered("row", cs, func(c int) int { return c / n }))
	}
	const maxGroups = 4
	var groups []string
	for i := 0; i < len(cs); {
		j := i
		for j < len(cs) && cs[j]/n == cs[i]/n {
			j++
		}
		if len(groups) == maxGroups-1 && j < len(cs) {
			groups = append(groups, fmt.Sprintf("%d more cells", len(cs)-i))
			break
		}
		groups = append(groups, fmt.Sprintf("row %d, %s", cs[i]/n, numbered("column", cs[i:j], func(c int) int { return c % n })))
		i = j
	}
	switch k := len(groups); k {
	case 1:
		return groups[0]
	case 2:
		return groups[0] + " and " + groups[1]
	default:
		return strings.Join(groups[:k-1], "; ") + "; and " + groups[k-1]
	}
}

// numbered is "row 3", "columns 2 and 5" or "columns 2 to 6" (a range when
// three or more are consecutive), from each cell's index.
func numbered(noun string, cells []int, index func(int) int) string {
	idx := make([]int, len(cells))
	for i, c := range cells {
		idx[i] = index(c)
	}
	slices.Sort(idx)
	if len(idx) > 1 {
		noun += "s"
	}
	if k := len(idx); k >= 3 && idx[k-1]-idx[0] == k-1 {
		return fmt.Sprintf("%s %d to %d", noun, idx[0], idx[k-1])
	}
	s := make([]string, len(idx))
	for i, v := range idx {
		s[i] = fmt.Sprint(v)
	}
	return noun + " " + joinList(s, "and")
}

// runName names a run by where it is: "the across run in row 3, columns 2
// to 5".
func runName(b *board, r int) string {
	rn := &b.runs[r]
	cells := make([]int, rn.n)
	for i, c := range rn.cells[:rn.n] {
		cells[i] = int(c)
	}
	if rn.down {
		return fmt.Sprintf("the down run in column %d, %s", cells[0]%b.n, numbered("row", cells, func(c int) int { return c / b.n }))
	}
	return fmt.Sprintf("the across run in row %d, %s", cells[0]/b.n, numbered("column", cells, func(c int) int { return c % b.n }))
}

// runCells lists a run's cells.
func runCells(b *board, r int) []int {
	rn := &b.runs[r]
	out := make([]int, rn.n)
	for i, c := range rn.cells[:rn.n] {
		out[i] = int(c)
	}
	return out
}

// comboPhrase writes a digit set as a sum: "1 + 2 + 4".
func comboPhrase(mask uint16) string {
	ds := digitsOf(mask)
	s := make([]string, len(ds))
	for i, d := range ds {
		s[i] = fmt.Sprint(d)
	}
	return strings.Join(s, " + ")
}

// combosPhrase lists digit sets as sums: "1 + 2 + 5 or 1 + 3 + 4".
func combosPhrase(masks []uint16) string {
	s := make([]string, len(masks))
	for i, m := range masks {
		s[i] = comboPhrase(m)
	}
	return joinList(s, "or")
}

// setPhrase lists the digits in mask: "1, 2 or 4", or "3 to 7" for four or
// more in a row.
func setPhrase(mask uint16, conj string) string {
	ds := digitsOf(mask)
	if k := len(ds); k >= 4 && ds[k-1]-ds[0] == k-1 {
		return fmt.Sprintf("%d to %d", ds[0], ds[k-1])
	}
	return digitPhrase(ds, conj)
}

func digitPhrase(ds []int, conj string) string {
	s := make([]string, len(ds))
	for i, d := range ds {
		s[i] = fmt.Sprint(d)
	}
	return joinList(s, conj)
}

func countWord(k int) string {
	if k >= 0 && k <= 9 {
		return [...]string{"no", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine"}[k]
	}
	return fmt.Sprint(k)
}

// aDigit is the digit with its article: "a 7", "an 8".
func aDigit(d int) string {
	if d == 8 {
		return "an 8"
	}
	return fmt.Sprintf("a %d", d)
}

func popcount(m uint16) int { return bits.OnesCount16(m) }

func upper(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func joinList(s []string, conj string) string {
	switch len(s) {
	case 0:
		return ""
	case 1:
		return s[0]
	}
	return strings.Join(s[:len(s)-1], ", ") + " " + conj + " " + s[len(s)-1]
}

func pick(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}
