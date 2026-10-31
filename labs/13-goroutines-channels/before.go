// Package goroutines is the lab for module 13: goroutines, channels and the
// scheduler.
//
// before.go holds the naive implementations. Don't edit this file: it is the
// baseline that after.go is benchmarked against.
package goroutines

import (
	"runtime"
	"sync"
)

// salePrice applies a 10% discount in cents, rounding the discount down.
func salePrice(cents uint64) uint64 { return cents - cents/10 }

// TotalPricesBefore returns the sum of salePrice(x) for every item, using GOMAXPROCS
// worker goroutines fed through an unbuffered channel, one item per send.
func TotalPricesBefore(items []uint64) uint64 {
	workers := runtime.GOMAXPROCS(0)
	jobs := make(chan uint64)
	partials := make(chan uint64, workers)

	for range workers {
		go func() {
			var sum uint64
			for x := range jobs {
				sum += salePrice(x)
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

// DiscountPricesBefore returns salePrice(x) for every item. It starts one goroutine per item:
// goroutines are cheap, aren't they?
func DiscountPricesBefore(items []uint64) []uint64 {
	out := make([]uint64, len(items))
	var wg sync.WaitGroup
	for i, x := range items {
		wg.Go(func() {
			out[i] = salePrice(x)
		})
	}
	wg.Wait()
	return out
}
