# Helping the Garbage Collector

> **Want to learn more?** Read the official [Go GC guide](https://go.dev/doc/gc-guide), then the pacer in [`runtime/mgcpacer.go`](https://github.com/golang/go/blob/go1.27.0/src/runtime/mgcpacer.go), the marking code in [`runtime/mgcmark.go`](https://github.com/golang/go/blob/go1.27.0/src/runtime/mgcmark.go) and the new Green Tea marker in [`runtime/mgcmark_greenteagc.go`](https://github.com/golang/go/blob/go1.27.0/src/runtime/mgcmark_greenteagc.go). The `gctrace` format is documented in [`runtime/extern.go`](https://github.com/golang/go/blob/go1.27.0/src/runtime/extern.go).

So far we've been reducing *allocations*: fewer objects means fewer GC cycles. This module looks at the other half of the bill. **How much does each GC cycle cost?** The answer surprises many people: it barely depends on how much garbage you make. It depends on how much *live* memory you have, and above all on **how many pointers are in it**. A program with a big, pointer-heavy cache pays for that cache on every single GC cycle, forever. We'll measure that, make the cache invisible to the GC, and then learn the two knobs that control how often the GC runs: `GOGC` and `GOMEMLIMIT`.

## Learning Objectives

By the end of this module, you will:

- Know how Go's concurrent mark-sweep GC works, and what Green Tea (the default GC since Go 1.26) changes
- Understand the cost model: mark work is proportional to the live, *pointerful* heap
- Know why `noscan` objects (no pointers inside) are almost free for the GC, and that strings are pointers too
- Read a `GODEBUG=gctrace=1` line field by field
- Know how the pacer computes the heap goal, and how to trade memory for CPU with `GOGC` and `GOMEMLIMIT`

## The Concept: What the GC Actually Does

### Concurrent mark and sweep

Go's GC is a **non-moving, concurrent, tri-color mark-sweep** collector. Each cycle has four phases:

1. **Sweep termination** (stop-the-world, usually tens of microseconds): finish the previous cycle's sweeping, enable the write barrier.
2. **Concurrent mark**: starting from the *roots* (goroutine stacks, globals), follow every pointer and mark every reachable object. This runs alongside your code, on dedicated background workers (25% of `GOMAXPROCS`, `gcBackgroundUtilization = 0.25`), on idle Ps, and through **mark assists**: goroutines that allocate too fast are made to do some marking themselves.
3. **Mark termination** (stop-the-world, short).
4. **Concurrent sweep**: unmarked objects are freed, lazily, as spans are reused.

Freeing garbage is cheap: sweeping is just looking at mark bits. The expensive part is **marking**, and marking only touches *live* objects. Garbage is never visited.

### The cost model: pointers are what costs

When the marker reaches an object, it has to scan it: read every word that might be a pointer, and follow it. But Go knows the type of every allocation, and the allocator puts objects **without any pointers** in separate *noscan* spans. Look at `greyobject` in `runtime/mgcmark.go`:

```go
// If this is a noscan object, fast-track it to black
// instead of greying it.
if span.spanclass.noscan() {
	gcw.bytesMarked += uint64(span.elemsize)
	return
}
```

![The GC follows every pointer in the heap; pointer-free memory is never scanned](images/14-mark.webp)

A noscan object is marked and that's it: the GC never reads its contents. A 100 MB `[]byte` or `[]float64` costs the same to mark as a 16-byte one. A 100 MB `[]*T` means 13 million pointers to follow, each to a different object that must be found, marked and scanned in turn.

So the cost of one GC cycle is roughly:

> **mark work ∝ (live heap that contains pointers) + (stacks) + (globals)**

Things that contain pointers, and that people often forget: `string` (pointer + length), slices, maps of pointer-ish types, interfaces, `time.Time` (it has a `*Location`).

### Green Tea: the default GC since Go 1.26

Go 1.25 shipped a new marking algorithm as `GOEXPERIMENT=greenteagc`. Since Go 1.26 it's **on by default**, and it still is in Go 1.27 (`GreenTeaGC: true` in the baseline of `internal/buildcfg/exp.go`; you can still turn it off with `GOEXPERIMENT=nogreenteagc` at build time). The comment at the top of `mgcmark_greenteagc.go` explains the idea:

> *"achieve better locality during mark/scan by delaying scanning so that we can accumulate objects to scan within the same span, then scan the objects that have accumulated on the span all together."*

The classic marker kept a LIFO stack of individual objects and jumped around the heap following pointers, a cache miss per object. Green Tea queues whole **spans** (FIFO), so by the time a span is scanned, several of its objects have been marked, and they're scanned together from the same few cache lines. On CPUs with AVX-512 (like the Zen 5 used below), it even scans small-object spans with SIMD (`internal/runtime/gc/scan`).

Green Tea makes each pointer cheaper to follow. It doesn't change the cost model: pointers are still what costs. We'll measure both.

### The pacer: when does a cycle start?

The pacer decides the **heap goal**, the heap size at which the next cycle should be done. In `gcControllerState.commit` (`mgcpacer.go`):

```go
gcPercentHeapGoal = c.heapMarked + (c.heapMarked+c.lastStackScan.Load()+c.globalsScan.Load())*uint64(gcPercent)/100
```

![The heap goal is the live heap plus GOGC percent; a GC cycle starts as the heap approaches the goal](images/14-pacer.webp)

With the default `GOGC=100`: if 200 MB survived the last cycle, the next one should finish when the heap reaches ~400 MB. There is also a floor, `heapMinimum` (4 MB at `GOGC=100`, `defaultHeapMinimum`), so tiny heaps don't collect constantly. So:

- **GOGC** trades memory for CPU. `GOGC=200` lets the heap grow to 3x the live heap and runs half as many cycles. `GOGC=50` does the opposite. `GOGC=off` disables the proportional goal. You can also call `debug.SetGCPercent(n)` at run time.
- **GOMEMLIMIT** (Go 1.19+, `debug.SetMemoryLimit`) is a **soft limit on the total memory the runtime uses**. When you get close to it, the pacer uses a lower heap goal (`memoryLimitHeapGoal`), whatever `GOGC` says. To avoid a death spiral when the live heap is near the limit, a CPU limiter caps GC CPU at about 50% (`mgclimit.go`). The common pattern for containers is `GOGC=off GOMEMLIMIT=<~90% of the container>`: no GC until you need it.

### Reading `gctrace`

`GODEBUG=gctrace=1` prints one line per cycle to stderr. Here's one from this module's lab:

```
gc 10 @0.537s 1%: 0.041+15+0.011 ms clock, 1.3+0/117/307+0.36 ms cpu, 286->286->286 MB, 573 MB goal, 0 MB stacks, 0 MB globals, 32 P (forced)
```

| Field | Meaning |
|---|---|
| `gc 10` | 10th GC cycle since the program started |
| `@0.537s` | time since the program started |
| `1%` | share of the CPU spent in GC since the program started |
| `0.041+15+0.011 ms clock` | wall time of: STW sweep termination + concurrent mark + STW mark termination |
| `1.3+0/117/307+0.36 ms cpu` | CPU time of: sweep termination + mark split into **assist / background / idle** + mark termination |
| `286->286->286 MB` | heap at the start of the cycle -> at the end -> live (marked) heap |
| `573 MB goal` | the heap goal for the next cycle: 286 × 2, because `GOGC=100` |
| `0 MB stacks, 0 MB globals` | scannable stacks and globals, the other roots |
| `32 P` | `GOMAXPROCS` |
| `(forced)` | triggered by `runtime.GC()`, not by the pacer |

The pauses (first and third numbers) are tiny. The mark phase took 15 ms of wall time but **424 ms of CPU** across cores. That CPU is what you pay, every cycle, for this heap.

## The Lab

```
labs/14-gc-pressure/
  before.go      StoreBefore: []*Record + map[int64]*Record (baseline, don't edit)
  after.go       your copy: make it pointer-free
  solution.go    reference solution (built with -tags solution)
  lab_test.go    correctness test + benchmarks
  cmd/gctune/    GOGC / GOMEMLIMIT experiment
```

`StoreBefore` is an in-memory table of user records, indexed by ID: perfectly idiomatic Go.

```go
type Record struct {
	ID    int64
	Name  string
	Email string
	Score float64
	Age   int32
}

type StoreBefore struct {
	records []*Record
	byID    map[int64]*Record
}
```

With 2 million records that's 6 million heap objects (2M `Record`s and 4M string backing arrays) and about **8 million pointers** the GC must follow on every cycle: two strings per record, plus 2M in the slice and 2M more in the map. There are three benchmarks, each with a 2M-record store:

- `GC`: the cost of one full `runtime.GC()` with the store alive. Pure mark cost.
- `Churn`: a "request handler" next to the store that allocates 16 KB of short-lived garbage per op. It reports `gc-cpu-ns/op`, the GC CPU time per op (from `runtime/metrics`, `/cpu/classes/gc/total:cpu-seconds`).
- `AverageScore`: a full scan of the records, to check that the new layout isn't slower for normal work.

### Step 1: Measure the baseline

```bash
make bench LAB=14
```

This one takes a minute or two: building a 2M-record store takes a while.

### Step 2: Look at the GC

```bash
cd labs
go test -c -o gc.test ./14-gc-pressure/
GODEBUG=gctrace=1 ./gc.test -test.run '^$' -test.bench 'GC/impl=before' -test.benchtime 3x 2>&1 | grep '^gc ' | tail -3
```

```
gc 10 @0.537s 1%: 0.041+15+0.011 ms clock, 1.3+0/117/307+0.36 ms cpu, 286->286->286 MB, 573 MB goal, 0 MB stacks, 0 MB globals, 32 P (forced)
gc 11 @0.560s 2%: 0.062+13+0.011 ms clock, 2.0+0/105/260+0.35 ms cpu, 286->286->286 MB, 573 MB goal, 0 MB stacks, 0 MB globals, 32 P (forced)
gc 12 @0.584s 2%: 0.033+13+0.010 ms clock, 1.0+0/104/216+0.33 ms cpu, 286->286->286 MB, 573 MB goal, 0 MB stacks, 0 MB globals, 32 P (forced)
```

286 MB live, ~14 ms of mark wall time and ~370 ms of mark CPU, every cycle. Nothing in the store changes between cycles, but the GC can't know that: it follows all 8 million pointers again each time.

### Step 3: Make the store pointer-free

Edit `labs/14-gc-pressure/after.go`:

1. **Strings are pointers.** Store all names and emails in one big `text []byte`, and keep only `uint32` offsets and `uint16` lengths in the record. `Lookup` rebuilds the string with `string(s.text[off:off+len])`.
2. **Store records by value**: `records []recordAfter`, not `[]*recordAfter`. Now the slice is one big allocation, and since `recordAfter` has no pointers, it lands in a noscan span.
3. **Make the map pointer-free**: `map[int64]int32`, an index into `records`. Since the map's keys and values contain no pointers, its groups are allocated as noscan too.

Now the whole store is three objects: one with pointers (the `StoreAfter` struct itself) and two big noscan arrays, plus the map's noscan groups. Run `go test ./14-gc-pressure/` to check that `Lookup` and `AverageScore` still agree.

### Step 4: Compare

```bash
make bench LAB=14
```

Results with the reference solution (`make solution LAB=14`) on an AMD Ryzen AI MAX+ 395 (16 cores, 32 threads, shared with other workloads while measuring):

```
                │    before     │                after                │
                │    sec/op     │    sec/op     vs base               │
GC-32             22769.9µ ± 3%   387.4µ ± 19%  -98.30% (p=0.002 n=6)
Churn-32           1367.5n ± 6%   819.4n ±  8%  -40.08% (p=0.002 n=6)
AverageScore-32     7.627m ± 4%   1.409m ±  4%  -81.52% (p=0.002 n=6)

         │    before     │                after                 │
         │ gc-cpu-sec/op │ gc-cpu-sec/op  vs base               │
Churn-32   13512.0n ± 9%    137.2n ± 11%  -98.98% (p=0.002 n=6)
```

(`benchstat` renames our `gc-cpu-ns/op` unit to `gc-cpu-sec/op`. The B/op and allocs/op tables are identical for both, so they're omitted.)

And `gctrace` for the new store:

```
gc 8 @0.357s 0%: 0.038+0.14+0.007 ms clock, 1.2+0/0.38/0.062+0.24 ms cpu, 210->210->210 MB, 420 MB goal, 0 MB stacks, 0 MB globals, 32 P (forced)
```

## Understanding the Results

**A full GC went from 23 ms to 0.39 ms, 59x faster**, and the heap is still 210 MB. The heap is only 27% smaller, so the size of the heap isn't what changed. The *number of pointers* went from ~8 million to almost zero. The GC marks the three big objects, sees `noscan`, and stops.

**Churn: GC CPU per op is ~98x lower**, and the request handler itself got 40% faster. In the before case, the background mark workers and assists spent about 13.5 µs of CPU per 1.4 µs op. That's 10x more CPU on GC than on the actual work. It doesn't fully show up in `sec/op` because most of it runs on otherwise idle cores. On a busy production server, those cores aren't idle.

**The scan got 5.4x faster too.** `[]recordAfter` is one contiguous array: the hardware prefetcher streams it (see [CPU Caches and Data Locality](10-cpu-caches.md)). `[]*Record` makes the CPU load a pointer, then jump to the record. Pointer-free layouts help both the GC and your own loops.

**What does Green Tea contribute?** You can build the same benchmark with the old marker and compare (same machine):

```bash
GOEXPERIMENT=nogreenteagc go test -c -tags solution -o old.test ./14-gc-pressure/
```

```
                  │ nogreenteagc │         greenteagc (default)         │
                  │    sec/op    │    sec/op     vs base                │
GC/impl=before-32   33.90m ± 32%   22.37m ±  2%  -34.01% (p=0.000 n=10)
GC/impl=after-32    649.0µ ±  6%   419.5µ ± 14%  -35.37% (p=0.000 n=10)
```

Green Tea cuts mark time on the pointer-heavy store by about a third for free (we measured about a quarter with Go 1.26; the old marker is very noisy here, see its ±32%). Changing the data layout cut it by **98%**. A better GC helps, but the best GC work is the work it doesn't have to do.

> **Trade-offs.** Offsets into a `[]byte` aren't free: you can't delete a record without leaving a hole, `Lookup` now allocates a string (callers could use the bytes directly), and the code is less obvious. Do this for big, long-lived structures (caches, indexes, in-memory tables) that a profile or `gctrace` shows dominating the mark phase, not for everything.

## Tuning GOGC and GOMEMLIMIT

`cmd/gctune` keeps a 1M-record `StoreBefore` alive (~140 MB live) and allocates 8 GiB of short-lived 16 KB buffers next to it. Then it reports the GC cycles, GC CPU time, wall time and peak RSS. Run it with different settings:

```bash
cd labs
GOGC=50 go run ./14-gc-pressure/cmd/gctune
GOGC=off GOMEMLIMIT=400MiB go run ./14-gc-pressure/cmd/gctune
go run -tags solution ./14-gc-pressure/cmd/gctune -store=after
```

```
store=before GOGC=25    GOMEMLIMIT=off       GCs= 203  GC CPU= 24.98s  wall= 2.21s  peak RSS= 219 MiB
store=before GOGC=50    GOMEMLIMIT=off       GCs=  96  GC CPU= 11.71s  wall= 1.15s  peak RSS= 279 MiB
store=before GOGC=100   GOMEMLIMIT=off       GCs=  45  GC CPU=  5.18s  wall= 0.70s  peak RSS= 418 MiB
store=before GOGC=200   GOMEMLIMIT=off       GCs=  24  GC CPU=  3.08s  wall= 0.51s  peak RSS= 615 MiB
store=before GOGC=400   GOMEMLIMIT=off       GCs=  12  GC CPU=  1.44s  wall= 0.50s  peak RSS= 951 MiB
store=before GOGC=100   GOMEMLIMIT=300MiB    GCs=  85  GC CPU= 10.28s  wall= 1.10s  peak RSS= 293 MiB
store=before GOGC=off   GOMEMLIMIT=400MiB    GCs=  50  GC CPU=  6.07s  wall= 0.75s  peak RSS= 386 MiB
store=before GOGC=off   GOMEMLIMIT=1GiB      GCs=  12  GC CPU=  1.52s  wall= 0.55s  peak RSS= 984 MiB
store=after  GOGC=100   GOMEMLIMIT=off       GCs=  78  GC CPU=  0.11s  wall= 0.39s  peak RSS= 230 MiB
store=after  GOGC=25    GOMEMLIMIT=off       GCs= 314  GC CPU=  0.36s  wall= 0.38s  peak RSS= 138 MiB
```

Read it as a trade-off curve:

- **GOGC is a dial between memory and CPU.** Each doubling of GOGC roughly halves the number of cycles and the GC CPU, and raises the peak memory. The cost of each cycle stays the same (~120 ms of CPU, the cost of marking the store).
- **GOMEMLIMIT caps memory instead of setting a ratio.** `GOGC=off GOMEMLIMIT=1GiB` behaves like `GOGC=400` here, because that's where the limit happens to fall. With a tight limit (`300MiB`, close to the live heap plus the garbage in flight), the GC runs twice as often as with the default settings. Set the limit too close to the live heap and the program spends most of its time collecting. That's what the 50% CPU limiter is there to stop.
- **The layout beats the knobs.** With the pointer-free store, `GOGC=25` runs 314 cycles (more than any other row, because the live heap is smaller) and still uses **about 1/14th of the GC CPU** of the default settings with the pointer-heavy store, with a third of the memory. Tuning GOGC moves you along the curve. Removing pointers gives you a better curve.

The "GC CPU" column is much bigger than the wall time because marking runs in parallel on up to 32 cores, using idle Ps. On a machine with no idle cores, that CPU comes out of your request handling. The numbers depend a lot on core count.

## Key Takeaways

- Marking costs ∝ live heap **with pointers** + stacks + globals. Garbage and pointer-free (`noscan`) memory are nearly free
- Strings, slices, maps, interfaces and `time.Time` all contain pointers
- Big long-lived structures: prefer `[]T` over `[]*T`, indices over pointers, one `[]byte` + offsets over many strings, and pointer-free map keys and values
- Green Tea (default since 1.26) scans span by span for locality, and makes marking ~33% cheaper here. Layout changes made it 98% cheaper
- Heap goal = live + (live + stacks + globals) × GOGC/100. `GOGC` trades memory for CPU, `GOMEMLIMIT` caps memory
- `GODEBUG=gctrace=1` tells you how often the GC runs, how big the live heap is and how much CPU marking takes

## Going Further (optional)

1. Add a `time.Time` field to `Record` in a copy of the after store. Run the `GC` benchmark again. What happened, and why?
2. Run `cmd/gctune` with `GOGC=off GOMEMLIMIT=200MiB`. Watch the GC CPU time and the wall time. What is the runtime doing?
3. Use `go tool pprof` on a CPU profile of `BenchmarkChurn/impl=before`. How much time goes to `gcBgMarkWorker`, and how much to `gcAssistAlloc`?
4. Compare `runtime/metrics` `/gc/scan/heap:bytes` for both stores. How does it relate to the mark time?
