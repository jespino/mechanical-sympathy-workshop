// gctune keeps a pointer-heavy store alive and allocates a fixed amount of
// short-lived garbage next to it, then reports how many GC cycles that took,
// how much CPU the GC burned and the peak memory of the process.
//
// Run it with different GOGC / GOMEMLIMIT values to see the trade-off:
//
//	GOGC=50 go run ./12-gc-pressure/cmd/gctune
//	GOGC=off GOMEMLIMIT=600MiB go run ./12-gc-pressure/cmd/gctune
//
// -store=after uses your StoreAfter instead (add -tags solution for the
// reference one).
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"runtime/metrics"
	"syscall"
	"time"

	gcpressure "labs/12-gc-pressure"
)

var sink []byte

func main() {
	which := flag.String("store", "before", "which store to keep alive: before or after")
	flag.Parse()
	var store any
	switch *which {
	case "before":
		store = gcpressure.NewStoreBefore(1_000_000)
	case "after":
		store = gcpressure.NewStoreAfter(1_000_000)
	default:
		fmt.Fprintln(os.Stderr, "unknown -store", *which)
		os.Exit(2)
	}
	runtime.GC()

	startGC := gcCPUSeconds()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	startNum := ms.NumGC

	start := time.Now()
	const total = 8 << 30 // allocate 8 GiB of garbage, 16 KiB at a time
	for range total / (16 << 10) {
		buf := make([]byte, 16<<10)
		for i := 0; i < len(buf); i += 4096 {
			buf[i] = 1 // touch every page, like real code would
		}
		sink = buf
	}
	elapsed := time.Since(start)

	runtime.ReadMemStats(&ms)
	var ru syscall.Rusage
	syscall.Getrusage(syscall.RUSAGE_SELF, &ru)

	fmt.Printf("store=%-6s GOGC=%-5s GOMEMLIMIT=%-8s  GCs=%4d  GC CPU=%6.2fs  wall=%5.2fs  peak RSS=%4d MiB\n",
		*which, env("GOGC", "100"), env("GOMEMLIMIT", "off"),
		ms.NumGC-startNum, gcCPUSeconds()-startGC, elapsed.Seconds(), ru.Maxrss>>10)
	runtime.KeepAlive(store)
}

func env(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func gcCPUSeconds() float64 {
	s := []metrics.Sample{{Name: "/cpu/classes/gc/total:cpu-seconds"}}
	metrics.Read(s)
	return s[0].Value.Float64()
}
