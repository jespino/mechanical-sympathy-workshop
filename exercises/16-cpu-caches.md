# CPU Caches and Data Locality

> **Want to learn more?** Memory distance doesn't stop at the caches. [NUMA](https://internals-for-interns.com/posts/linux-kernel-numa/) on Internals for Interns explains why, on larger machines, part of the RAM is slower to reach than the rest.

A loop can spend more time waiting for memory than doing arithmetic. CPU caches keep recently used data close to the processor. In this first module of Part IV, we will change how code visits data so it makes better use of those caches.

## Learning Objectives

By the end of this module, you will:

- Understand cache levels and cache lines
- Recognize the benefit of sequential memory access
- Compare slices of values with slices of pointers
- Choose a layout that matches the fields a loop reads

## The Concept: The Memory Hierarchy

### Small and fast, large and slow

L1 is a small cache close to each CPU core. L2 and L3 hold more data but take longer to access. RAM is larger again and slower. Exact sizes and timings depend on your machine.

![The memory hierarchy: each level is bigger and slower than the one above it](images/16-hierarchy.webp)

Try the latency demo. It reads data sets of increasing size so you can see access time rise as they stop fitting in each cache:

```bash
(cd labs && go run ./16-cpu-caches/cmd/latency)
```

### Use nearby data

Caches fetch chunks called **cache lines**, commonly 64 bytes. Reading adjacent values makes use of the bytes already fetched. Reusing the same data soon also helps because it may still be cached.

![Row-major traversal uses every byte of each cache line; column-major traversal touches a new cache line on every access](images/16-traversal.webp)

The CPU can often fetch upcoming data early when you visit memory in a predictable order. Following pointers to scattered objects makes that harder.

### Match the layout to the loop

`[]Account` keeps records together; `[]*Account` can point to records spread around memory. If a loop only needs balances, a separate `[]float64` of balances avoids reading unrelated account fields. Keeping one slice per field is called a **struct of arrays**.

That layout adds bookkeeping when inserting or updating a whole record. Use it for measured hot loops that read only a few fields, not automatically for every data type.

## The Lab

The data sets are deliberately large. `DailySales` totals sales per day from a table whose rows are stores and columns are days. The table is stored by row but read column by column. `PtrLedger` follows shuffled account pointers. `Ledger` reads balances from full account structs. We will improve each access pattern.

Run the commands from the repository root.

### Step 1: Measure the baseline

```bash
make bench LAB=16
```

### Step 2: Find the cost

Read the three loops in `before.go`. Ask: is the next value beside the current one, or somewhere else in memory? Does the loop use all the fields it loads?

Optional on Linux: after the fix, compare cache misses for the two versions.

> **Need `perf`?** It's Linux only. Install it with `sudo apt install linux-tools-common linux-tools-$(uname -r)` (Debian/Ubuntu) or `sudo dnf install perf` (Fedora). If `perf stat true` fails with a permissions error, run `sudo sysctl kernel.perf_event_paranoid=1`. On macOS, Windows, or a VM or container without hardware counters, skip this step.

```bash
(cd labs && go test -c -o caches.test ./16-cpu-caches/)
(cd labs && perf stat -e cache-misses ./caches.test -test.run '^$' -test.bench 'DailySales/impl=before' -test.benchtime 20x)
(cd labs && perf stat -e cache-misses ./caches.test -test.run '^$' -test.bench 'DailySales/impl=after' -test.benchtime 20x)
```

If counters are unavailable, use benchmark timings. Setup work is included in the counters, so they are supporting evidence rather than an exact count of misses in the loop.

### Step 3: Fix it

Edit `labs/16-cpu-caches/after.go`. Keep `before.go` as the baseline.

1. In `DailySalesAfter`, visit stores in the outer loop and days in the inner loop, adding each value to `sums[j]`. Keep each column's additions in their original order.
2. In `PtrLedgerAfter`, store `[]Account` instead of `[]*Account` and fill it directly in `NewPtrLedgerAfter`.
3. In `LedgerAfter`, use one slice per field. Update construction and make `Total` scan only the balances slice.

### Step 4: Check and compare

```bash
(cd labs && go test ./16-cpu-caches/)
make bench LAB=16
```

Reference solution (`make solution LAB=16`), on my machine:

| Benchmark | Before | After |
|---|---|---|
| `DailySales` | 77.889 ms | 8.286 ms |
| `PtrLedger` | 3.614 ms | 1.386 ms |
| `Ledger` | 1392 µs | 289 µs |

## Understanding the Results

**DailySales** is about 9x faster because it reads the sales table in storage order. Each fetched cache line supplies several useful values instead of one.

**PtrLedger** is about 2.6x faster because account values are together rather than reached through scattered pointers. **Ledger** is about 4.8x faster because it reads only balances instead of full records.

These timed scans don't allocate. The gains come from data access. Repeating a benchmark can also warm the cache, so try input sizes and access patterns that match real use.

## Key Takeaways

- Visit data in the order it is stored.
- Prefer contiguous values for large scans when the design allows it.
- Separate frequently read fields when that saves significant memory traffic.
- Benchmark realistic data sizes, not only a warm cache.

## Going Further (optional)

1. Set `matrixN` to `256`. How does a smaller sales table change the benefit of row order?
2. In an experimental copy, remove pointer shuffling from the baseline. Does `PtrLedger` get faster?
3. Compare the latency demo with the scan timings. Why are sequential scans less affected by individual memory-access latency?
