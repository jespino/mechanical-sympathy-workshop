//go:build solution

package contention

import "sync/atomic"

// cacheLine is the cache line size on amd64 and most arm64 cores. The
// standard library uses the same value: internal/cpu.CacheLinePadSize is 64
// on amd64 (golang.org/x/sys/cpu.CacheLinePad exports it).
const cacheLine = 64

// paddedShard fills a whole cache line, so two shards can never share one.
type paddedShard struct {
	n atomic.Int64
	_ [cacheLine - 8]byte
}

// CounterAfter is sharded *and* padded: every worker increments a cache line
// that no other core writes to, so the line stays in its core's L1 cache.
type CounterAfter struct {
	shards []paddedShard
	mask   int
}

// NewCounterAfter returns a zero counter with one shard per P (rounded up to
// a power of two).
func NewCounterAfter() *CounterAfter {
	n := numShards()
	return &CounterAfter{shards: make([]paddedShard, n), mask: n - 1}
}

// Inc adds one to the caller's shard.
func (c *CounterAfter) Inc(worker int) { c.shards[worker&c.mask].n.Add(1) }

// Load sums all the shards. Reads are rare, so it's fine that they are now
// O(shards) instead of O(1).
func (c *CounterAfter) Load() int64 {
	var sum int64
	for i := range c.shards {
		sum += c.shards[i].n.Load()
	}
	return sum
}
