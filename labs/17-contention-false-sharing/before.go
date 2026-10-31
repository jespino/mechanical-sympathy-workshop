// Package contention is the lab for module 17: contention and false sharing.
//
// before.go holds the naive implementation. Don't edit this file: it is the
// baseline that after.go is benchmarked against.
package contention

import "sync"

// Counter is a statistics counter hammered by many goroutines at once, like
// a requests-served counter in a server. Writes are very frequent, reads are
// rare (a metrics scrape every few seconds).
//
// worker identifies the calling goroutine (0, 1, 2, ...). Implementations
// are free to ignore it.
type Counter interface {
	Inc(worker int)
	Load() int64
}

// CounterBefore protects a single int64 with a mutex. Every Inc, from every
// core, fights for the same lock and the same cache line.
type CounterBefore struct {
	mu sync.Mutex
	n  int64
}

// NewCounterBefore returns a zero counter.
func NewCounterBefore() *CounterBefore { return &CounterBefore{} }

// Inc adds one to the counter.
func (c *CounterBefore) Inc(worker int) {
	c.mu.Lock()
	c.n++
	c.mu.Unlock()
}

// Load returns the current value.
func (c *CounterBefore) Load() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}
