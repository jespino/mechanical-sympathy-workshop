//go:build solution

package bce

func LineTotalsAfter(dst, prices, quantities, discounts []int, n int) {
	if n <= 0 {
		return
	}
	if n > len(dst) || n > len(prices) || n > len(quantities) || n > len(discounts) {
		panic("LineTotalsAfter: slice shorter than n")
	}
	dst, prices, quantities, discounts = dst[:n], prices[:n], quantities[:n], discounts[:n]
	for i := range dst {
		dst[i] = prices[i]*quantities[i] - discounts[i]
	}
}
