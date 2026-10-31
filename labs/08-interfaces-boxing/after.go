//go:build !solution

package ifaces

// Exercise 1: every element is an int. Change []any to []int in both the
// return type and make call. Keep the loop unchanged.
// Run: make bench LAB=08 BENCH=CopyPrices
func CopyPricesAfter(prices []int) []any {
	out := make([]any, len(prices))
	for i, price := range prices {
		out[i] = price
	}
	return out
}

// Exercise 2: this loop always receives a *Budget. Change its parameter
// type from PriceFilter to *Budget and keep the body unchanged.
// Run: make bench LAB=08 BENCH=CountAffordable
//
//go:noinline
func CountAffordableAfter(prices []int, budget PriceFilter) int {
	var count int
	for _, price := range prices {
		if budget.Keep(price) {
			count++
		}
	}
	return count
}
