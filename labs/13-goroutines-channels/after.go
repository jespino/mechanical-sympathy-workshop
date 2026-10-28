//go:build !solution

package goroutines

import (
	"runtime"
	"sync"
)

// This is your file. It starts as a copy of before.go: make the per-item
// overhead disappear, then run the benchmarks to compare against before.go.
// The reference solution is in solution.go (run with -tags solution).

// SumAfter returns the sum of work(x) for every item.
// TODO: send chunks of items (a []uint64 sub-slice, ~1000 items) instead of
// one item per channel operation. Sub-slicing doesn't copy anything.
func SumAfter(items []uint64) uint64 {
	workers := runtime.GOMAXPROCS(0)
	jobs := make(chan uint64)
	partials := make(chan uint64, workers)

	for range workers {
		go func() {
			var sum uint64
			for x := range jobs {
				sum += work(x)
			}
			partials <- sum
		}()
	}

	for _, x := range items {
		jobs <- x
	}
	close(jobs)

	var total uint64
	for range workers {
		total += <-partials
	}
	return total
}

// MapAfter returns work(x) for every item.
// TODO: start GOMAXPROCS goroutines, not len(items), and give each one a
// contiguous range of indices to process.
func MapAfter(items []uint64) []uint64 {
	out := make([]uint64, len(items))
	var wg sync.WaitGroup
	for i, x := range items {
		wg.Go(func() {
			out[i] = work(x)
		})
	}
	wg.Wait()
	return out
}
