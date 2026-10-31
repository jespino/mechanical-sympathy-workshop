# Pre-allocation

We start with the simplest form of mechanical sympathy there is. If you already know how much space you need, ask for it up front. It sounds obvious, but `append` makes growing a slice look free, and that hides a surprising amount of work. In this module we look at what `append` does when it runs out of room, and then we remove that work.

## Learning Objectives

By the end of this module, you will:

- Know what `runtime.growslice` does and how Go picks the new capacity
- Understand why growing a slice costs allocations, copies *and* GC work
- Know how Go maps grow, and what a size hint in `make(map[K]V, n)` buys you
- Recognize `growslice` in a CPU profile and fix it with pre-allocation
- Know when an upper bound is good enough, and when pre-allocating backfires

## The Concept: What `append` Really Does

A slice is a three-word header: a pointer to a backing array, a length and a capacity.

```go
type slice struct {
    array unsafe.Pointer
    len   int
    cap   int
}
```

As long as `len < cap`, `append` just writes the element and bumps the length. It's cheap and it never touches the allocator. When `len == cap`, the compiler-generated code calls `runtime.growslice`, which:

1. **Computes a new capacity** with `nextslicecap`
2. **Allocates a new backing array** through `mallocgc`
3. **Copies every existing element** to the new array with `memmove`
4. **Leaves the old array behind** as garbage for the GC to find and free

### The growth formula

Here is `nextslicecap` from `runtime/slice.go`:

```go
const threshold = 256
if oldCap < threshold {
    return doublecap
}
for {
    // Transition from growing 2x for small slices
    // to growing 1.25x for large slices.
    newcap += (newcap + 3*threshold) >> 2
    if uint(newcap) >= uint(newLen) {
        break
    }
}
```

![Each time append runs out of capacity, growslice allocates a bigger array, copies everything and leaves the old one as garbage](images/03-growslice.webp)

Small slices double. Bigger slices grow by a factor that slides smoothly from 2x down toward 1.25x.

