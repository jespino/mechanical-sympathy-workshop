// Package preallocation is the lab for module 03: pre-allocation.
//
// before.go holds the naive implementations. Don't edit this file: it is the
// baseline that after.go is benchmarked against.
package preallocation

// SquaresBefore returns the squares of 0..n-1. The final size is known up
// front, but the slice starts empty and grows as we append.
func SquaresBefore(n int) []int {
	var out []int
	for i := range n {
		out = append(out, i*i)
	}
	return out
}

// EvensBefore returns the even numbers in nums. We don't know the final size,
// but we do know an upper bound: len(nums).
func EvensBefore(nums []int) []int {
	var out []int
	for _, n := range nums {
		if n%2 == 0 {
			out = append(out, n)
		}
	}
	return out
}

// CountWordsBefore counts how many times each word appears.
func CountWordsBefore(words []string) map[string]int {
	counts := map[string]int{}
	for _, w := range words {
		counts[w]++
	}
	return counts
}
