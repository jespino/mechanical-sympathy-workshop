//go:build !solution

package bce

// TODO: Check the input lengths once, then re-slice all four slices to n.
// Use range over dst so the compiler can prove each index is safe.
// Remember: re-slicing checks capacity, not the original length.
func LineTotalsAfter(dst, prices, quantities, discounts []int, n int) {
	for i := 0; i < n; i++ {
		dst[i] = prices[i]*quantities[i] - discounts[i]
	}
}
