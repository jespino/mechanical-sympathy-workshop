package benchmarking

// before.go holds the naive implementation. Don't edit this file: it is the
// baseline that after.go is benchmarked against.

// StatsBefore returns the sum and the average of nums. It calls Sum twice,
// so it walks the whole slice twice.
func StatsBefore(nums []int) (sum, avg int) {
	return Sum(nums), Sum(nums) / len(nums)
}
