package contention

import (
	"runtime"
	"sync/atomic"
)

// This file holds the intermediate steps between before.go and the solution,
// so the benchmarks can show the whole progression. Don't edit it.

// AtomicCounter replaces the mutex with a single atomic add. No lock, no
// parking goroutines... but still one cache line that every core writes to.
type AtomicCounter struct {
	n atomic.Int64
}

// NewAtomicCounter returns a zero counter.
func NewAtomicCounter() *AtomicCounter { return &AtomicCounter{} }

// Inc adds one to the counter.
func (c *AtomicCounter) Inc(worker int) { c.n.Add(1) }

// Load returns the current value.
func (c *AtomicCounter) Load() int64 { return c.n.Load() }

// ShardedCounter gives every worker its own slot, so no two workers ever
// write the same variable. But the slots are 8 bytes each, packed next to
// each other: eight of them share one 64-byte cache line.
type ShardedCounter struct {
	shards []atomic.Int64
	mask   int
}

// NewShardedCounter returns a zero counter with one shard per P (rounded up
// to a power of two).
func NewShardedCounter() *ShardedCounter {
	n := numShards()
	return &ShardedCounter{shards: make([]atomic.Int64, n), mask: n - 1}
}

// Inc adds one to the caller's shard.
func (c *ShardedCounter) Inc(worker int) { c.shards[worker&c.mask].Add(1) }

// Load sums all the shards.
func (c *ShardedCounter) Load() int64 {
	var sum int64
	for i := range c.shards {
		sum += c.shards[i].Load()
	}
	return sum
}

// numShards returns GOMAXPROCS rounded up to a power of two.
func numShards() int {
	n := 1
	for n < runtime.GOMAXPROCS(0) {
		n <<= 1
	}
	return n
}
