# Contention and False Sharing

More workers do not always mean more progress. If every goroutine updates the same counter, they compete for the same memory. We will compare a mutex, an atomic counter and separate counters per worker, then fix the hidden sharing between nearby counters.

## Content You May Want to Check First

- [Profiling](https://internals-for-interns.com/posts/go-runtime-profiling/#mutex-profiling-the-lock-contention-one) (article, Internals for Interns): how the runtime collects the mutex profile used in this lab.

## Learning Objectives

By the end of this module, you will:

- Recognize contention in a shared counter
- Understand why one atomic variable can still limit scaling
- Explain false sharing between nearby variables
- Split a counter into padded shards and measure across CPU counts

## The Concept: Cores Waiting for Each Other

### Waiting for a mutex

A mutex lets only one goroutine update the counter at a time. When many goroutines want it, they spend time waiting and being woken. The mutex profile shows this waiting.

### Why an atomic is not enough

An atomic increment avoids an explicit mutex, but updates to one variable still need to happen in a coordinated order. Each core has its own cache, and the cache line containing the counter has to move between cores that write it. This traffic can dominate a tiny increment.

### Different variables can still share memory

Giving every worker its own counter helps, but adjacent counters may occupy the same **cache line**. On the machine used by this lab, a line is 64 bytes, enough for eight `int64` counters. Different cores can interfere even though they write different variables. This is **false sharing**.

![Two cores writing different counters on the same cache line bounce it back and forth; padding gives each counter its own line](images/17-false-sharing.webp)

Separate the counters with padding so they occupy different lines:

```go
type paddedShard struct {
    n atomic.Int64
    _ [56]byte
}
```

This example assumes a 64-byte line. Other hardware may need different spacing. Padding uses more memory, so apply it to shared writes that measurements show are expensive.

## The Lab

`Inc(worker)` increments a counter and `Load` reads the total. The benchmark includes `before` (mutex), `atomic` (one shared atomic), `sharded` (packed per-worker counters) and `after` (your version). `steps.go` provides the intermediate implementations.

Run the commands from the repository root.

### Step 1: Measure the baseline

```bash
make bench LAB=17 CPU=1,2,4,8
```

### Step 2: Find the cost

```bash
(cd labs && go test -run '^$' -bench 'Inc/impl=before' -mutexprofile mutex.out ./17-contention-false-sharing/ && go tool pprof -top -cum mutex.out)
```

Look for time attributed to the counter's lock. The atomic versions may show no mutex waiting and still scale poorly: cache-line traffic is a separate cost.

### Step 3: Fix it

Edit `labs/17-contention-false-sharing/after.go`. Keep `before.go` as the baseline.

1. Import `sync/atomic` and start from the `ShardedCounter` design in `steps.go`. Allocate `numShards()` counters and set `mask` to the count minus one.
2. Use `paddedShard` instead of `atomic.Int64` as the slice element. Increment `c.shards[worker&c.mask].n` with `Add(1)`.
3. Implement `Load` by summing the shards with atomic loads. Keep atomics because reads can happen while workers are writing.

### Step 4: Check and compare

```bash
(cd labs && go test ./17-contention-false-sharing/)
make bench LAB=17 CPU=1,2,4,8
```

Reference solution (`make solution LAB=17 CPU=1,2,4,8`), on my machine:

| CPUs | Mutex | Shared atomic | Packed shards | Padded shards |
|---|---|---|---|---|
| 1 | 8.551 ns | 4.228 ns | 4.385 ns | 4.445 ns |
| 4 | 26.880 ns | 7.476 ns | 7.118 ns | 1.126 ns |
| 8 | 47.025 ns | 9.284 ns | 10.630 ns | 0.5766 ns |

## Understanding the Results

Read down each column to see scaling. The mutex and shared atomic get slower with more CPUs. Packed shards still interfere because they share cache lines. Padded shards let cores update separate memory with much less interference.

These are throughput timings: wall time divided by all completed increments, so values below one nanosecond don't mean each individual increment finishes that quickly.

The tradeoff is a larger counter and a slower `Load`, which must add all shards. A concurrent `Load` is not a single instant snapshot of every shard. This is useful for metrics, but not for decisions such as reserving the last available ticket.

## Key Takeaways

- A shared atomic can still be a bottleneck.
- False sharing happens when independent writes share a cache line.
- Split frequent writes and combine results less often.
- Measure scaling with several CPU counts, not just one.

## Going Further (optional)

1. Try 128-byte shards. Does your machine benefit from the extra space?
2. Compare `CPU=1,2,4,8,16` where available. When does scaling slow down?
3. In an experimental copy, replace the mutex with `sync.RWMutex`. Does it improve the increment-heavy workload?
