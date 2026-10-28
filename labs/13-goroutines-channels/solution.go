//go:build solution

package goroutines

import (
	"runtime"
	"sync"
)

// chunkSize is how many items travel in one channel operation. A channel
// send/receive costs on the order of 100ns under contention; with 1024 items
// per send that becomes ~0.1ns per item.
const chunkSize = 1024

// SumAfter feeds the workers chunks of items instead of single items.
// items[i:end] is just a new slice header pointing into the same array:
// nothing is copied.
func SumAfter(items []uint64) uint64 {
	workers := runtime.GOMAXPROCS(0)
	jobs := make(chan []uint64, workers)
	partials := make(chan uint64, workers)

	for range workers {
		go func() {
			var sum uint64
			for chunk := range jobs {
				for _, x := range chunk {
					sum += work(x)
				}
			}
			partials <- sum
		}()
	}

	for i := 0; i < len(items); i += chunkSize {
		jobs <- items[i:min(i+chunkSize, len(items))]
	}
	close(jobs)

	var total uint64
	for range workers {
		total += <-partials
	}
	return total
}

// MapAfter splits the index range into one contiguous block per P. Each
// goroutine writes its own part of out, so no synchronization is needed
// beyond the final Wait.
func MapAfter(items []uint64) []uint64 {
	out := make([]uint64, len(items))
	workers := runtime.GOMAXPROCS(0)
	per := (len(items) + workers - 1) / workers
	var wg sync.WaitGroup
	for start := 0; start < len(items); start += per {
		end := min(start+per, len(items))
		wg.Go(func() {
			for i := start; i < end; i++ {
				out[i] = work(items[i])
			}
		})
	}
	wg.Wait()
	return out
}
