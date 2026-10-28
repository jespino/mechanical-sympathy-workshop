package contention

import (
	"sync"
	"sync/atomic"
	"testing"
)

var impls = []struct {
	name string
	new  func() Counter
}{
	{"before", func() Counter { return NewCounterBefore() }},
	{"atomic", func() Counter { return NewAtomicCounter() }},
	{"sharded", func() Counter { return NewShardedCounter() }},
	{"after", func() Counter { return NewCounterAfter() }},
}

func TestCount(t *testing.T) {
	const workers, perWorker = 8, 10_000
	for _, impl := range impls {
		c := impl.new()
		var wg sync.WaitGroup
		for w := range workers {
			wg.Go(func() {
				for range perWorker {
					c.Inc(w)
				}
			})
		}
		wg.Wait()
		if got := c.Load(); got != workers*perWorker {
			t.Errorf("%s: Load() = %d, want %d", impl.name, got, workers*perWorker)
		}
	}
}

// BenchmarkInc increments the counter from GOMAXPROCS goroutines at once.
// Run it with -cpu 1,2,4,8,16 to see how each implementation scales.
func BenchmarkInc(b *testing.B) {
	for _, impl := range impls {
		b.Run("impl="+impl.name, func(b *testing.B) {
			c := impl.new()
			var nextWorker atomic.Int64
			b.RunParallel(func(pb *testing.PB) {
				w := int(nextWorker.Add(1) - 1)
				for pb.Next() {
					c.Inc(w)
				}
			})
			if c.Load() != int64(b.N) {
				b.Fatalf("Load() = %d, want %d", c.Load(), b.N)
			}
		})
	}
}
