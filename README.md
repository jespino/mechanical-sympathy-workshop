# Go Mechanical Sympathy Workshop

A hands-on workshop about writing Go that works *with* the machine instead of against it. Every module explains a mechanism inside the Go compiler, the runtime or the hardware, and then you measure the before and the after yourself with benchmarks and profiles.

Uses stock **Go 1.27**. No need to build Go from source.

## Modules

| # | Part | Module | Lab |
|---|------|--------|-----|
| 0 | Measure first | Introduction and Setup | Verify the environment |
| 1 | Measure first | Benchmarking 101 | Fix benchmarks that lie, use `benchstat` |
| 2 | Measure first | Profiling 101 | Find a hot spot with `pprof` |
| 3 | Allocations | Pre-allocation | `growslice` vs `make([]T, 0, n)`, map size hints |
| 4 | Allocations | Strings and Bytes | Conversions, concatenation, `strings.Builder` |
| 5 | Allocations | Escape Analysis | Keep values on the stack |
| 6 | Allocations | Inlining | Get a hot function under the budget |
| 7 | Allocations | Inlining × Escape Analysis | Outlined constructors, 0 allocs/op |
| 8 | Allocations | Interfaces, Boxing and Generics | `convT` allocations, dispatch, devirtualization |
| 9 | Hardware | Struct Layout and Size Classes | Reorder fields, drop a size class |
| 10 | Hardware | CPU Caches and Data Locality | Traversal order, `[]*T` vs `[]T`, AoS vs SoA |
| 11 | Hardware | Bounds Check Elimination | Help the `prove` pass |
| 12 | Hardware | Branch Prediction | Sorted vs unsorted, branchless code |
| 13 | Runtime | sync.Pool | Pool buffers in a hot path |
| 14 | Runtime | Helping the Garbage Collector | Pointer-free heaps, `GOGC`, `GOMEMLIMIT` |
| 15 | Runtime | Contention and False Sharing | Mutex → atomic → sharded → padded |
| 16 | Runtime | Goroutines, Channels and the Scheduler | Batching, worker pools |
| 17 | Runtime | I/O and Syscalls | Buffered I/O |
| 18 | Together | Profile-Guided Optimization | Rebuild with a CPU profile |
| 19 | Together | Capstone | Optimize a log analyzer end to end |

## Prerequisites

- Go 1.27 or newer
- `benchstat`: `go install golang.org/x/perf/cmd/benchstat@latest`
- Optional: Graphviz (pprof graphs), Linux `perf` (hardware counters in Part III)

## Running the Labs

```bash
make test-labs              # every lab builds and passes its tests
make bench LAB=03           # your labs/03-*/after.go vs before.go
make solution LAB=03        # the reference solution vs before.go
make bench LAB=15 CPU=1,4,16  # also sweep GOMAXPROCS
make pgo                    # module 18: build without vs with a CPU profile
```

## Enabling and Disabling Modules

Modules are listed in [`exercises/modules.json`](exercises/modules.json). Set `"enabled": false` on a module to leave it out of the website. Links to it from other modules turn into plain text, and prev/next navigation skips it.

To override the manifest for a single build:

```bash
make generate SKIP=12,17          # everything enabled except 12 and 17
make serve ONLY=00,01,02,03,05    # only these modules
```

Module numbers stay stable (they match the `labs/` directories), so a disabled module leaves a gap in the numbering.

## Running the Website Locally

```bash
make generate   # build into website/
make serve      # dev server with live reload at http://localhost:8000
```

## Structure

```
exercises/          Module pages (NN-name.md + NN-name.es.md) and modules.json
labs/               One Go module, one package per lab (before/after/solution)
images/             Images referenced from module pages
diagrams/           Prompts and script to regenerate the diagrams (uses codex)
website-generator/  Static site generator
website/            Generated output (gitignored)
```

---

Created by [Jesús Espino](https://github.com/jespino)
