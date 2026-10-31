//go:build !solution

package contention

import "sync"

// This is your file. It starts as a copy of before.go: make Inc scale with
// the number of cores, then run the benchmarks to compare.
// The reference solution is in solution.go (run with -tags solution).
//
// TODO:
//  1. Replace the single mutex-protected int64 with one counter per shard
//     (look at ShardedCounter in steps.go, and use numShards()).
//  2. Benchmark it: ShardedCounter barely beats AtomicCounter. Why?
//  3. Pad every shard to a full cache line (64 bytes on amd64) so no two
//     shards share one, and benchmark again.

// CounterAfter is your version of the counter.
type CounterAfter struct {
	mu sync.Mutex
	n  int64
}

// NewCounterAfter returns a zero counter.
func NewCounterAfter() *CounterAfter { return &CounterAfter{} }

// Inc adds one to the counter.
func (c *CounterAfter) Inc(worker int) {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
}

// Load returns the current value.
func (c *CounterAfter) Load() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}
