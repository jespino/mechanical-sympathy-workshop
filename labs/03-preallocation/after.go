//go:build !solution

package preallocation

// This is your file. It starts as a copy of before.go: change it so that it
// allocates less, then run the benchmarks to compare against before.go.
// The reference solution is in solution.go (run with -tags solution).

// SquaresAfter returns the squares of 0..n-1.
// TODO: the final size is known. Allocate it once.
func SquaresAfter(n int) []int {
	var out []int
	for i := range n {
		out = append(out, i*i)
	}
	return out
}

// EvensAfter returns the even numbers in nums.
// TODO: the final size is unknown, but len(nums) is an upper bound.
func EvensAfter(nums []int) []int {
	var out []int
	for _, n := range nums {
		if n%2 == 0 {
			out = append(out, n)
		}
	}
	return out
}

// CountWordsAfter counts how many times each word appears.
// TODO: maps accept a size hint too.
func CountWordsAfter(words []string) map[string]int {
	counts := map[string]int{}
	for _, w := range words {
		counts[w]++
	}
	return counts
}
