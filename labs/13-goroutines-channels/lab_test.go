package goroutines

import (
	"slices"
	"testing"
)

const size = 100_000

var priceSink uint64
var pricesSink []uint64

func testItems(n int) []uint64 {
	items := make([]uint64, n)
	for i := range items {
		items[i] = 100 + uint64(i%10_000)
	}
	return items
}

func TestSameResults(t *testing.T) {
	for _, n := range []int{0, 1, 999, 1024, 1025, 10_000} {
		items := testItems(n)
		if s, b, a := totalSequential(items), TotalPricesBefore(items), TotalPricesAfter(items); s != b || s != a {
			t.Errorf("TotalPrices(n=%d): sequential=%d before=%d after=%d", n, s, b, a)
		}
		if b, a := DiscountPricesBefore(items), DiscountPricesAfter(items); !slices.Equal(b, a) {
			t.Errorf("DiscountPrices(n=%d): after differs from before", n)
		}
	}
}

// totalSequential is the reference point: one goroutine, no channels.
func totalSequential(items []uint64) uint64 {
	var sum uint64
	for _, x := range items {
		sum += salePrice(x)
	}
	return sum
}

func BenchmarkTotalPrices(b *testing.B) {
	items := testItems(size)
	for _, impl := range []struct {
		name string
		fn   func([]uint64) uint64
	}{{"before", TotalPricesBefore}, {"after", TotalPricesAfter}, {"sequential", totalSequential}} {
		b.Run("impl="+impl.name, func(b *testing.B) {
			for b.Loop() {
				priceSink = impl.fn(items)
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*size), "ns/item")
		})
	}
}

func BenchmarkDiscountPrices(b *testing.B) {
	items := testItems(size)
	for _, impl := range []struct {
		name string
		fn   func([]uint64) []uint64
	}{{"before", DiscountPricesBefore}, {"after", DiscountPricesAfter}} {
		b.Run("impl="+impl.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				pricesSink = impl.fn(items)
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*size), "ns/item")
		})
	}
}

func TestSalePrices(t *testing.T) {
	prices := []uint64{100, 200, 1000}
	if got := DiscountPricesAfter(prices); !slices.Equal(got, []uint64{90, 180, 900}) {
		t.Fatalf("sale prices: %v", got)
	}
	if got := TotalPricesAfter(prices); got != 1170 {
		t.Fatalf("total: got %d, want 1170", got)
	}
	if !slices.Equal(prices, []uint64{100, 200, 1000}) {
		t.Fatal("changed input prices")
	}
}
