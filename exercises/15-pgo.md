# Profile-Guided Optimization

> **Want to learn more?** [The IR](https://internals-for-interns.com/posts/the-go-ir/#profile-guided-devirtualization) on Internals for Interns covers profile-guided devirtualization alongside the static kind.

So far, we have used profiles to decide what code to change. With **profile-guided optimization (PGO)**, we give a CPU profile to the compiler so it can make better decisions. In this lab, the source stays the same; only the build changes.

## Learning Objectives

- Collect a representative CPU profile.
- Build and benchmark with and without PGO.
- Recognize how extra inlining can remove allocations.
- Use `default.pgo` in an application build.

## The Concept: Feeding the Profile to the Compiler

### Let measurements guide the compiler

Without a profile, the compiler uses general rules such as the inlining budget of 80. A CPU profile shows which calls use the most CPU time. PGO lets the compiler spend more effort on those **hot calls**, including an inlining budget of up to 2000.

![PGO: build, run under real load, collect a CPU profile, and rebuild with it so the compiler optimizes the hot paths](images/15-pgo-loop.webp)

### More than removing calls

If an interface usually holds one concrete type, PGO can add a direct call for that type, with a fallback for other types. This is **devirtualization**. The fallback preserves correct behavior even when the workload changes.

Inlining can then let escape analysis see that an object is only needed locally. The result can be fewer heap allocations as well as fewer calls.

### Choosing a profile

| Build flag | Behavior |
|---|---|
| `-pgo=off` | build without PGO |
| `-pgo=cpu.prof` | use the specified profile |
| `-pgo=auto` (default) | use `default.pgo` beside the main package |

For this library benchmark, pass the profile explicitly. In an application, put `default.pgo` beside `main.go` to use it in normal builds. Collect profiles under representative load and refresh them as the workload changes.

## The Lab

`Aggregator.Ingest` reads web-request logs and passes each record to an `Observer`. A line such as `17,200,1534` means route 17 returned HTTP 200 in 1,534 microseconds. The usual observer counts requests in response-time ranges. The baseline parser is too large to inline and allocates a record per line. We will let a profile guide the compiler to optimize those calls.

There is no `after.go` in this lab. Use the commands below instead of `make bench LAB=15`. From the repository root, `make pgo` also runs the main comparison for you.

### Step 1: Measure without PGO

Run the remaining commands from `labs/`:

```bash
cd labs
go test -pgo=off -run '^$' -bench Ingest -benchmem -count 6 ./15-pgo/ | tee old.txt
```

Note the time and `allocs/op`. The baseline makes about 100,000 allocations per run.

### Step 2: Collect a profile

```bash
go test -pgo=off -run '^$' -bench Ingest -cpuprofile cpu.prof ./15-pgo/
go tool pprof -top cpu.prof
```

Look for `parseRecord` and allocation work such as `mallocgc`. This benchmark is our training workload: it should exercise the code we want to improve.

### Step 3: Build with the profile

```bash
go build -pgo=cpu.prof -gcflags='labs/15-pgo=-m -d=pgodebug=1' ./15-pgo/ 2>&1 | grep -E 'PGO|inline|Record.*escape'
```

Look for inlining of `parseRecord` and a PGO direct call to `Histogram.Observe`. At the inlined parser call, `Record` should no longer escape to the heap.

### Step 4: Check and compare

```bash
go test -pgo=cpu.prof ./15-pgo/
go test -pgo=cpu.prof -run '^$' -bench Ingest -benchmem -count 6 ./15-pgo/ | tee new.txt
benchstat old.txt new.txt
```

On my machine:

| Measurement | Without PGO | With PGO |
|---|---|---|
| Time per run | 1.979 ms | 1.004 ms |
| Allocations per run | 100,000 | 0 |

## Understanding the Results

The benchmark is about twice as fast. Most of the gain comes from inlining `parseRecord`, which lets escape analysis keep the record on the stack. The source code did not change.

This lab deliberately makes that interaction easy to see. Other programs may improve much less, and a profile from the wrong workload may not help. Measure your own application and check that its results stay correct.

## Using `default.pgo`

Still in `labs/`, try the application version:

```bash
go run ./15-pgo/cmd/ingest -cpuprofile /tmp/ingest.prof
cp /tmp/ingest.prof 15-pgo/cmd/ingest/default.pgo
go build -o /tmp/ingest-pgo ./15-pgo/cmd/ingest
go version -m /tmp/ingest-pgo | grep pgo
```

A normal build now finds the profile beside the main package. Use `-pgo=off` whenever you want an explicit comparison without it.

## Key Takeaways

- PGO uses a CPU profile to prioritize frequent calls.
- Extra inlining can also remove heap allocations.
- Pass `-pgo=file` for benchmarks; use `default.pgo` beside an application's main package.
- Use representative profiles and measure the resulting build.

## Going Further (optional)

1. Return `Record` by value and benchmark with `-pgo=off`. How much of the gain can you get by changing the code?
2. Change the benchmark observer to `&Counter{}`, collect a new profile and inspect the compiler's decisions.
3. Compare a build using the old profile with one using the new profile. Are both correct, and are their timings different?
