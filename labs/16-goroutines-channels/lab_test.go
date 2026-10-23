package goroutines

import (
	"slices"
	"testing"
)

const size = 100_000

func testItems(n int) []uint64 {
	items := make([]uint64, n)
	for i := range items {
		items[i] = uint64(i) * 0x9e3779b97f4a7c15
	}
	return items
}

func TestSameResults(t *testing.T) {
	for _, n := range []int{0, 1, 999, 1024, 1025, 10_000} {
		items := testItems(n)
		if s, b, a := sumSequential(items), SumBefore(items), SumAfter(items); s != b || s != a {
			t.Errorf("Sum(n=%d): sequential=%d before=%d after=%d", n, s, b, a)
		}
		if b, a := MapBefore(items), MapAfter(items); !slices.Equal(b, a) {
			t.Errorf("Map(n=%d): after differs from before", n)
		}
	}
}

// sumSequential is the reference point: one goroutine, no channels.
func sumSequential(items []uint64) uint64 {
	var sum uint64
	for _, x := range items {
		sum += work(x)
	}
	return sum
}

func BenchmarkSum(b *testing.B) {
	items := testItems(size)
	for _, impl := range []struct {
		name string
		fn   func([]uint64) uint64
	}{{"before", SumBefore}, {"after", SumAfter}, {"sequential", sumSequential}} {
		b.Run("impl="+impl.name, func(b *testing.B) {
			for b.Loop() {
				impl.fn(items)
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*size), "ns/item")
		})
	}
}

func BenchmarkMap(b *testing.B) {
	items := testItems(size)
	for _, impl := range []struct {
		name string
		fn   func([]uint64) []uint64
	}{{"before", MapBefore}, {"after", MapAfter}} {
		b.Run("impl="+impl.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				impl.fn(items)
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*size), "ns/item")
		})
	}
}
