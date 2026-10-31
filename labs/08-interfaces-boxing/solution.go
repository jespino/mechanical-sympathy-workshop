//go:build solution

package ifaces

// CopyPricesAfter keeps prices as ints, so no element needs boxing.
func CopyPricesAfter(prices []int) []int {
	out := make([]int, len(prices))
	for i, price := range prices {
		out[i] = price
	}
	return out
}

// CountAffordableAfter knows the concrete receiver type, allowing Keep to inline.
//
//go:noinline
func CountAffordableAfter(prices []int, budget *Budget) int {
	var count int
	for _, price := range prices {
		if budget.Keep(price) {
			count++
		}
	}
	return count
}
