// loganalyzer generates a log, analyzes it with the before or after
// implementation, and writes the report to stdout. Use it to profile and to
// look at the output:
//
//	go run ./19-capstone/cmd/loganalyzer -impl before -lines 200000
//	go run ./19-capstone/cmd/loganalyzer -impl after -cpuprofile cpu.prof
//	go run ./19-capstone/cmd/loganalyzer -write access.log   # just the data
package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"runtime/pprof"
	"time"

	capstone "labs/19-capstone"
)

func main() {
	impl := flag.String("impl", "after", "before or after")
	lines := flag.Int("lines", 200_000, "number of log lines to generate")
	seed := flag.Uint64("seed", 42, "random seed for the generator")
	rounds := flag.Int("rounds", 1, "analyze the log this many times (for profiling)")
	cpuprofile := flag.String("cpuprofile", "", "write a CPU profile to this file")
	write := flag.String("write", "", "only write the generated log to this file")
	flag.Parse()

	data := capstone.GenerateLog(*lines, *seed)
	if *write != "" {
		if err := os.WriteFile(*write, data, 0o644); err != nil {
			log.Fatal(err)
		}
		return
	}

	analyze := capstone.AnalyzeAfter
	if *impl == "before" {
		analyze = capstone.AnalyzeBefore
	}

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

	start := time.Now()
	for i := range *rounds {
		var out io.Writer = io.Discard
		if i == *rounds-1 {
			out = os.Stdout
		}
		if err := analyze(bytes.NewReader(data), out); err != nil {
			log.Fatal(err)
		}
	}
	fmt.Fprintf(os.Stderr, "\n%s: %d lines (%.1f MB) x %d in %v\n", *impl, *lines,
		float64(len(data))/1e6, *rounds, time.Since(start).Round(time.Millisecond))
}
