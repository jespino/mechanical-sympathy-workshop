// Package bce is the lab for module 09: bounds check elimination.
// before.go is the baseline. Do not edit it.
package bce

// LineTotalsBefore computes receipt lines in cents for the first n items.
// A discount applies to the whole line. n <= 0 does nothing; short slices panic.
func LineTotalsBefore(dst, prices, quantities, discounts []int, n int) {
	for i := 0; i < n; i++ {
		dst[i] = prices[i]*quantities[i] - discounts[i]
	}
}
