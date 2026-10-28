// Package benchmarking is the lab for module 01: benchmarking.
package benchmarking

// Sum returns the sum of nums.
func Sum(nums []int) int {
	total := 0
	for _, n := range nums {
		total += n
	}
	return total
}
