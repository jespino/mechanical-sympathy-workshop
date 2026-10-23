# Contention and False Sharing

> **Want to learn more?** Read the mutex in [`internal/sync/mutex.go`](https://github.com/golang/go/blob/go1.27.0/src/internal/sync/mutex.go) (since Go 1.24, `sync.Mutex` is a thin wrapper around it), the semaphores it parks on in [`runtime/sema.go`](https://github.com/golang/go/blob/go1.27.0/src/runtime/sema.go), and the padding constants in [`internal/cpu/cpu.go`](https://github.com/golang/go/blob/go1.27.0/src/internal/cpu/cpu.go). The diagnostics guide covers the [mutex and block profiles](https://go.dev/doc/diagnostics#profiling).

Until now our benchmarks mostly ran on one core. Real servers run on all of them, and that's when a new kind of cost appears: cores **waiting for each other**. Sometimes it's obvious, like a mutex every goroutine needs. Sometimes it's invisible in the source: two goroutines writing two *different* variables that happen to live in the same 64-byte cache line, and that line bouncing between cores millions of times per second. In this module we take a shared counter, the simplest concurrent data structure there is, and make it scale from 1 to 32 threads.

## Learning Objectives

By the end of this module, you will:

- Know how `sync.Mutex` works: fast path, spinning, parking, normal vs starvation mode
- Understand why an atomic add on a shared variable doesn't scale either (cache coherence)
- Know what false sharing is, how to recognize it in a benchmark, and how to fix it with padding
- Shard a hot counter so that each core writes its own cache line
- Find contention with the mutex profile and the block profile

## The Concept: Cores Waiting for Each Other

### Inside `sync.Mutex`

Since Go 1.24, `sync.Mutex` just wraps `internal/sync.Mutex`. Its whole state is an `int32` plus a semaphore:

```go
const (
	mutexLocked = 1 << iota // mutex is locked
	mutexWoken
	mutexStarving
	mutexWaiterShift = iota
	...
	starvationThresholdNs = 1e6
)
```

The top bits count waiters. `Lock` is:

1. **Fast path**: one `CompareAndSwap` from 0 to `mutexLocked`. Uncontended, that's all a lock costs (~8 ns in our benchmark, two atomic operations per Lock/Unlock pair).
2. **Spin**: if the mutex is held, `lockSlow` may spin a few times (`runtime_canSpin`: at most `active_spin = 4` iterations of 30 `PAUSE` instructions, and only on a multicore machine with other running Ps and an empty local run queue), hoping the owner releases it soon.
3. **Park**: otherwise it increments the waiter count and calls `runtime_SemacquireMutex`. The goroutine is put to sleep on a semaphore in the runtime's `semTable` (251 buckets, each a treap of waiting goroutines guarded by a runtime lock) and the P runs something else. `Unlock` with waiters calls `runtime_Semrelease` to wake one.

The comment in `mutex.go` describes the two **fairness modes**:

> *"In normal mode waiters are queued in FIFO order, but a woken up waiter does not own the mutex and competes with new arriving goroutines over the ownership. [...] If a waiter fails to acquire the mutex for more than 1ms, it switches mutex to the starvation mode. In starvation mode ownership of the mutex is directly handed off from the unlocking goroutine to the waiter at the front of the queue."*

Normal mode is faster (a running goroutine can grab the lock again without a context switch), and starvation mode bounds the tail latency. Either way, under heavy contention, goroutines spend their time parking and waking up instead of doing work.

### Atomics and cache coherence

"Just use an atomic" removes the parking. But look at what the hardware does for `atomic.AddInt64` (a `LOCK XADD` on amd64). Every core has its own L1/L2 cache, and the caches are kept coherent with a protocol in the **MESI** family (Modified, Exclusive, Shared, Invalid; AMD uses MOESI, a variant). To *write* a cache line, a core must own it in the Modified/Exclusive state, which means **invalidating every other core's copy**. So when 32 cores do `Add(1)` on the same variable:

- core A gets the line, adds, and then core B asks for it
- the line moves (~tens to over a hundred cycles across cores, more across CCDs) to B, which adds, then C asks...

The operations are serialized by the cache line moving around. That's **cache-line ping-pong**. An atomic is faster than a mutex, but on a shared variable it's still a queue with one item served at a time.

### False sharing

The fix seems obvious: give every goroutine its own counter and sum them when reading. But coherence works on **cache lines** (64 bytes on amd64; `getconf LEVEL1_DCACHE_LINESIZE` prints 64 on the machine used here), not on variables. If your shards are a `[]atomic.Int64`, eight of them share one line. Different goroutines write different variables, but the hardware sees writes to the same line and ping-pongs it anyway. That's **false sharing**: the sharing exists only in the hardware.

![Two cores writing different counters on the same cache line bounce it back and forth; padding gives each counter its own line](images/15-false-sharing.webp)

The fix is **padding**: make each shard fill its own line.

```go
type paddedShard struct {
	n atomic.Int64
	_ [64 - 8]byte
}
```

Go's standard library does exactly this. `internal/cpu` defines `CacheLinePad` with `CacheLinePadSize = 64` on amd64 (128 on arm64 and ppc64, 256 on s390x), exported for everyone as `golang.org/x/sys/cpu.CacheLinePad`. And you already saw `sync.Pool` pad its per-P slots to 128 bytes in [sync.Pool](13-sync-pool.md). Some Intel CPUs prefetch pairs of adjacent lines, so 128 bytes is a common conservative choice. On this Zen 5 CPU we measured no difference between 64 and 128 bytes.

## The Lab

```
labs/15-contention-false-sharing/
  before.go      Counter interface + CounterBefore (sync.Mutex) (baseline, don't edit)
  steps.go       AtomicCounter and ShardedCounter: the intermediate steps (don't edit)
  after.go       your CounterAfter: make it scale
  solution.go    reference solution (built with -tags solution)
  lab_test.go    correctness test + benchmark
```

The counter is a classic "requests served" metric: `Inc` is called all the time from every goroutine, `Load` rarely (a metrics scrape). `Inc(worker int)` receives the caller's id, so a sharded implementation can pick a shard (the mutex and atomic versions ignore it). `BenchmarkInc` uses `b.RunParallel` and runs four implementations:

| `impl=` | What it is |
|---|---|
| `before` | one `int64` behind a `sync.Mutex` |
| `atomic` | one `atomic.Int64` |
| `sharded` | one `atomic.Int64` per shard, packed (`[]atomic.Int64`) |
| `after` | your version |

The shard count is `GOMAXPROCS` rounded up to a power of two.

### Step 1: Measure the baseline, at several core counts

Contention is about *how it scales*, so run the benchmarks at several `GOMAXPROCS` values with `CPU=` (it is passed to `go test -cpu`):

```bash
make bench LAB=15 CPU=1,2,4,8,16,32
```

With `RunParallel`, `ns/op` is wall time divided by the *total* number of increments across all goroutines. A perfectly scaling counter halves its ns/op every time you double `-cpu`.

### Step 2: Find the contention

```bash
cd labs
go test -run '^$' -bench 'Inc/impl=before' -mutexprofile mutex.out ./15-contention-false-sharing/
go tool pprof -top -cum mutex.out
```

```
      flat  flat%   sum%        cum   cum%
         0     0%     0%     34.73s 83.94%  labs/15-contention-false-sharing.(*CounterBefore).Inc
    23.88s 57.70% 57.70%     31.70s 76.61%  sync.(*Mutex).Unlock (inline)
    17.50s 42.29%   100%     17.50s 42.29%  runtime.unlock (inline)
         0     0%   100%      7.82s 18.91%  internal/sync.runtime_Semrelease
```

The mutex profile measures **delay**: 41 seconds of goroutines waiting in a benchmark that ran for about a second, because up to 32 goroutines were waiting at once. Delay is charged to the `Unlock` that ended the wait, i.e. to the code *holding* the lock. Note `runtime.unlock`: so much contention that the runtime's own semaphore table lock (in `sema.go`) is contended too. `go test` enables the profile for you. In a real program, call `runtime.SetMutexProfileFraction(n)` (sample 1 in n events) and read `/debug/pprof/mutex`. The related **block profile** (`-blockprofile`, `runtime.SetBlockProfileRate`) also covers channel operations, `select`, `sync.Cond` and `WaitGroup.Wait`.

The atomic and sharded versions don't show up in this profile at all: cache-line ping-pong isn't a lock, and no profile reports it directly. You find it the way we will in a moment: a benchmark that doesn't scale with `-cpu`. Or with hardware counters (`perf c2c` on Linux was built to find false sharing).

### Step 3: Fix it

Edit `labs/15-contention-false-sharing/after.go`:

1. Replace the mutex and `int64` with `shards []atomic.Int64` and `mask int`, sized with `numShards()` (copy `ShardedCounter` from `steps.go`). `Inc` does `c.shards[worker&c.mask].Add(1)`, and `Load` sums the shards.
2. Benchmark it. It's barely better than `atomic`. Why?
3. Pad each shard to 64 bytes (`struct { n atomic.Int64; _ [56]byte }`) and benchmark again.

### Step 4: Compare

```bash
make bench LAB=15 CPU=1,2,4,8,16,32      # your version
make solution LAB=15 CPU=1,2,4,8,16,32   # the reference
```

On an AMD Ryzen AI MAX+ 395 (16 cores, 32 threads, shared with other workloads while measuring):

```
        │     before     │                atomic                 │               sharded                │                after                │
        │     sec/op     │     sec/op      vs base               │    sec/op      vs base               │    sec/op     vs base               │
Inc         8.551n ±  1%     4.228n ±  1%  -50.56% (p=0.000 n=8)     4.385n ± 1%  -48.72% (p=0.000 n=8)    4.445n ± 1%  -48.01% (p=0.000 n=8)
Inc-2      11.840n ±  3%     6.692n ±  3%  -43.48% (p=0.000 n=8)     7.001n ± 8%  -40.87% (p=0.000 n=8)    2.213n ± 2%  -81.31% (p=0.000 n=8)
Inc-4      26.880n ±  3%     7.476n ± 12%  -72.19% (p=0.000 n=8)     7.118n ± 7%  -73.52% (p=0.000 n=8)    1.126n ± 1%  -95.81% (p=0.000 n=8)
Inc-8     47.0250n ± 13%    9.2840n ±  6%  -80.26% (p=0.000 n=8)   10.6300n ± 5%  -77.40% (p=0.000 n=8)   0.5766n ± 3%  -98.77% (p=0.000 n=8)
Inc-16    48.3200n ±  6%   11.1900n ±  7%  -76.84% (p=0.000 n=8)    6.1540n ± 7%  -87.26% (p=0.000 n=8)   0.3080n ± 2%  -99.36% (p=0.000 n=8)
Inc-32    51.1450n ±  3%   14.2600n ±  2%  -72.12% (p=0.000 n=8)    3.0060n ± 2%  -94.12% (p=0.000 n=8)   0.2083n ± 6%  -99.59% (p=0.000 n=8)
```

## Understanding the Results

Read each column from top to bottom: that's how it scales.

**Mutex (`before`) gets slower as you add cores**: 8.6 ns alone, 47 ns with 8 threads and 51 ns with 32. Adding cores makes the *total* throughput worse, because more and more time goes to spinning, parking and waking.

**Atomic** is 2x faster single-threaded (one atomic instead of two, no function calls), but it also gets *slower* with more cores: 4 ns → 14 ns. Every increment waits for the cache line to travel to its core.

**Sharded without padding is the interesting one.** Up to 8 threads, it's the same as atomic, or a bit worse. With 8 threads there are 8 shards × 8 bytes = **exactly one cache line**: it *is* a single shared variable, as far as the hardware is concerned. At 16 threads there are 2 lines, and at 32 there are 4, so it starts to improve, but only because fewer cores share each line. That's false sharing.

**Sharded and padded (`after`) scales perfectly**: 4.4 → 2.2 → 1.1 → 0.58 → 0.31 → 0.21 ns. Each doubling of threads halves the time per increment, up to the 16 physical cores (the last step to 32 is SMT, two threads per core, and gains less). Each core increments a line that stays in its own L1 cache. At 32 threads it's **250x faster than the mutex and 70x faster than the atomic**.

Your numbers depend on core count, CPU topology (cores on different CCDs are further apart) and whatever else runs on the machine, but the shape should be the same.

> **Trade-offs.** The padded counter uses 64 bytes per shard instead of 8, and `Load` is now O(shards) and not an atomic snapshot of all shards at the same instant. That's fine for metrics. For something like "reserve one of N tickets" it isn't. The general lesson: when something is hot and shared, find a way to make it *not shared* (per core, per goroutine, per request), and merge rarely.

### Where false sharing hides in real code

- Arrays of per-worker structs: `stats [N]struct{ hits, misses int64 }` that each worker updates
- A mutex next to the hot data it protects, or two hot mutexes next to each other in a struct
- A read-mostly field (config pointer) sitting next to a write-heavy counter: readers keep losing the line
- Struct layout matters here too ([Struct Layout and Size Classes](09-struct-layout.md)): `_ [56]byte` fields are the one time you *want* to waste space

## Key Takeaways

- `sync.Mutex`: CAS fast path → brief spinning → parking on a runtime semaphore. Starvation mode kicks in after a 1 ms wait
- Under contention, a mutex gets *slower* as you add cores
- Atomics avoid parking, but a single shared atomic still serializes on cache-line transfers
- Coherence works on 64-byte lines (on amd64): different variables in the same line still ping-pong. That's false sharing
- Shard hot writes per worker/core, pad shards to a cache line (`cpu.CacheLinePad`, or 64/128 bytes), and combine on read
- Find lock contention with the mutex/block profiles. Find cache-line contention with `-cpu` scaling benchmarks (and `perf c2c`)

## Going Further (optional)

1. Change `paddedShard` to 128 bytes. Does it make any difference on your CPU?
2. Give each shard a plain `int64` instead of `atomic.Int64` (each worker owns its shard). Run `go test -race`. Why does the race detector complain, even though only one goroutine writes each shard?
3. Run `perf c2c record` and `perf c2c report` on the `sharded` benchmark (Linux). Can you see the shared cache line?
4. Replace the mutex in `CounterBefore` with a `sync.RWMutex` and use `RLock` in `Load`. Does `Inc` get any faster? Why not?
