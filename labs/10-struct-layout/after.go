//go:build !solution

package structlayout

// This is your file. It starts as a copy of before.go: change it so that
// OrderAfter takes less memory, then run the benchmarks to compare against
// before.go. The reference solution is in solution.go (run with -tags
// solution).

// OrderAfter holds the same fields as OrderBefore.
// TODO: unsafe.Sizeof(OrderAfter{}) is 56. Reorder the fields (largest
// alignment first) so the padding disappears. Don't add, remove or retype
// fields: the test checks the values are the same.
type OrderAfter struct {
	Paid          bool
	SubtotalCents int64
	Items         uint16
	ShippingCents int64
	Delivered     bool
	DiscountCents int64
	ID            uint32
}

// CreateOrdersAfter allocates n orders, one heap object each.
func CreateOrdersAfter(n int) []*OrderAfter {
	out := make([]*OrderAfter, n)
	for i := range out {
		out[i] = &OrderAfter{
			Paid:          i%3 != 0,
			Delivered:     true,
			Items:         uint16(i % 7),
			SubtotalCents: int64(i),
			ShippingCents: int64(i) / 2,
			DiscountCents: int64(i) / 4,
			ID:            uint32(i),
		}
	}
	return out
}

// FillOrdersAfter fills ps with the same orders CreateOrdersAfter creates, but stored
// by value in one contiguous slice.
func FillOrdersAfter(ps []OrderAfter) {
	for i := range ps {
		ps[i] = OrderAfter{
			Paid:          i%3 != 0,
			Delivered:     true,
			Items:         uint16(i % 7),
			SubtotalCents: int64(i),
			ShippingCents: int64(i) / 2,
			DiscountCents: int64(i) / 4,
			ID:            uint32(i),
		}
	}
}

// TotalPaidAfter adds subtotal and shipping, minus discount, for paid orders.
func TotalPaidAfter(ps []OrderAfter) int64 {
	var sum int64
	for i := range ps {
		p := &ps[i]
		if p.Paid {
			sum += p.SubtotalCents + p.ShippingCents - p.DiscountCents
		}
	}
	return sum
}
