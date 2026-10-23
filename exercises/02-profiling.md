# Profiling 101

> **Want to learn more?** Read [`runtime/pprof/pprof.go`](https://github.com/golang/go/blob/go1.27.0/src/runtime/pprof/pprof.go) (look for `StartCPUProfile`), the per-thread profiling timers in [`runtime/os_linux.go`](https://github.com/golang/go/blob/go1.27.0/src/runtime/os_linux.go) (`setThreadCPUProfiler`, `validSIGPROF`), the signal handler `sigprof` in [`runtime/proc.go`](https://github.com/golang/go/blob/go1.27.0/src/runtime/proc.go), and the heap sampler in [`runtime/malloc.go`](https://github.com/golang/go/blob/go1.27.0/src/runtime/malloc.go) and [`runtime/mprof.go`](https://github.com/golang/go/blob/go1.27.0/src/runtime/mprof.go).

A benchmark tells you *how fast* something is. It doesn't tell you *why*. For that you need a profiler, and Go has one built into the runtime: no instrumentation, no recompilation, just a flag. In this module we look at how the CPU and heap profilers collect their data, because knowing that tells you how far you can trust them. Then we use `go tool pprof` to find a hot spot that isn't where you'd guess.

## Learning Objectives

By the end of this module, you will:

- Understand how the CPU profiler samples your program (SIGPROF, 100 Hz, per-thread timers on Linux)
- Understand how the heap profiler samples allocations (one every ~512 KB) and the difference between `alloc_space` and `inuse_space`
- Collect profiles from benchmarks (`-cpuprofile`, `-memprofile`) and from programs (`runtime/pprof`, `net/http/pprof`)
- Read `pprof` output: `flat` vs `cum`, `-top`, `-list`, `-peek` and the flame graph
- Find and fix a hot spot hidden behind a helper function

## The Concept: Sampling Profilers

Go's profilers are **sampling** profilers. They don't record every function call, which would be far too slow. Instead they take a snapshot every so often and count how many snapshots land in each place. More snapshots means more time (or more bytes).

### The CPU profiler: a signal 100 times per second

`pprof.StartCPUProfile` hard-codes the rate:

```go
// 100 Hz is a reasonable choice: it is frequent enough to
// produce useful data, rare enough not to bog down the
// system, and a nice round number to make it easy to
// convert sample counts to seconds. Instead of requiring
// each client to specify the frequency, we hard code it.
const hz = 100
```

![The CPU profiler interrupts the program 100 times per second and records the stack](images/02-sampling.webp)

and calls `runtime.SetCPUProfileRate(hz)`. From then on, the kernel delivers a `SIGPROF` signal every 10 ms **of CPU time consumed**. The runtime's signal handler calls `sigprof` (in `proc.go`), which walks the interrupted goroutine's stack (up to `maxCPUProfStack = 64` frames) and adds it to a buffer that a background goroutine writes out as the profile.

*Which* clock drives those signals matters. The classic Unix mechanism, `setitimer(ITIMER_PROF)`, is process-wide: the kernel picks some thread to deliver the signal to, and with many busy threads, samples get lost or land in the wrong place. On Linux, Go also creates **one timer per thread** with `timer_create(CLOCK_THREAD_CPUTIME_ID)`, in `setThreadCPUProfiler`:

```go
spec.it_value.setNsec(1 + int64(cheaprandn(uint32(1e9/hz))))
spec.it_interval.setNsec(1e9 / int64(hz))

var timerid int32
var sevp sigevent
sevp.notify = _SIGEV_THREAD_ID
sevp.signo = _SIGPROF
sevp.sigev_notify_thread_id = int32(mp.procid)
ret := timer_create(_CLOCK_THREAD_CPUTIME_ID, &sevp, &timerid)
```

Each thread gets a signal for every 10 ms of *its own* CPU time. The random first delay means that even a thread that runs for just 1 ms has a 10% chance of being sampled, instead of never being seen. Because both timers can be active, `validSIGPROF` checks the signal's origin (`_SI_TIMER` vs `_SI_KERNEL`) to avoid counting the same CPU time twice.

What this means for you:

- **Samples measure CPU time, not wall time.** A goroutine waiting on a channel, a lock or the network doesn't consume CPU, so it doesn't show up. (Use the block, mutex or trace profilers for that. For goroutines stuck *forever*, on a channel or lock no running goroutine can reach any more, Go 1.27 adds the `goroutineleak` profile, also served at `/debug/pprof/goroutineleak`.)
- **Total samples can exceed wall time.** Two threads busy for 1 s produce 2 s of samples. You'll see `Duration: 1.20s, Total samples = 1.59s (132.93%)` below: the extra is mostly the garbage collector running on other threads.
- **It's statistical.** 1 s of CPU gives you ~100 samples. A function at 2% might be noise. Profile for longer if you need detail.

### The heap profiler: one sample every 512 KB

Allocations are sampled by bytes, not by time. From `mprof.go`:

```go
// The profiler aims to sample an average of
// one allocation per MemProfileRate bytes allocated.
var MemProfileRate int = 512 * 1024
```

Each `mcache` keeps a countdown, `nextSample`. When an allocation crosses it, `profilealloc` records the allocation's stack and draws a new countdown from an exponential distribution with mean `MemProfileRate` (`nextSample` in `malloc.go`), so sampling points are spread randomly instead of hitting every Nth object. `pprof` then scales each sample back up to estimate the real totals. Like the CPU profile, small numbers are estimates: set `runtime.MemProfileRate = 1` (or `go test -memprofilerate=1`) to record every allocation, at a cost.

A heap profile contains four values per stack:

| Sample type | Meaning |
|---|---|
| `alloc_space` / `alloc_objects` | Everything allocated since the program started, even if it's already garbage |
| `inuse_space` / `inuse_objects` | What was still live at the most recent GC |

The `heap` profile defaults to `inuse_space` (what's using memory *now*: hunting leaks), and the `allocs` profile is the same data defaulting to `alloc_space` (what's creating work for the allocator and the GC: hunting speed). `go test -memprofile` writes the `allocs` profile, after a `runtime.GC()` to "materialize all statistics".

### Getting profiles

From benchmarks, which is what we'll use in the labs:

```bash
go test -run '^$' -bench Foo -cpuprofile cpu.prof ./pkg/
go test -run '^$' -bench Foo -memprofile mem.prof ./pkg/
```

From a program, with `runtime/pprof`:

```go
f, _ := os.Create("cpu.prof")
pprof.StartCPUProfile(f)
defer pprof.StopCPUProfile()
```

From a running server, by importing `net/http/pprof` for its side effect. It registers handlers under `/debug/pprof/` on `http.DefaultServeMux`:

```go
import _ "net/http/pprof"
```

```bash
go tool pprof 'http://localhost:6060/debug/pprof/profile?seconds=5'   # CPU
go tool pprof http://localhost:6060/debug/pprof/heap                   # heap
```

### Reading a profile: `flat` vs `cum`

Every `pprof` report has two numbers per function:

- **flat**: samples where this function was *at the top of the stack*, running its own code
- **cum** (cumulative): samples where this function was *anywhere on the stack*: itself plus everything it called

![flat counts samples where a function itself was running, cum includes everything it called](images/02-flat-cum.webp)

`flat` tells you which code burns cycles. `cum` tells you which *decision* is responsible for it. A slow program often has a flat profile with nothing standing out (malloc here, a map lookup there) while one function has an enormous `cum`. The commands you'll use:

| Command | Shows |
|---|---|
| `-top` | Functions sorted by flat. Add `-cum` to sort by cumulative |
| `-list Regexp` | Annotated source of the matching functions, with time per line |
| `-peek Regexp` | Who calls the matching functions and what they call |
| `-http=:` | Web UI with a graph, a flame graph (View → Flame Graph) and source views |

## The Lab

```
labs/02-profiling/
  before.go          ReportBefore, the slow version (baseline, don't edit)
  after.go           your copy: make this one fast
  solution.go        reference solution (built with -tags solution)
  data.go            GenerateLog, a fake access-log generator
  lab_test.go        correctness tests + benchmark
  cmd/logreport/     the same code as a program, with -cpuprofile and -http
```

`ReportBefore` takes access-log lines like

```
2026-10-23T10:00:00Z GET /api/users/4821/orders/17 200 12ms
```

and returns per-endpoint request and error counts, with numeric IDs normalized (`GET /api/users/:id/orders/:id`). The benchmark runs it over 10,000 lines. It's slow, and your job is to find out why **without guessing**. Resist reading `before.go` too carefully before you profile!

### Step 1: Measure the baseline

```bash
make bench LAB=02
```

Write down `sec/op`, `B/op` and `allocs/op`. About 12 ms, 16 MB and 225,000 allocations to process 10,000 lines: 22 allocations and 1.6 KB per line. That should already feel like a lot.

### Step 2: Profile the CPU

```bash
cd labs
go test -run '^$' -bench 'Report/impl=before' -cpuprofile cpu.prof ./02-profiling/
go tool pprof -top cpu.prof | head -20
```

```
Duration: 1.20s, Total samples = 1.59s (132.93%)
      flat  flat%   sum%        cum   cum%
     0.11s  6.92%  6.92%      0.11s  6.92%  strings.Fields
     0.07s  4.40% 11.32%      0.11s  6.92%  regexp.(*Regexp).tryBacktrack
     0.06s  3.77% 15.09%      0.06s  3.77%  runtime.(*mspan).init
     0.05s  3.14% 18.24%      0.32s 20.13%  runtime.growslice
     0.05s  3.14% 21.38%      0.41s 25.79%  runtime.mallocgc
     ...
```

The flat view is a mess: no single function is above 7%. Lots of malloc, some regexp, some GC. (In Go 1.27 the allocator cost is split even further: further down you'll find size-specialized routines such as `runtime.mallocgcSmallScanNoHeaderSC5`, one per size class; see [module 03](03-preallocation.md).) Now sort by cumulative:

```bash
go tool pprof -top -cum cpu.prof | head -16
```

```
      flat  flat%   sum%        cum   cum%
         0     0%     0%      1.27s 79.87%  labs/02-profiling.ReportBefore
         0     0%     0%      1.09s 68.55%  labs/02-profiling.normalizePathBefore
         0     0%     0%      0.79s 49.69%  regexp.MustCompile
         0     0%     0%      0.79s 49.69%  regexp.Compile (inline)
         0     0%     0%      0.79s 49.69%  regexp.compile
     0.05s  3.14%  3.14%      0.41s 25.79%  runtime.mallocgc
     0.01s  0.63%  3.77%      0.33s 20.75%  regexp/syntax.parse
         0     0% 14.47%      0.20s 12.58%  runtime.gcBgMarkWorker
```

That's a story: most of `ReportBefore` is `normalizePathBefore`, and most of *that* is `regexp.MustCompile`. Confirm who calls it:

```bash
go tool pprof -peek 'regexp.MustCompile$' cpu.prof
```

```
                                             0.79s   100% |   labs/02-profiling.normalizePathBefore
         0     0%     0%      0.79s 49.69%                | regexp.MustCompile
                                             0.79s   100% |   regexp.Compile (inline)
```

And look at the line-level cost:

```bash
go tool pprof -list 'normalizePathBefore' cpu.prof
```

```
         .      790ms     48:	re := regexp.MustCompile(`/[0-9]+`)
         .      300ms     49:	return re.ReplaceAllString(path, "/:id")
```

Finally, open the flame graph and find the same thing visually: `go tool pprof -http=: cpu.prof`, then *View → Flame Graph*. The width of each bar is its `cum`.

### Step 3: Confirm with the heap profile

```bash
go test -run '^$' -bench 'Report/impl=before' -memprofile mem.prof ./02-profiling/
go tool pprof -sample_index=alloc_space -top mem.prof | head -12
```

```
Showing nodes accounting for 1.33GB, 99.12% of 1.34GB total
      flat  flat%   sum%        cum   cum%
    0.49GB 36.81% 36.81%     0.49GB 36.81%  regexp/syntax.(*compiler).inst (inline)
    0.36GB 26.54% 63.34%     0.36GB 26.54%  regexp/syntax.(*parser).newRegexp (inline)
    0.14GB 10.27% 73.61%     1.19GB 88.49%  regexp.compile
  ...
    0.06GB  4.48% 86.06%     0.06GB  4.48%  strings.Fields
```

**88% of all bytes** come from compiling the regular expression. Try `-sample_index=inuse_space` too: it's almost empty, because all of that is short-lived garbage. Not a leak, just a lot of work for the allocator and the GC (that's the `gcBgMarkWorker` in the CPU profile).

### Step 4: Fix it

Edit `labs/02-profiling/after.go`. The regular expression never changes, yet it's compiled once **per log line**. Compile it once, in a package-level variable, and reuse it (a compiled `*regexp.Regexp` is safe for concurrent use):

```go
var idSegment = regexp.MustCompile(`/[0-9]+`)
```

`go test ./02-profiling/` checks the report didn't change.

### Step 5: Compare

```bash
make bench LAB=02
```

Results with the reference solution (`make solution LAB=02`) on an AMD Ryzen AI MAX+ 395:

```
          │    before    │               after                │
          │    sec/op    │   sec/op     vs base               │
Report-32   11.593m ± 8%   3.109m ± 2%  -73.19% (p=0.002 n=6)

          │    before     │                after                │
          │     B/op      │     B/op      vs base               │
Report-32   15.897Mi ± 0%   1.684Mi ± 0%  -89.40% (p=0.002 n=6)

          │    before    │               after                │
          │  allocs/op   │  allocs/op   vs base               │
Report-32   225.08k ± 0%   55.04k ± 0%  -75.55% (p=0.002 n=6)
```

Clean up the profiles when you're done: `rm -f cpu.prof mem.prof 02-profiling.test`. (When you pass a profiling flag, `go test` keeps the test binary next to the profile.)

## Understanding the Results

A **3.7x speedup and 9x less memory from moving one line**. The lesson is not "regexps are slow". It's that the cost was invisible in the code (`normalizePath` looks innocent and tiny) and invisible in the flat profile, where it was spread over dozens of regexp-parser and allocator functions. Only `cum` connected all that work back to one decision.

Profile the new version and the picture changes:

```
      flat  flat%   sum%        cum   cum%
     0.01s  0.75%  0.75%      1.15s 86.47%  labs/02-profiling.ReportAfter
         0     0%  0.75%      0.68s 51.13%  labs/02-profiling.normalizePathAfter (inline)
         0     0%  0.75%      0.68s 51.13%  regexp.(*Regexp).ReplaceAllString
     0.26s 19.55% 26.32%      0.29s 21.80%  strings.Fields
         0     0% 42.86%      0.13s  9.77%  runtime.gcBgMarkWorker
```

Now running the regexp is half the time, and `strings.Fields` (one `[]string` per line) is the next item. That's how profiling works in practice: fix the top item, profile again, and stop when the top item is something you're happy to pay for.

### Profiling a program instead of a benchmark

The same code is wrapped in a small program. Run it with `-cpuprofile`, which uses `runtime/pprof` directly:

```bash
cd labs
go run ./02-profiling/cmd/logreport -cpuprofile cpu.prof
go tool pprof -top -cum cpu.prof | head -12
```

Or start it with `-http` and grab a profile from the running process through `net/http/pprof`, the way you'd do it with a production server:

```bash
go run ./02-profiling/cmd/logreport -http localhost:6060 -duration 60s &
go tool pprof -http=: 'http://localhost:6060/debug/pprof/profile?seconds=5'
```

`pprof` saves every profile it downloads under `$HOME/pprof/`, so you can come back to it later.

## Key Takeaways

- Go's profilers **sample**: the CPU profiler at 100 Hz of CPU time per thread (SIGPROF, per-thread `timer_create` timers on Linux), the heap profiler about once per 512 KB allocated
- The CPU profile shows **CPU time, not waiting**. Total samples can exceed wall time when several threads are busy
- `alloc_space` finds code that creates work for the allocator and the GC; `inuse_space` finds what's holding memory
- `flat` shows where cycles are burned; `cum` shows who's responsible. Start with `-top -cum`, then `-peek` and `-list`
- Measure with a benchmark, explain with a profile, fix, and **profile again**

## Going Further (optional)

1. The next hot spot is `regexp.ReplaceAllString`. Replace the regular expression with a hand-written loop over the path's segments. How much faster is it? Is the code still as easy to read?
2. Replace `strings.Fields` with a version that doesn't allocate a slice per line (for example `strings.Cut` three times). Check the gain with `benchstat`.
3. Run `go test -bench . -memprofile mem.prof -memprofilerate 1` and compare the totals with the default sampled profile. How close was the estimate?
