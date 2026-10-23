# CPU Caches and Data Locality

> **Want to learn more?** Ulrich Drepper's [What Every Programmer Should Know About Memory](https://people.freebsd.org/~lstewart/articles/cpumemory.pdf) is the classic (long, but sections 3 and 6 are the core). Scott Meyers' talk [CPU Caches and Why You Care](https://www.youtube.com/watch?v=WDIkqP4JbkE) is the one-hour version. For Go specifically, the runtime's own data structures are full of cache-conscious tricks: look for `cpu.CacheLinePad` in [`internal/cpu/cpu.go`](https://github.com/golang/go/blob/go1.27.0/src/internal/cpu/cpu.go).

In the previous module, a struct got 43% smaller and a loop over it got 45% faster without changing a line of the loop. That's because the loop wasn't limited by the CPU: it was waiting for memory. In this module we look at the machinery between the CPU and RAM, the cache hierarchy, and at three access patterns that either work with it or fight it.

## Learning Objectives

By the end of this module, you will:

- Know the levels of the memory hierarchy and their latencies, measured on your own machine
- Understand cache lines, spatial locality and temporal locality
- Know what the hardware prefetcher does, and why pointer chasing defeats it
- Fix three classic cache-hostile patterns: column-major traversal, `[]*T` and array-of-structs with one hot field
- Use `perf stat` to count cache misses (Linux)

## The Concept: The Memory Hierarchy

A modern core can execute several instructions per nanosecond. DRAM needs about 100 ns to answer a request for an address it hasn't seen recently. Caches bridge that gap: small, fast memories that keep copies of recently used data close to the core.

![The memory hierarchy: each level is bigger and slower than the one above it](images/10-hierarchy.webp)

You can read your machine's cache sizes with `lscpu` or from sysfs:

```bash
lscpu | grep -i cache
grep . /sys/devices/system/cpu/cpu0/cache/index*/{level,type,size,coherency_line_size}
```

On the AMD Ryzen AI MAX+ 395 used for this workshop (16 cores, 32 threads):

| Level | Size | Shared by | Line size |
|---|---|---|---|
| L1 data | 48 KB | 1 core | 64 B |
| L2 | 1 MB | 1 core | 64 B |
| L3 | 32 MB | 8 cores (one CCD; two CCDs in total) | 64 B |
| DRAM | 64 GB | everyone | |

On macOS, `sysctl hw.l1dcachesize hw.l2cachesize hw.cachelinesize` gives you the same information.

### Measuring latency

Sizes are only half the story. The lab includes a program that measures the **latency** of one load for working sets of increasing size. It follows a random cycle of pointers, where each load's address depends on the previous load, so the CPU can't overlap or predict anything:

```bash
cd labs
go run ./10-cpu-caches/cmd/latency
```

```
working set      16 KB:    1.0 ns per load
working set      32 KB:    1.0 ns per load
working set      64 KB:    3.0 ns per load
working set     256 KB:    3.0 ns per load
working set     512 KB:    3.4 ns per load
working set    1024 KB:    5.5 ns per load
working set    2048 KB:   10.8 ns per load
working set    8192 KB:   11.2 ns per load
working set   16384 KB:   13.5 ns per load
working set   32768 KB:   80.5 ns per load
working set   65536 KB:  134.8 ns per load
working set  262144 KB:  157.0 ns per load
```

You can read the hierarchy straight off the numbers: ~1 ns while everything fits in L1 (48 KB), ~3 ns in L2, ~10-14 ns in L3, and then a cliff to **80-160 ns** once the working set is bigger than the 32 MB L3. (At the large sizes, part of the cost is also TLB misses: the CPU's cache of virtual-to-physical address translations runs out too.) A DRAM access costs as much as **150 L1 hits** or more.

### Cache lines

Caches don't move bytes, they move **cache lines**: 64 aligned bytes on every current x86 and most ARM cores (Apple's M-series uses 128-byte lines). Read one `int64` and you pay for the whole line it's in. Two consequences:

- **Spatial locality**: if your next access is in the same line, it's free. Reading a `[]int64` sequentially costs one miss per 8 elements.
- **Temporal locality**: if you touch the same data again soon, it's still in the cache. If you touch a lot of other data in between, it was evicted.

![Row-major traversal uses every byte of each cache line; column-major traversal touches a new cache line on every access](images/10-traversal.webp)

### The hardware prefetcher

The CPU watches your misses. When it sees a pattern, like consecutive lines or a constant stride, it starts fetching lines **before you ask for them**, so a sequential scan over a slice runs close to memory **bandwidth** (tens of GB/s) instead of paying full **latency** on every line.

What prefetchers can't do is guess the target of a pointer they haven't loaded yet. In `for _, p := range ptrs { sum += p.X }`, the address of the next `p.X` is in memory, so the prefetcher can't see it coming. That's **pointer chasing**, and it's what the latency program above does on purpose.

> **Where does Go come in?** The Go compiler doesn't reorder your loops or your data structures for cache locality, and it never turns a `[]*T` into a `[]T`. That's on you. The good news is that Go gives you value types, contiguous slices and control over struct layout, which is everything you need.

## The Lab

```
labs/10-cpu-caches/
  before.go      cache-hostile implementations (baseline, don't edit)
  after.go       your copy: make it cache friendly
  solution.go    reference solution (built with -tags solution)
  lab_test.go    correctness test + benchmarks
  cmd/latency/   the latency program above
```

All data sets are bigger than L3 on purpose, so the effects are visible:

| Benchmark | Data | Problem |
|---|---|---|
| `ColumnSums` | 4096×4096 `float64` matrix, row-major (128 MB) | walks it column by column: every access is 32 KB from the previous one |
| `PtrLedger` | 1M × `*Account` (64 MB of accounts), shuffled | every access follows a pointer to a random cache line |
| `Ledger` | 1M × `Account` (64 MB), array of structs | needs 8 bytes of each 64-byte account, loads all 64 |

### Step 1: Measure the baseline

```bash
make bench LAB=10
```

### Step 2: Count the misses (optional, Linux only)

> **perf is Linux-only.** On macOS and Windows, skip this step: the benchmarks alone tell the story. On macOS, Instruments' *CPU Counters* template can show similar hardware counters if you want to dig further.

`perf stat` reads the CPU's hardware performance counters while your program runs. Many distributions (Ubuntu and Debian included) ship with `kernel.perf_event_paranoid` set to 4, which blocks unprivileged users. If `perf stat true` says *"No supported events found"* or *"Access to performance monitoring … is limited"*, lower it until the next reboot:

```bash
sudo sysctl kernel.perf_event_paranoid=1
```

Compile the test binary once, then run one benchmark at a time under `perf stat`. Use a fixed iteration count (`-test.benchtime 20x`) so the two runs do the same amount of work:

```bash
cd labs
go test -c -o caches.test ./10-cpu-caches/
perf stat -e cache-references,cache-misses,L1-dcache-loads,L1-dcache-load-misses \
    ./caches.test -test.run '^$' -test.bench 'ColumnSums/impl=before' -test.benchtime 20x
perf stat -e cache-references,cache-misses,L1-dcache-loads,L1-dcache-load-misses \
    ./caches.test -test.run '^$' -test.bench 'ColumnSums/impl=after' -test.benchtime 20x
```

*(Run both and compare. Write your numbers here.)*

What to look for:

- `L1-dcache-loads` should be about the same in both runs: the loop does the same number of loads.
- `L1-dcache-load-misses` and `cache-misses` should drop sharply for `impl=after`. Column-major order misses on almost every load; row-major misses about once per 8 `float64`s, and the prefetcher hides most of those.
- The counters include the test setup (building a 128 MB matrix), which is the same for both runs. Raise `-test.benchtime` if it drowns out the difference.

Event names depend on the CPU. `cache-references`/`cache-misses` are generic aliases (on AMD they map to L2/L3 events, not exactly "last-level cache"). Run `perf list cache` to see what yours supports.

### Step 3: Fix it

Edit `labs/10-cpu-caches/after.go`:

1. **`ColumnSumsAfter`**: swap the loops. Walk row by row and accumulate into `sums[j]`. The `sums` slice is only 32 KB, so it stays in L1 for the whole run.
2. **`PtrLedgerAfter`**: store `[]Account` instead of `[]*Account`, and fill it directly in `NewPtrLedgerAfter`.
3. **`LedgerAfter`**: turn the array of structs into a **struct of arrays**: one slice per field (`ids`, `balances`, `created`, …), and make `Total` range over `balances` only.

`go test ./10-cpu-caches/` checks the results are the same. (In `ColumnSums`, each column is still summed in the same order, so the floating-point results are bit-for-bit identical.)

### Step 4: Compare

```bash
make bench LAB=10
```

Reference solution (`make solution LAB=10`) on an AMD Ryzen AI MAX+ 395:

```
              │    before    │               after                │
              │    sec/op    │   sec/op     vs base               │
ColumnSums-32   77.889m ± 5%   8.286m ± 6%  -89.36% (p=0.002 n=6)
PtrLedger-32     3.614m ± 5%   1.386m ± 4%  -61.63% (p=0.002 n=6)
Ledger-32       1392.0µ ± 3%   289.0µ ± 5%  -79.24% (p=0.002 n=6)
```

Note the B/op and allocs/op columns: nothing changed there. Every one of these wins is pure memory access pattern.

## Understanding the Results

**ColumnSums: 9x faster** with the same number of additions. Column-major order reads one `float64` per 64-byte line and moves 32 KB forward each time, crossing a 4 KB page boundary on every access. By the time the next column comes around, those lines are long gone from cache, so we pay for every line of the 128 MB matrix **8 times**. Row-major order uses all 8 values of each line, and the prefetcher streams the matrix at full bandwidth.

**PtrLedger: 2.6x faster.** Same accounts, same loop, same result. With pointers in random order, each `a.Balance` is a dependent load to an unpredictable line, and the prefetcher can't help. Out-of-order execution still overlaps several of these loads (the addresses come from the pointer slice, which *is* sequential), which is why we lose "only" 2.6x and not the full 100x of the latency program. In a real program, pointer-heavy structures are scattered by allocation order, GC and time: `[]*T`, linked lists, trees, `map[K]*V`.

**Ledger: 4.8x faster.** The array-of-structs loop loads 64 MB to use 8 MB. The struct-of-arrays loop loads only the 8 MB of balances. That's 8x less data, and there's a bonus: 8 MB fits in the 32 MB L3, so after the first iteration the benchmark mostly reads from L3 instead of DRAM. Keep this in mind when you read benchmarks: **a benchmark that runs the same function in a loop over the same data may be measuring a warm cache that production never sees.**

> **When is struct-of-arrays worth it?** When a hot loop touches a few fields of many elements, and you can live with the ergonomics: adding an element means appending to every slice, and you lose `&accounts[i]`. Code that usually handles one whole record at a time (most business logic) is better off with an array of structs. The common middle ground is to split a struct into a small "hot" struct and a "cold" one that the hot one points to.

## Key Takeaways

- L1 ~1 ns, L2 ~3 ns, L3 ~10-15 ns, DRAM ~100+ ns: a cache miss to RAM costs ~100 L1 hits
- Memory moves in **64-byte cache lines**: use every byte you pay for
- Sequential access is fast because the **prefetcher** can predict it; strided and random access is not
- Traverse multi-dimensional data in the order it's stored (row-major in Go)
- Prefer `[]T` over `[]*T` for data you scan: pointers scatter it and defeat the prefetcher
- When a loop needs one field of many records, struct-of-arrays reads a fraction of the memory
- `perf stat -e cache-misses,L1-dcache-load-misses` gives you hard evidence on Linux

## Going Further (optional)

1. In `lab_test.go`, set `matrixN` to `256` (512 KB, fits in L2). How much does column order cost now?
2. Remove the `Shuffle` from `NewPtrLedgerBefore` and rerun. Why does `[]*Account` get much closer to `[]Account` when the pointers are in allocation order? Would that last in a long-running program?
3. Run `cmd/latency` pinned to one core (`taskset -c 2 go run ./10-cpu-caches/cmd/latency`) and on a busy machine. What changes?
