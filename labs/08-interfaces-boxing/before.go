// Package ifaces is the lab for module 08: interfaces and boxing.
// before.go is the baseline. Do not edit it.
package ifaces

// CopyPricesBefore stores prices in an unnecessarily general slice of any.
func CopyPricesBefore(prices []int) []any {
	out := make([]any, len(prices))
	for i, price := range prices {
		out[i] = price
	}
	return out
}

// PriceFilter decides which product prices to keep.
type PriceFilter interface {
	Keep(price int) bool
}

// Budget keeps prices at or below the customer's spending limit, in cents.
type Budget struct{ MaxCents int }

func (b *Budget) Keep(price int) bool { return price <= b.MaxCents }

// CountAffordableBefore calls the filter once for every price.
//
//go:noinline
func CountAffordableBefore(prices []int, budget PriceFilter) int {
	var count int
	for _, price := range prices {
		if budget.Keep(price) {
			count++
		}
	}
	return count
}
