//go:build solution

package inlining

import "strings"

// priceAfter is small enough to inline into TotalAfter.
func priceAfter(cents int, coupon string) int {
	if coupon == "" {
		return max(cents, 0)
	}
	return priceWithCoupon(cents, coupon)
}

// priceWithCoupon keeps normalization and discount rules out of the wrapper.
func priceWithCoupon(cents int, coupon string) int {
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

func TotalAfter(items []Item) int {
	var total int
	for _, item := range items {
		total += priceAfter(item.PriceCents, item.Coupon)
	}
	return total
}
