package ifaces

import (
	"slices"
	"testing"
)

// Prices exceed 255 so the allocation exercise doesn't use Go's shared
// storage for small boxed integers.
func testPrices(n int) []int {
	prices := make([]int, n)
	for i := range prices {
		prices[i] = 1000 + i%1000
	}
	return prices
}

func TestCopyPrices(t *testing.T) {
	for _, prices := range [][]int{nil, {}, {0, 1, 255, 256, 1000, -1}, testPrices(1000)} {
		original := slices.Clone(prices)
		before, after := CopyPricesBefore(prices), CopyPricesAfter(prices)
		if len(before) != len(prices) || len(after) != len(prices) {
			t.Fatal("copy has wrong length")
		}
		for i, want := range prices {
			// Comparing with an int works with both the starter's []any
			// and the solution's []int, so the test supports the type change.
			if before[i] != want || after[i] != want {
				t.Fatalf("price %d: before=%v after=%v want=%d", i, before[i], after[i], want)
			}
		}
		if len(after) > 0 {
			after[0] = 9999
			before[0] = 8888
		}
		if !slices.Equal(prices, original) {
			t.Fatal("copy shares or changes input storage")
		}
	}
}

func TestCountAffordable(t *testing.T) {
	cases := []struct {
		prices []int
		limit  int
		want   int
	}{
		{nil, 1000, 0},
		{[]int{500, 1000, 1500}, 1000, 2},
		{[]int{500, 1000, 1500}, 499, 0},
		{[]int{500, 1000, 1500}, 1500, 3},
		{[]int{0, 1000, 1000}, 1000, 3},
	}
	for _, tc := range cases {
		budget := &Budget{MaxCents: tc.limit}
		if b, a := CountAffordableBefore(tc.prices, budget), CountAffordableAfter(tc.prices, budget); b != tc.want || a != tc.want {
			t.Fatalf("limit=%d: before=%d after=%d want=%d", tc.limit, b, a, tc.want)
		}
	}
}

var priceSink int
var countSink int
var budgetLimit = 1500

func BenchmarkCopyPrices(b *testing.B) {
	prices := testPrices(1000)
	b.Run("impl=before", func(b *testing.B) {
		var last int
		for b.Loop() {
			out := CopyPricesBefore(prices)
			last = any(out[len(out)-1]).(int)
		}
		priceSink = last
	})
	b.Run("impl=after", func(b *testing.B) {
		var last int
		for b.Loop() {
			out := CopyPricesAfter(prices)
			// This temporary conversion works with either result type
			// and does not escape. The compiler avoids its allocation.
			last = any(out[len(out)-1]).(int)
		}
		priceSink = last
	})
}

// Keep both batch functions uninlined so the benchmark measures the
// difference between indirect and direct Keep calls inside their loops.
func BenchmarkCountAffordable(b *testing.B) {
	prices := testPrices(10_000)
	budget := &Budget{MaxCents: budgetLimit}
	b.Run("impl=before", func(b *testing.B) {
		for b.Loop() {
			countSink = CountAffordableBefore(prices, budget)
		}
	})
	b.Run("impl=after", func(b *testing.B) {
		for b.Loop() {
			countSink = CountAffordableAfter(prices, budget)
		}
	})
}
