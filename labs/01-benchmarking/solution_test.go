//go:build solution

package benchmarking

import (
	"slices"
	"testing"
)

// benchPopCountAfter uses b.Loop: the compiler keeps the result of each
// PopCount call alive, so the work can't be thrown away.
func benchPopCountAfter(b *testing.B) {
	x := uint64(0)
	for b.Loop() {
		PopCount(x)
		x++
	}
}

// benchMixAfter feeds Mix a different value in every iteration, so the
// result can't be computed at compile time.
func benchMixAfter(b *testing.B) {
	x := uint64(42)
	for b.Loop() {
		Mix(x)
		x++
	}
}

// benchSortAfter sorts a fresh copy of the random data in every iteration.
// The copy is measured too, but it costs well under 1% of the sort (check it
// with a benchmark that only copies).
func benchSortAfter(b *testing.B) {
	data := randomInts(10_000)
	work := make([]int, len(data))
	for b.Loop() {
		copy(work, data)
		slices.Sort(work)
	}
}
