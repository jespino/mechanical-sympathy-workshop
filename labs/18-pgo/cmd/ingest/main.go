// ingest runs the lab's workload as a real program, so you can see how the
// go command picks up a default.pgo file next to a main package:
//
//	go run ./18-pgo/cmd/ingest -cpuprofile /tmp/ingest.pprof
//	cp /tmp/ingest.pprof 18-pgo/cmd/ingest/default.pgo
//	go build -o /tmp/ingest ./18-pgo/cmd/ingest   # now built with PGO
//	go version -m /tmp/ingest | grep pgo
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"runtime/pprof"
	"time"

	pgo "labs/18-pgo"
)

func main() {
	cpuprofile := flag.String("cpuprofile", "", "write a CPU profile to this file")
	rounds := flag.Int("rounds", 500, "how many times to ingest the log")
	flag.Parse()

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Fatal(err)
		}
		if err := pprof.StartCPUProfile(f); err != nil {
			log.Fatal(err)
		}
		defer pprof.StopCPUProfile()
	}

	// The observer is chosen at run time, as if from a config file.
	var obs pgo.Observer = &pgo.Histogram{}
	if os.Getenv("INGEST_COUNT_ONLY") != "" {
		obs = &pgo.Counter{}
	}

	data := pgo.GenerateLog(100_000)
	a := pgo.NewAggregator(obs)
	start := time.Now()
	lines := 0
	for range *rounds {
		n, err := a.Ingest(data)
		if err != nil {
			log.Fatal(err)
		}
		lines += n
	}
	elapsed := time.Since(start)
	fmt.Printf("ingested %d lines in %v (%.1f ns/line)\n",
		lines, elapsed.Round(time.Millisecond), float64(elapsed.Nanoseconds())/float64(lines))
}
