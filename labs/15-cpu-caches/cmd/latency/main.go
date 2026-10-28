// Command latency measures the average time of one dependent memory load for
// working sets of increasing size. Each load's address comes from the
// previous load (pointer chasing through a random cycle), so neither
// out-of-order execution nor the prefetcher can hide the latency: you see
// the L1, L2, L3 and DRAM steps directly.
package main

import (
	"fmt"
	"math/rand/v2"
	"time"
)

// node is padded to a full 64-byte cache line, so every hop touches a new
// line.
type node struct {
	next int
	_    [56]byte
}

func chase(size int) float64 {
	n := size / 64
	nodes := make([]node, n)
	// Build one random cycle through all nodes (Sattolo's algorithm).
	perm := make([]int, n)
	for i := range perm {
		perm[i] = i
	}
	r := rand.New(rand.NewPCG(42, 42))
	for i := n - 1; i > 0; i-- {
		j := r.IntN(i)
		perm[i], perm[j] = perm[j], perm[i]
	}
	for i := range n {
		nodes[perm[i]].next = perm[(i+1)%n]
	}

	const hops = 20_000_000
	p := 0
	for range n { // warm up: touch every node once
		p = nodes[p].next
	}
	start := time.Now()
	for range hops {
		p = nodes[p].next
	}
	elapsed := time.Since(start)
	if p < 0 { // keep p alive
		fmt.Println(p)
	}
	return float64(elapsed.Nanoseconds()) / hops
}

func main() {
	for _, kb := range []int{16, 32, 64, 256, 512, 1024, 2048, 8192, 16384, 32768, 65536, 262144} {
		ns := chase(kb << 10)
		fmt.Printf("working set %7d KB: %6.1f ns per load\n", kb, ns)
	}
}
