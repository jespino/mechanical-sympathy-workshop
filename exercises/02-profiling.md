# Profiling 101

A benchmark tells you *how fast* something is. It doesn't tell you *why*. For that you need a profiler, and Go has one built into the runtime: no instrumentation, no recompilation, just a flag. In this module we look at how the CPU and heap profilers collect their data, because knowing that tells you how far you can trust them. Then we use `go tool pprof` to find a hot spot that isn't where you'd guess.

## Learning Objectives

By the end of this module, you will:

- Understand how the CPU profiler samples your program (a snapshot of the stack 100 times per second)
- Understand how the heap profiler samples allocations (about one every 512 KB) and the difference between `alloc_space` and `inuse_space`
- Collect profiles from benchmarks (`-cpuprofile`, `-memprofile`) and from programs (`runtime/pprof`, `net/http/pprof`)
- Read `pprof` output: `flat` vs `cum`, `-top`, `-list` and the flame graph
- Find and fix a hot spot hidden behind a helper function

## The Concept: Sampling Profilers

Go's profilers are **sampling** profilers. They don't record every function call, which would be far too slow. Instead they take a snapshot every so often and count how many snapshots land in each place. More snapshots means more time (or more bytes).

### The CPU profiler: a snapshot 100 times per second

While the CPU profiler is on, the Go runtime interrupts your program 100 times per second (every 10 ms) and writes down what it was doing at that moment: the function that was running, and the functions that called it (the **stack**). At the end, `pprof` counts how many snapshots each function appears in. A function that shows up in half of them used about half of the CPU time.

![The CPU profiler interrupts the program 100 times per second and records the stack](images/02-sampling.webp)

The clock behind those snapshots counts **CPU time**, not wall-clock time, and each thread has its own. What this means for you:

- **Waiting doesn't show up.** A goroutine blocked on a channel, a lock or the network isn't using the CPU, so it's never caught. Go has other profiles for that, like the block and mutex profiles.
- **Total samples can exceed wall time.** Two threads busy for 1 s produce 2 s of samples.
- **It's statistics, not an exact count.** 1 s of CPU gives you about 100 snapshots, so a function at 2% might be noise.

### The heap profiler: one sample every 512 KB

The heap profiler doesn't interrupt your program. Instead, the allocator keeps track of how many bytes have been allocated, and roughly every 512 KB it writes down the stack of the allocation that happens at that moment. `pprof` then scales the samples up to estimate the real totals, so small numbers are estimates here too. If you need every allocation, `go test -memprofilerate=1` records them all, but it makes your program noticeably slower, so use it only when you need exact numbers.

A heap profile contains four values per stack:

| Sample type | Meaning |
|---|---|
| `alloc_space` / `alloc_objects` | Everything allocated since the program started, even if it's already garbage |
| `inuse_space` / `inuse_objects` | What was still live at the most recent GC |

The `heap` profile shows `inuse_space` by default: what's using memory *now*, which is what you want when hunting leaks. The `allocs` profile has the same data but shows `alloc_space` by default: everything that created work for the allocator and the GC, which is what you want when making code faster. `go test -memprofile` writes the `allocs` profile.

### Getting profiles

There are three common ways to get a profile, depending on what you're running.

The easiest one, and the one we'll use in the labs, is to ask `go test` for it while it runs a benchmark. Add `-cpuprofile` for a CPU profile, or `-memprofile` for a heap profile, and give it a file name:

```bash
go test -run '^$' -bench Foo -cpuprofile cpu.prof ./pkg/
go test -run '^$' -bench Foo -memprofile mem.prof ./pkg/
```

When the code you want to look at isn't a benchmark but a regular program, you can start and stop the CPU profiler yourself with the `runtime/pprof` package. Everything that runs between those two calls ends up in the file:

```go
f, _ := os.Create("cpu.prof")
pprof.StartCPUProfile(f)
defer pprof.StopCPUProfile()
```

And from a server, you can grab a profile while it keeps running and serving requests. Import `net/http/pprof` with a blank identifier: you don't call anything from it, the import alone adds a few pages under `/debug/pprof/` to your HTTP server (if it uses `http.DefaultServeMux`):

```go
import _ "net/http/pprof"
```

Then point `go tool pprof` at those pages. It downloads the profile and opens it, just like a file. The CPU one records for the number of seconds you ask for:

```bash
go tool pprof 'http://localhost:6060/debug/pprof/profile?seconds=5'   # CPU
go tool pprof http://localhost:6060/debug/pprof/heap                   # heap
```

### Reading a profile: `flat` vs `cum`

Every `pprof` report has two numbers per function:

- **flat**: samples where this function was *at the top of the stack*, running its own code
- **cum** (cumulative): samples where this function was *anywhere on the stack*: itself plus everything it called

![flat counts samples where a function itself was running, cum includes everything it called](images/02-flat-cum.webp)

`flat` tells you which code burns cycles. `cum` tells you which *decision* is responsible for it. A slow program often has a flat profile with nothing standing out (malloc here, a map lookup there) while one function has an enormous `cum`. That normally means the problem isn't one slow piece of code, but a decision in that function, like calling something expensive inside a loop or doing the same work again and again. That function is where you should look first.

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

The flat view is a mess: no single function is above 7%. Lots of malloc, some regexp, some GC. Now sort by cumulative, by adding the `-cum` flag:

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

That's a story: most of `ReportBefore` is `normalizePathBefore`, and most of *that* is `regexp.MustCompile`. Look at the line-level cost with `-list`:

```bash
go tool pprof -list 'normalizePathBefore' cpu.prof
```

```
         .      790ms     48:	re := regexp.MustCompile(`/[0-9]+`)
         .      300ms     49:	return re.ReplaceAllString(path, "/:id")
```

Finally, open the flame graph and find the same thing visually: `go tool pprof -http=: cpu.prof`, then *View → Flame Graph*. The width of each bar is its `cum`.

![The pprof flame graph of ReportBefore: normalizePathBefore takes most of the width, and regexp.MustCompile most of that](images/02-flame-graph.webp)

`normalizePathBefore` covers most of the width, and inside it `regexp.MustCompile` is the widest bar.

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

**88% of all bytes** come from compiling the regular expression. All of it is work for the allocator and the GC (that's the `gcBgMarkWorker` in the CPU profile).

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

## Key Takeaways

- Go's profilers **sample**: the CPU profiler about 100 times per second of CPU time, the heap profiler about once every 512 KB allocated
- The CPU profile shows **CPU time, not waiting**. Total samples can exceed wall time when several threads are busy
- `alloc_space` finds code that creates work for the allocator and the GC; `inuse_space` finds what's holding memory
- `flat` shows where cycles are burned; `cum` shows who's responsible. Start with `-top -cum`, then `-list`
- Measure with a benchmark, explain with a profile, fix, and **profile again**

## Going Further (optional)

1. The next hot spot is `regexp.ReplaceAllString`. Replace the regular expression with a hand-written loop over the path's segments. How much faster is it? Is the code still as easy to read?
2. Replace `strings.Fields` with a version that doesn't allocate a slice per line (for example `strings.Cut` three times). Check the gain with `benchstat`.
3. Run `go test -bench . -memprofile mem.prof -memprofilerate 1` and compare the totals with the default sampled profile. How close was the estimate?
