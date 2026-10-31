# Helping the Garbage Collector

Reducing allocations helps the garbage collector, but the data we keep alive matters too. A large collection of objects linked by pointers takes work to scan on every GC cycle. We will change a store layout to reduce that repeated work.

## Learning Objectives

By the end of this module, you will:

- Understand why live objects with pointers add GC work
- Use values and indexes to reduce pointer-heavy storage
- Read the main information in a GC trace
- Explore the memory-versus-CPU tradeoff of `GOGC` and `GOMEMLIMIT`

## The Concept: What the GC Actually Does

### What gets scanned

The GC starts from values the program can still reach, such as local variables and globals. It follows pointers to find live objects and reclaims objects that are no longer reachable. Much of this happens while your program runs, so short pauses don't mean the GC uses little CPU.

![The GC follows every pointer in the heap; pointer-free memory is never scanned](images/12-mark.webp)

A `[]*Record` contains pointers to many separate records. String fields also point to their bytes. The GC must inspect those references on each cycle.

Memory containing no pointers, such as a `[]byte` or `[]float64` backing array, doesn't need its contents scanned. This is called **noscan** memory. The object still needs to be kept alive, but the GC doesn't follow references inside it.

### Fewer pointers, less repeated work

Prefer `[]Record` to `[]*Record` when separate object identity isn't needed. An integer index can replace a pointer into that slice. Text can be stored in one byte slice with offsets and lengths instead of many individual strings.

These changes make the code more involved. They are useful for large, long-lived data that profiles show is costly to scan, not for every struct.

### Memory and CPU settings

![The heap goal is the live heap plus GOGC percent; a GC cycle starts as the heap approaches the goal](images/12-pacer.webp)

`GOGC` controls how much the heap can grow relative to live data. A larger value usually uses more memory and runs GC less often; a smaller value does the opposite. The default is 100.

`GOMEMLIMIT` is a **soft limit** on memory managed by the Go runtime, not a hard cap on process memory. Set it too close to the live data size and the GC can spend a lot of CPU trying to make room.

Modern Go also improves scanning with the Green Tea collector. Better data layout still matters because it avoids work instead of just making scanning faster.

## The Lab

`StoreBefore` keeps two million user records using `[]*Record` and `map[int64]*Record`. `GC` measures a collection while the store stays alive, `Churn` adds temporary allocations beside it, and `AverageScore` scans the data. Building the store makes this lab slower to run than earlier ones.

Run the commands from the repository root.

### Step 1: Measure the baseline

```bash
make bench LAB=12
```

### Step 2: Find the cost

```bash
(cd labs && go test -c -o gc.test ./12-gc-pressure/ && GODEBUG=gctrace=1 ./gc.test -test.run '^$' -test.bench 'GC/impl=before' -test.benchtime 3x)
```

Each `gc` line is one collection. In `286->286->286 MB`, the last number is the live heap. The three numbers before `ms clock` show the short pause, concurrent marking time and final pause. The `ms cpu` values include work across cores, so they can be much larger than elapsed time.

### Step 3: Fix it

Edit `labs/12-gc-pressure/after.go`. Keep `before.go` as the baseline.

1. Store names and emails in one `text []byte`. Keep their offsets and lengths in `recordAfter`; rebuild the name in `Lookup` with `string(s.text[off:off+uint32(length)])`.
2. Store records by value in `[]recordAfter`, instead of keeping a slice of pointers.
3. Change `byID` to `map[int64]int32`, mapping each ID to its record index. Update construction, `Lookup` and `AverageScore` to use the new layout. Measure `BENCH=GC` after each change.

### Step 4: Check and compare

```bash
(cd labs && go test ./12-gc-pressure/)
make bench LAB=12
```

Reference solution (`make solution LAB=12`), on my machine:

| Benchmark | Before | After |
|---|---|---|
| `GC` | 22.77 ms | 0.387 ms |
| `Churn` | 1367.5 ns | 819.4 ns |
| `AverageScore` | 7.627 ms | 1.409 ms |

## Understanding the Results

The full GC is about 59x faster even though the store still uses a lot of memory. The main change is the number of pointers the GC must follow, not just the heap size.

`Churn` also improves because its temporary allocations no longer trigger expensive scans of the old store. `AverageScore` gets faster because records are next to each other in memory.

The tradeoff is more bookkeeping. Updating or deleting text is harder, and `Lookup` now allocates the returned string. Use this layout where repeated GC scanning is a measured problem.

## Try the GC settings

The demo reports GC cycles, GC CPU, elapsed time and peak memory:

```bash
(cd labs && GOGC=50 go run ./12-gc-pressure/cmd/gctune)
(cd labs && GOGC=200 go run ./12-gc-pressure/cmd/gctune)
(cd labs && GOGC=off GOMEMLIMIT=400MiB go run ./12-gc-pressure/cmd/gctune)
```

More memory can reduce how often GC runs. Reducing pointers makes each cycle cheaper; it is a different kind of improvement.

## Key Takeaways

- Live pointers add scanning work on every GC cycle.
- Values, indexes and shared text storage can reduce that work.
- `GOGC` trades memory for collection frequency; `GOMEMLIMIT` is a soft limit.
- Measure GC CPU and normal application work, not just pause times.

## Going Further (optional)

1. Run the tuning demo with `GOGC=50`, `100` and `200`. Compare memory use and GC CPU.
2. Try a lower `GOMEMLIMIT`. Does the GC need more CPU to stay near it?
3. Add a pointer field to `recordAfter`. How does the `GC` benchmark change?
