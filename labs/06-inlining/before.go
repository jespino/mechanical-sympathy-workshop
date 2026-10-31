// Package inlining is the lab for module 06: inlining.
// before.go is the baseline. Do not edit it.
package inlining

import "strings"

// Item is one cart item. Prices are in cents; negative prices are treated as zero.
type Item struct {
	PriceCents int
	Coupon     string
}

// priceBefore handles both the common no-coupon case and coupon processing.
func priceBefore(cents int, coupon string) int {
	if coupon == "" {
		return max(cents, 0)
	}
	cents = max(cents, 0)
	coupon = strings.ToUpper(strings.TrimSpace(coupon))
	switch coupon {
	case "SAVE10":
		return cents - cents/10
	case "SAVE20":
		return cents - cents/5
	case "HALF":
		return cents / 2
	default:
		return cents
	}
}

// TotalBefore adds the final price of every item.
func TotalBefore(items []Item) int {
	var total int
	for _, item := range items {
		total += priceBefore(item.PriceCents, item.Coupon)
	}
	return total
}
