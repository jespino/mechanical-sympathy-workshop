package bce

import (
	"slices"
	"testing"
)

const (
	ordersN = 4096 // 4096 records × 16 B = 64 KB: fits in L2, so we measure the CPU, not memory
	blendN  = 4096 // 4 slices × 16 KB: fits in L1/L2
)

func testOrders(n int) []byte {
	b := make([]byte, n*RecordSize+5) // + a partial record at the end
	for i := range b {
		b[i] = byte(i*7 + i>>8)
	}
	return b
}

func testVectors(n int) (a, b, c []uint32) {
	a, b, c = make([]uint32, n), make([]uint32, n), make([]uint32, n)
	for i := range n {
		a[i], b[i], c[i] = uint32(i), uint32(i*i), uint32(n-i)
	}
	return a, b, c
}

func TestSameResults(t *testing.T) {
	for _, n := range []int{0, 1, 100} {
		b := testOrders(n)
		if SumOrdersBefore(b) != SumOrdersAfter(b) {
			t.Errorf("SumOrders(%d records): after differs from before", n)
		}
	}
	a, b, c := testVectors(1000)
	d1, d2 := make([]uint32, 1000), make([]uint32, 1000)
	BlendBefore(d1, a, b, c, 900)
	BlendAfter(d2, a, b, c, 900)
	if !slices.Equal(d1, d2) {
		t.Error("Blend: after differs from before")
	}
}

func TestBlendShortSlicePanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("BlendAfter accepted n > len(c)")
		}
	}()
	a, b, c := testVectors(10)
	BlendAfter(make([]uint32, 20), a, b, c[:5], 10)
}

var sink uint64

func BenchmarkSumOrders(b *testing.B) {
	orders := testOrders(ordersN)
	for _, impl := range []struct {
		name string
		fn   func([]byte) uint64
	}{{"before", SumOrdersBefore}, {"after", SumOrdersAfter}} {
		b.Run("impl="+impl.name, func(b *testing.B) {
			for b.Loop() {
				sink += impl.fn(orders)
			}
		})
	}
}

func BenchmarkBlend(b *testing.B) {
	a, v, c := testVectors(blendN)
	dst := make([]uint32, blendN)
	for _, impl := range []struct {
		name string
		fn   func(dst, a, b, c []uint32, n int)
	}{{"before", BlendBefore}, {"after", BlendAfter}} {
		b.Run("impl="+impl.name, func(b *testing.B) {
			for b.Loop() {
				impl.fn(dst, a, v, c, blendN)
			}
		})
	}
}
