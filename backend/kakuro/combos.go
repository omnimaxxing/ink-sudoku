package kakuro

import (
	"math/bits"
	"slices"
)

const allDigits uint16 = 0x1FF

// maxCombos is the most digit sets any (length, sum) has: 12, for 20 in
// four cells and 25 in five.
const maxCombos = 12

func bit(d uint8) uint16 { return 1 << (d - 1) }

func lowDigit(mask uint16) uint8 { return uint8(bits.TrailingZeros16(mask)) + 1 }

func single(mask uint16) bool { return mask != 0 && mask&(mask-1) == 0 }

// digitsOf lists the digits in mask in ascending order.
func digitsOf(mask uint16) []int {
	out := make([]int, 0, bits.OnesCount16(mask))
	for m := mask; m != 0; m &= m - 1 {
		out = append(out, bits.TrailingZeros16(m)+1)
	}
	return out
}

var (
	// combos[k][s] lists the sets of k distinct digits adding up to s, as
	// masks, in reading order (1 + 2 + 6 before 1 + 3 + 5). combos[0][0]
	// is the empty set.
	combos [10][46][]uint16
	// comboUnion and comboInter are the digits in some and in every set.
	comboUnion, comboInter [10][46]uint16
	maskSum                [512]uint8
	// minSum[k] and maxSum[k] bound the sum of k distinct digits.
	minSum, maxSum [10]int
)

func init() {
	combos[0][0] = []uint16{0}
	for m := 1; m < 512; m++ {
		s := 0
		for x := m; x != 0; x &= x - 1 {
			s += bits.TrailingZeros(uint(x)) + 1
		}
		maskSum[m] = uint8(s)
		k := bits.OnesCount(uint(m))
		combos[k][s] = append(combos[k][s], uint16(m))
	}
	for k := 0; k <= 9; k++ {
		minSum[k], maxSum[k] = k*(k+1)/2, k*(19-k)/2
		for s := 0; s <= 45; s++ {
			cs := combos[k][s]
			if len(cs) > maxCombos {
				panic("kakuro: maxCombos too small")
			}
			slices.SortFunc(cs, func(a, b uint16) int {
				// Lowest differing digit decides: the set holding it comes first.
				d := a ^ b
				if d == 0 {
					return 0
				}
				if a&(d&-d) != 0 {
					return -1
				}
				return 1
			})
			if len(cs) == 0 {
				continue
			}
			inter := allDigits
			for _, c := range cs {
				comboUnion[k][s] |= c
				inter &= c
			}
			comboInter[k][s] = inter
		}
	}
}

// sumOK reports whether k distinct digits can add up to s.
func sumOK(k, s int) bool { return k >= 0 && k <= 9 && s >= 0 && s <= 45 && len(combos[k][s]) > 0 }
