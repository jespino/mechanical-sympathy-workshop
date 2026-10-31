// Package structlayout is the lab for module 10: struct layout and size
// classes.
//
// before.go holds the naive implementations. Don't edit this file: it is the
// baseline that after.go is benchmarked against.
package structlayout

// OrderBefore has its fields in "logical" order: each flag sits next to
// the value it describes. The compiler keeps that order, so every small field
// in front of an 8-byte field drags padding along with it.
type OrderBefore struct {
	Paid          bool
	SubtotalCents int64
	Items         uint16
	ShippingCents int64
	Delivered     bool
	DiscountCents int64
	ID            uint32
}

// CreateOrdersBefore allocates n orders, one heap object each.
func CreateOrdersBefore(n int) []*OrderBefore {
	out := make([]*OrderBefore, n)
	for i := range out {
		out[i] = &OrderBefore{
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

// FillOrdersBefore fills ps with the same orders CreateOrdersBefore creates, but
// stored by value in one contiguous slice.
func FillOrdersBefore(ps []OrderBefore) {
	for i := range ps {
		ps[i] = OrderBefore{
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

// TotalPaidBefore adds subtotal and shipping, minus discount, for paid orders.
func TotalPaidBefore(ps []OrderBefore) int64 {
	var sum int64
	for i := range ps {
		p := &ps[i]
		if p.Paid {
			sum += p.SubtotalCents + p.ShippingCents - p.DiscountCents
		}
	}
	return sum
}