> **Note.** `growslice` also rounds the requested byte size **up to the next allocator size class** (we'll study size classes in [Struct Layout and Size Classes](10-struct-layout.md)). That's why the capacities you observe are not the exact output of the formula.

Let's watch it happen. The lab includes a small program that prints every reallocation:

```bash
cd labs
go run ./03-preallocation/cmd/growth
```

```
len=    1  cap     0 ->     4  (x4.00)
len=    5  cap     4 ->     8  (x2.00)
...
len=  257  cap   256 ->   512  (x2.00)
len=  513  cap   512 ->   848  (x1.66)
len=  849  cap   848 ->  1280  (x1.51)
len= 1281  cap  1280 ->  1792  (x1.40)
len= 1793  cap  1792 ->  2560  (x1.43)

appended 2000 ints (16000 bytes), copied 39520 bytes while growing
```

The program needed 16 KB of data, but along the way it moved **almost 40 KB** and made 12 allocations. Each one leaves an old array for the GC. Look at `512 -> 848`: the formula says 832, but 832 × 8 = 6656 bytes isn't a size class, so it was rounded up to the 6784-byte class, which fits 848 ints.

> **Amortized O(1) is not free.** Doubling makes `append` *amortized* constant time, so the total copying is bounded by a constant times the final size. That's an algorithmic guarantee, not a performance one. The constant still includes allocations, copies, cache misses on the fresh memory, and GC work.

### Maps grow too

Maps have the same problem. Since Go 1.24, a Go map is a **Swiss Table**: its entries live in small groups of slots, organized in tables. When a map gets too full (about 7/8 of its slots), it grows: it allocates bigger tables and **re-inserts every entry** into them.

![A Swiss Table map: a directory of tables, each table made of groups of 8 slots. Growing means rehashing and re-inserting every entry](images/03-swiss.webp)

A map you fill from empty with 10,000 entries goes through that several times.

`make(map[K]V, hint)` tells the runtime how many entries to expect, so it can allocate enough room up front. The hint is allocated whether you use it or not, so a hint that's far too big wastes memory.

## The Lab

```
labs/03-preallocation/
  before.go      naive implementations (baseline, don't edit)
  after.go       your copy: make this one fast
  solution.go    reference solution (built with -tags solution)
  lab_test.go    correctness test + benchmarks
  cmd/growth/    the demo program above
```

There are three functions, each a different flavour of the same problem:

| Function | What you know about the final size |
|---|---|
| `Squares(n)` | Exactly `n` |
| `Evens(nums)` | At most `len(nums)` |
| `CountWords(words)` | At most `len(words)` distinct keys |

### Step 1: Measure the baseline

From the repository root:

```bash
make bench LAB=03
```

This runs every benchmark 6 times for both `impl=before` and `impl=after`, saves the raw output to `labs/03.bench.txt`, and prints a `benchstat` comparison with one column per implementation. Right now `after.go` is a copy of `before.go`, so the two columns should be statistically identical (`~`). That's your noise floor, so look at the `±` values to see how noisy your machine is.

### Step 2: Find the cost in a profile

Benchmarks tell you *how much*, and profiles tell you *where*:

```bash
cd labs
go test -run '^$' -bench 'Squares/impl=before' -cpuprofile cpu.prof ./03-preallocation/
go tool pprof -top -cum cpu.prof | grep -E 'growslice|mallocgc|gcBgMarkWorker|Squares'
```

```
0.17s  7.87%  7.87%   0.99s 45.83%  labs/03-preallocation.SquaresBefore
0.01s  0.46%  8.33%   0.82s 37.96%  runtime.growslice
    0     0%  8.33%   0.72s 33.33%  runtime.gcBgMarkWorker
    0     0% 29.17%   0.28s 12.96%  runtime.mallocgc
    0     0% 77.78%   0.03s  1.39%  runtime.mallocgcSmallNoScanSC6
```

More than **80% of `SquaresBefore`'s time is spent inside `growslice`**, not computing squares. And a third of the whole process is the GC's background mark workers cleaning up the abandoned arrays. That cost doesn't even show up under your function's name.

Try `go tool pprof -http=: cpu.prof` and open the flame graph (*View → Flame Graph*) to see the same thing visually:

![The pprof flame graph of SquaresBefore: growslice takes most of SquaresBefore, and gcBgMarkWorker is a big block of its own](images/03-flame-graph.webp)

Under `SquaresBefore`, almost everything is `runtime.growslice`. The big block next to it, `runtime.gcBgMarkWorker`, is the GC cleaning up after it.

### Step 3: Fix it

Edit `labs/03-preallocation/after.go`:

1. **`SquaresAfter`**: the size is exactly `n`. Allocate once with `make([]int, n)` and write by index (or `make([]int, 0, n)` and keep using `append`).
2. **`EvensAfter`**: you don't know the exact size, but `len(nums)` is an upper bound. Use `make([]int, 0, len(nums))`.
3. **`CountWordsAfter`**: give the map a size hint with `make(map[string]int, len(words))`.

`make test-labs` (or `go test ./03-preallocation/`) checks that the results didn't change.

### Step 4: Compare

```bash
make bench LAB=03
```

Here are the results with the reference solution (`make solution LAB=03`) on my machine. Your numbers will differ, but the shape should be the same:

```
              │    before     │                after                │
              │    sec/op     │    sec/op     vs base               │
Squares-32      28.684µ ± 30%   6.690µ ± 13%  -76.68% (p=0.002 n=6)
Evens-32        13.415µ ±  6%   7.069µ ±  7%  -47.31% (p=0.002 n=6)
CountWords-32    252.8µ ± 17%   152.6µ ± 10%  -39.63% (p=0.002 n=6)

              │    before     │                after                │
              │     B/op      │     B/op      vs base               │
Squares-32      349.19Ki ± 0%   80.00Ki ± 0%  -77.09% (p=0.002 n=6)
Evens-32        125.19Ki ± 0%   80.00Ki ± 0%  -36.10% (p=0.002 n=6)
CountWords-32    426.4Ki ± 0%   426.7Ki ± 0%   +0.06% (p=0.002 n=6)

              │   before    │               after               │
              │  allocs/op  │ allocs/op   vs base               │
Squares-32      16.000 ± 0%   1.000 ± 0%  -93.75% (p=0.002 n=6)
Evens-32        13.000 ± 0%   1.000 ± 0%  -92.31% (p=0.002 n=6)
CountWords-32    48.00 ± 0%   34.00 ± 0%  -29.17% (p=0.002 n=6)
```

## Understanding the Results

**Squares** is the clean case: 16 allocations become 1, and 349 KB of total allocation becomes exactly the 80 KB we need (10,000 × 8 bytes, rounded to whole pages).

**Evens** is interesting. Only half of the numbers are even, so the *exact* answer needs 40 KB, and we allocated 80 KB. We traded memory for speed: one allocation instead of 13, and no copying. That's usually a good trade when the upper bound is close to the real size. It's a bad trade when the upper bound is wildly pessimistic, for example reserving `len(input)` for a filter that keeps 0.1% of it.

**CountWords** has the same twist with maps. There are only 5,000 distinct words in 10,000 entries, so the hint over-allocates by 2x. That's why the B/op column didn't move at all: the bytes we saved by not growing were spent on slots we never used. The time still dropped 40%, because no rehashing happened. Why are there still 34 allocations? The keys are already-allocated strings, so those aren't new allocations. What's left is the directory, the tables and the groups of a big map, allocated once each.

> **Rule of thumb:** pre-allocate when you know the size exactly, or have an upper bound that is usually close. If the bound can be off by orders of magnitude, use a reasonable estimate, or let `append` do its job.

## Beyond Slices and Maps

The same idea shows up all over the standard library:

- `strings.Builder.Grow(n)` and `bytes.Buffer.Grow(n)`: pre-size a buffer you're about to fill (more in [Strings and Bytes](04-strings-bytes.md))
- `slices.Grow(s, n)`: make sure `s` has room for `n` more elements without changing its length
- `append(dst[:0], ...)`: reuse an existing backing array instead of allocating a new one
- `slices.Collect` and `maps.Collect` on an iterator can't know the size, while `make` + loop can

## Key Takeaways

- `append` on a full slice calls `growslice`: **allocate, copy everything, leave garbage behind**
- Growth is 2x for small slices, sliding toward 1.25x for large ones, rounded up to a size class
- Maps grow too, and every time they do, they re-insert all their entries
- A known size, or a tight upper bound, turns N allocations into 1
- The benefit shows up in **allocs/op**, **sec/op**, and in GC time that is charged to other goroutines
- An over-generous hint wastes memory, so measure B/op and not just sec/op

## Going Further (optional)

1. Change `size` in `lab_test.go` to `100` and to `1_000_000`. How does the speedup change? Why?
2. In `EvensAfter`, try `slices.Clip` on the result. What does it cost, and when would you want it?
3. Run the benchmarks with `GODEBUG=gctrace=1` and compare how many GC cycles each implementation triggers.
