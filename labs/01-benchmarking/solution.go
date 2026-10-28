//go:build solution

package benchmarking

// StatsAfter calls Sum once and reuses the result for the average.
func StatsAfter(nums []int) (sum, avg int) {
	sum = Sum(nums)
	return sum, sum / len(nums)
}
