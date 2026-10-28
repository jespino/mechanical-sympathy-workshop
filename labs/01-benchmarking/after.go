//go:build !solution

package benchmarking

// This is your file. It starts as a copy of before.go: change it, then run
// the benchmarks to compare against before.go.
// The reference solution is in solution.go (run with -tags solution).

// StatsAfter returns the sum and the average of nums.
// TODO: Sum is called twice with the same input. Call it once.
func StatsAfter(nums []int) (sum, avg int) {
	return Sum(nums), Sum(nums) / len(nums)
}
