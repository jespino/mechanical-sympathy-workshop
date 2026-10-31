// victim shows how long an object survives in a sync.Pool across garbage
// collections: the primary cache becomes the victim cache at the next GC,
// and the victim cache is dropped at the GC after that.
package main

import (
	"fmt"
	"runtime"
	"sync"
)

func main() {
	// Get only looks at the private slot of the current P's victim cache.
	// With one P the goroutine can't move to another P during runtime.GC(),
	// so the result doesn't depend on scheduling.
	runtime.GOMAXPROCS(1)

	var pool sync.Pool // no New: Get returns nil when the pool is empty

	for gcs := range 4 {
		pool.Put(new([1024]byte))
		for range gcs {
			runtime.GC()
		}
		got := pool.Get() != nil
		fmt.Printf("Put, %d x runtime.GC(), Get -> found=%v\n", gcs, got)
	}
}
