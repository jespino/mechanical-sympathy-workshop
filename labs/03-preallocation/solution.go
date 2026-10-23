//go:build solution

package preallocation

// SquaresAfter allocates the exact final size once and writes by index.
func SquaresAfter(n int) []int {
	out := make([]int, n)
	for i := range out {
		out[i] = i * i
	}
	return out
}

// EvensAfter reserves capacity for the worst case (every number is even), so
// append never has to grow the backing array.
func EvensAfter(nums []int) []int {
	out := make([]int, 0, len(nums))
	for _, n := range nums {
		if n%2 == 0 {
			out = append(out, n)
		}
	}
	return out
}

// CountWordsAfter gives the map a size hint. len(words) is an upper bound on
// the number of distinct words, so the map never has to grow.
func CountWordsAfter(words []string) map[string]int {
	counts := make(map[string]int, len(words))
	for _, w := range words {
		counts[w]++
	}
	return counts
}
