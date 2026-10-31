package inlining

import "testing"

const size = 10_000

func testCart() []Item {
	items := make([]Item, size)
	coupons := [...]string{"SAVE10", "SAVE20", "HALF", "UNKNOWN"}
	for i := range items {
		items[i].PriceCents = 100 + i%5_000
		if i%100 == 0 {
			items[i].Coupon = coupons[(i/100)%len(coupons)]
		}
	}
	return items
}

func TestPrices(t *testing.T) {
	cases := []struct {
		name   string
		cents  int
		coupon string
		want   int
	}{
		{"no coupon", 1000, "", 1000},
		{"ten percent", 1000, "SAVE10", 900},
		{"twenty percent", 1000, "SAVE20", 800},
		{"half", 1000, "HALF", 500},
		{"lowercase and spaces", 1000, "  save10 \t", 900},
		{"unicode whitespace", 1000, "\u2003half\u2003", 500},
		{"whitespace only", 1000, " \t", 1000},
		{"unknown coupon", 1000, "WELCOME", 1000},
		{"round discount down", 109, "SAVE10", 99},
		{"round half down", 109, "HALF", 54},
		{"free item", 0, "SAVE20", 0},
		{"negative without coupon", -100, "", 0},
		{"negative with coupon", -100, "SAVE20", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, impl := range []struct {
				name string
				fn   func(int, string) int
			}{{"before", priceBefore}, {"after", priceAfter}} {
				if got := impl.fn(tc.cents, tc.coupon); got != tc.want {
					t.Errorf("%s: got %d, want %d", impl.name, got, tc.want)
				}
			}
		})
	}
}

func TestTotals(t *testing.T) {
	for _, items := range [][]Item{nil, {}, {{1000, ""}, {1000, "SAVE10"}, {1000, "HALF"}}, testCart()} {
		var want int
		for _, item := range items {
			want += priceBefore(item.PriceCents, item.Coupon)
		}
		if got := TotalBefore(items); got != want {
			t.Errorf("before: got %d, want %d", got, want)
		}
		if got := TotalAfter(items); got != want {
			t.Errorf("after: got %d, want %d", got, want)
		}
	}
	if got := TotalAfter([]Item{{1000, ""}, {1000, "SAVE10"}, {1000, "HALF"}}); got != 2400 {
		t.Errorf("example cart: got %d, want 2400", got)
	}
}

var totalSink int

func BenchmarkTotal(b *testing.B) {
	items := testCart()
	for _, impl := range []struct {
		name string
		fn   func([]Item) int
	}{{"before", TotalBefore}, {"after", TotalAfter}} {
		b.Run("impl="+impl.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				totalSink = impl.fn(items)
			}
		})
	}
}
