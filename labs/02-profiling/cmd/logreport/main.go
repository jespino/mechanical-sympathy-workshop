// logreport builds the endpoint report over and over for a while, so you can
// profile it as a regular program instead of as a benchmark.
//
//	go run ./02-profiling/cmd/logreport -cpuprofile cpu.prof
//	go run ./02-profiling/cmd/logreport -http localhost:6060 -duration 60s
//
// -cpuprofile and -memprofile use runtime/pprof directly. -http serves the
// net/http/pprof endpoints, so you can grab a profile from the running
// process with:
//
//	go tool pprof http://localhost:6060/debug/pprof/profile?seconds=5
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof" // registers /debug/pprof/* on http.DefaultServeMux
	"os"
	"runtime"
	"runtime/pprof"
	"time"

	profiling "labs/02-profiling"
)

func main() {
	impl := flag.String("impl", "before", "implementation to run: before or after")
	lines := flag.Int("lines", 10_000, "number of log lines")
	duration := flag.Duration("duration", 3*time.Second, "how long to keep building reports")
	cpuprofile := flag.String("cpuprofile", "", "write a CPU profile to `file`")
	memprofile := flag.String("memprofile", "", "write an allocation profile to `file`")
	httpAddr := flag.String("http", "", "serve net/http/pprof on `addr` (e.g. localhost:6060)")
	flag.Parse()

	report := profiling.ReportBefore
	if *impl == "after" {
		report = profiling.ReportAfter
	}

	if *httpAddr != "" {
		go func() { log.Fatal(http.ListenAndServe(*httpAddr, nil)) }()
		log.Printf("pprof endpoints at http://%s/debug/pprof/", *httpAddr)
	}

	if *cpuprofile != "" {
		f, err := os.Create(*cpuprofile)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		if err := pprof.StartCPUProfile(f); err != nil {
			log.Fatal(err)
		}
		defer pprof.StopCPUProfile()
	}

	input := profiling.GenerateLog(*lines)
	var rows []profiling.EndpointStats
	reports := 0
	for start := time.Now(); time.Since(start) < *duration; reports++ {
		rows = report(input)
	}
	fmt.Printf("%s: built %d reports in %v (%v per report)\n",
		*impl, reports, *duration, *duration/time.Duration(reports))
	for _, r := range rows[:min(5, len(rows))] {
		fmt.Printf("  %-36s %6d requests %5d errors\n", r.Endpoint, r.Requests, r.Errors)
	}

	if *memprofile != "" {
		f, err := os.Create(*memprofile)
		if err != nil {
			log.Fatal(err)
		}
		defer f.Close()
		runtime.GC() // flush recent allocations into the profile
		if err := pprof.Lookup("allocs").WriteTo(f, 0); err != nil {
			log.Fatal(err)
		}
	}
}
