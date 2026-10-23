// Package goroutines is the lab for module 16: goroutines, channels and the
// scheduler.
//
// before.go holds the naive implementations. Don't edit this file: it is the
// baseline that after.go is benchmarked against.
package goroutines

import (
	"runtime"
	"sync"
)

// work is the per-item computation: a few rounds of an integer mixer.
// It takes some tens of nanoseconds, which is about the size of many real
// per-item tasks (parse a field, hash a key, update a struct).
func work(x uint64) uint64 {
	for range 16 {
		x ^= x >> 33
		x *= 0xff51afd7ed558ccd
		x ^= x >> 29
	}
	return x
}

// SumBefore returns the sum of work(x) for every item, using GOMAXPROCS
// worker goroutines fed through an unbuffered channel, one item per send.
func SumBefore(items []uint64) uint64 {
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

// MapBefore returns work(x) for every item. It starts one goroutine per item:
// goroutines are cheap, aren't they?
func MapBefore(items []uint64) []uint64 {
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
