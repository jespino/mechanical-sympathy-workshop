//go:build !solution

package benchmarking

// This is your file. It starts as a copy of before_test.go: fix each
// benchmark so that it measures what its name says, then compare with
// `make bench LAB=01`. The reference solution is in solution_test.go
// (run with -tags solution).

import (
	"slices"
	"testing"
)

// benchPopCountAfter should measure the cost of one PopCount.
// TODO: the result is never used, and the compiler knows it. Switch to
// b.Loop, which keeps the results of calls in its body alive.
func benchPopCountAfter(b *testing.B) {
	for i := 0; i < b.N; i++ {
		PopCount(uint64(i))
	}
}

// benchMixAfter should measure the cost of one Mix.
// TODO: the compiler can inline Mix and compute Mix(42) at compile time.
// Give it an input it can't know in advance.
func benchMixAfter(b *testing.B) {
	for b.Loop() {
		Mix(42)
	}
}

// benchSortAfter should measure the cost of sorting 10,000 random ints.
// TODO: after the first iteration, data is already sorted. Sort a fresh
// copy of the random data in every iteration.
func benchSortAfter(b *testing.B) {
	data := randomInts(10_000)
	for b.Loop() {
		slices.Sort(data)
	}
}
