package benchmarking

// before_test.go holds three broken benchmarks. Each one compiles, runs and
// prints a believable number, and each number is wrong. Don't edit this file:
// it is the baseline that after_test.go is compared against.

import (
	"slices"
	"testing"
)

// benchPopCountBefore uses the classic b.N loop and throws the result away.
func benchPopCountBefore(b *testing.B) {
	for i := 0; i < b.N; i++ {
		PopCount(uint64(i))
	}
}

// benchMixBefore uses b.Loop, which keeps the result alive... but the input
// is a constant.
func benchMixBefore(b *testing.B) {
	for b.Loop() {
		Mix(42)
	}
}

// benchSortBefore sorts the same slice over and over.
func benchSortBefore(b *testing.B) {
	data := randomInts(10_000)
	for b.Loop() {
		slices.Sort(data)
	}
}
