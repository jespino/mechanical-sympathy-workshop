//go:build solution

package structlayout

// OrderAfter orders its fields from the largest alignment to the smallest:
// three int64 (24 bytes), a uint32 (4), a uint16 (2) and two bools (2).
// That adds up to exactly 32 bytes with no padding at all, down from 56.
type OrderAfter struct {
	SubtotalCents int64
	ShippingCents int64
	DiscountCents int64
	ID            uint32
	Items         uint16
	Paid          bool
	Delivered     bool
}

// CreateOrdersAfter allocates n orders, one heap object each. Each object is now
// 32 bytes, which is exactly a size class, instead of 56 rounded up to 64.
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

// FillOrdersAfter is unchanged: the struct literal uses field names, so the order
// of the fields in the type doesn't matter here.
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

// TotalPaidAfter is unchanged too. It is faster only because two orders
// now fit in each 64-byte cache line, so the loop reads 43% less memory.
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
