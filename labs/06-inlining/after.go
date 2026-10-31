//go:build !solution

package inlining

import "strings"

// This is your file. It starts with the same behavior as before.go.
// TODO: Keep priceAfter small enough to inline. Handle the empty coupon here
// and move the remaining coupon processing into a priceWithCoupon helper.
// Keep TotalAfter unchanged. Check the compiler's decision with:
//
//     go build -gcflags=-m=2 ./06-inlining/ 2>&1 | grep price

func priceAfter(cents int, coupon string) int {
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

func TotalAfter(items []Item) int {
	var total int
	for _, item := range items {
		total += priceAfter(item.PriceCents, item.Coupon)
	}
	return total
}
