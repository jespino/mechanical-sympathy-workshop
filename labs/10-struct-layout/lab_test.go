package structlayout

import (
	"testing"
	"unsafe"
)

const (
	ordersN = 10_000    // individually allocated orders
	sumN    = 1_000_000 // 56 MB before, 32 MB after: bigger than the caches
)

func TestSameResults(t *testing.T) {
	t.Logf("sizeof: before=%d after=%d", unsafe.Sizeof(OrderBefore{}), unsafe.Sizeof(OrderAfter{}))
	b, a := CreateOrdersBefore(100), CreateOrdersAfter(100)
	for i := range b {
		pb, pa := b[i], a[i]
		if pb.Paid != pa.Paid || pb.Delivered != pa.Delivered || pb.Items != pa.Items ||
			pb.SubtotalCents != pa.SubtotalCents || pb.ShippingCents != pa.ShippingCents || pb.DiscountCents != pa.DiscountCents || pb.ID != pa.ID {
			t.Fatalf("order %d differs: %+v vs %+v", i, *pb, *pa)
		}
	}
	bs, as := make([]OrderBefore, 1000), make([]OrderAfter, 1000)
	FillOrdersBefore(bs)
	FillOrdersAfter(as)
	if TotalPaidBefore(bs) != TotalPaidAfter(as) {
		t.Error("TotalPaid: after differs from before")
	}
}

// The sinks keep the results alive so the compiler can't stack-allocate them.
var (
	totalPaidSink int64
	sinkBefore    []*OrderBefore
	sinkAfter     []*OrderAfter
)

func BenchmarkCreateOrders(b *testing.B) {
	b.Run("impl=before", func(b *testing.B) {
		for b.Loop() {
			sinkBefore = CreateOrdersBefore(ordersN)
		}
	})
	b.Run("impl=after", func(b *testing.B) {
		for b.Loop() {
			sinkAfter = CreateOrdersAfter(ordersN)
		}
	})
}

func BenchmarkTotalPaid(b *testing.B) {
	b.Run("impl=before", func(b *testing.B) {
		ps := make([]OrderBefore, sumN)
		FillOrdersBefore(ps)
		for b.Loop() {
			totalPaidSink = TotalPaidBefore(ps)
		}
	})
	b.Run("impl=after", func(b *testing.B) {
		ps := make([]OrderAfter, sumN)
		FillOrdersAfter(ps)
		for b.Loop() {
			totalPaidSink = TotalPaidAfter(ps)
		}
	})
}

func TestReceiptTotal(t *testing.T) {
	orders := []OrderAfter{
		{Paid: true, SubtotalCents: 1000, ShippingCents: 200, DiscountCents: 100},
		{Paid: false, SubtotalCents: 9000},
		{Paid: true, SubtotalCents: 500, ShippingCents: 0, DiscountCents: 50},
	}
	if got := TotalPaidAfter(orders); got != 1550 {
		t.Fatalf("got %d cents, want 1550", got)
	}
}
