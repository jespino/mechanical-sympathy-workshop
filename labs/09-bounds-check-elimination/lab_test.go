package bce

import (
	"slices"
	"testing"
)

const size = 4096

func testReceipt(n int) (prices, quantities, discounts []int) {
	prices, quantities, discounts = make([]int, n), make([]int, n), make([]int, n)
	for i := range n {
		prices[i], quantities[i], discounts[i] = 100+i%1000, 1+i%5, i%20
	}
	return
}

func TestLineTotals(t *testing.T) {
	for _, n := range []int{0, 1, 100} {
		prices, quantities, discounts := testReceipt(n + 2)
		before, after := make([]int, n+2), make([]int, n+2)
		for i := range before {
			before[i], after[i] = -1, -1
		}
		LineTotalsBefore(before, prices, quantities, discounts, n)
		LineTotalsAfter(after, prices, quantities, discounts, n)
		if !slices.Equal(before, after) {
			t.Fatalf("n=%d: %v != %v", n, before, after)
		}
		if after[n] != -1 {
			t.Fatal("wrote beyond requested lines")
		}
	}
	out := make([]int, 2)
	LineTotalsAfter(out, []int{100, 200}, []int{2, 3}, []int{10, 0}, 2)
	if !slices.Equal(out, []int{190, 600}) {
		t.Fatalf("receipt: got %v", out)
	}
	LineTotalsAfter(nil, nil, nil, nil, -1)
}

func TestShortSlicesPanic(t *testing.T) {
	for _, short := range []string{"dst", "prices", "quantities", "discounts"} {
		t.Run(short, func(t *testing.T) {
			dst, prices, quantities, discounts := make([]int, 10), make([]int, 10), make([]int, 10), make([]int, 10)
			// Spare capacity must not make a short input acceptable.
			switch short {
			case "dst":
				dst = dst[:5]
			case "prices":
				prices = prices[:5]
			case "quantities":
				quantities = quantities[:5]
			case "discounts":
				discounts = discounts[:5]
			}
			defer func() {
				if recover() == nil {
					t.Fatal("accepted a short slice with spare capacity")
				}
			}()
			LineTotalsAfter(dst, prices, quantities, discounts, 10)
		})
	}
}

var totalSink int

func BenchmarkLineTotals(b *testing.B) {
	prices, quantities, discounts := testReceipt(size)
	dst := make([]int, size)
	for _, impl := range []struct {
		name string
		fn   func([]int, []int, []int, []int, int)
	}{{"before", LineTotalsBefore}, {"after", LineTotalsAfter}} {
		b.Run("impl="+impl.name, func(b *testing.B) {
			for b.Loop() {
				impl.fn(dst, prices, quantities, discounts, size)
			}
			totalSink = dst[len(dst)-1]
		})
	}
}
