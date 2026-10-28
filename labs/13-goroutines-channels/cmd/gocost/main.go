// gocost measures what a goroutine costs: the time to start one, and the
// memory it holds while it is blocked (its stack, plus its g struct and
// runtime bookkeeping).
package main

import (
	"fmt"
	"runtime"
	"runtime/metrics"
	"sync"
	"time"
)

const n = 100_000

func main() {
	before := readMem()

	release := make(chan struct{})
	var started sync.WaitGroup
	start := time.Now()
	for range n {
		started.Add(1)
		go func() {
			started.Done()
			<-release // stay alive, blocked, so we can measure the memory
		}()
	}
	started.Wait()
	elapsed := time.Since(start)

	after := readMem()
	fmt.Printf("started %d goroutines in %v (%.0f ns each)\n", n, elapsed.Round(time.Millisecond), float64(elapsed.Nanoseconds())/n)
	fmt.Printf("goroutine stacks: %6.0f bytes per goroutine\n", float64(after.stacks-before.stacks)/n)
	fmt.Printf("total from OS:    %6.0f bytes per goroutine\n", float64(after.total-before.total)/n)
	close(release)
}

type mem struct{ stacks, total uint64 }

func readMem() mem {
	s := []metrics.Sample{
		{Name: "/memory/classes/heap/stacks:bytes"},
		{Name: "/memory/classes/total:bytes"},
	}
	runtime.GC()
	metrics.Read(s)
	return mem{s[0].Value.Uint64(), s[1].Value.Uint64()}
}
