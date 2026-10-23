# Benchmarking 101

> **Want to learn more?** Read [`testing/benchmark.go`](https://github.com/golang/go/blob/go1.27.0/src/testing/benchmark.go) (look for `launch`, `predictN`, `runN` and `Loop`) and the compiler pass that makes `b.Loop` work, [`cmd/compile/internal/bloop/bloop.go`](https://github.com/golang/go/blob/go1.27.0/src/cmd/compile/internal/bloop/bloop.go).

Every module in this workshop ends with the same sentence: *measure it*. So before we optimize anything, we need a measuring tool we can trust. Go ships a good one in `testing`, but it's surprisingly easy to write a benchmark that compiles, runs, prints a believable number... and measures nothing at all. In this module we look at how `go test -bench` actually runs your code, how the compiler can delete the code you wanted to measure, and how to compare results without fooling yourself.

## Learning Objectives

By the end of this module, you will:

- Know how `testing.B` picks `b.N` and how many times it calls your benchmark function
- Understand what `b.Loop` does (and doesn't) guarantee, down to the compiler pass that implements it
- Recognize the three classic lies: dead-code elimination, constant folding and a benchmark that changes its own input
- Know what `-benchmem`, `-count`, `-cpu` and `-benchtime` do, and what `ResetTimer` / `StopTimer` really cost
- Use `benchstat` to compare runs with statistics instead of eyeballing

## The Concept: How `go test -bench` Runs Your Code

### The `b.N` ramp-up

The classic benchmark is a loop to `b.N`:

```go
func BenchmarkFoo(b *testing.B) {
    for i := 0; i < b.N; i++ {
        Foo()
    }
}
```

![How go test grows b.N until a run lasts long enough to measure](images/01-bn-ramp.webp)

You don't choose `b.N`. The framework runs your function with `b.N = 1`, measures how long it took, and predicts how many iterations it needs to fill the target time (`-benchtime`, 1s by default). Here's `predictN` from `testing/benchmark.go`:

```go
n := goalns * prevIters / prevns
// Run more iterations than we think we'll need (1.2x).
n += n / 5
// Don't grow too fast in case we had timing errors previously.
n = min(n, 100*last)
// Be sure to run at least one more than last time.
n = max(n, last+1)
// Don't run more than 1e9 times. (This also keeps n in int range on 32 bit platforms.)
n = min(n, maxBenchPredictIters)
```

`launch` calls it in a loop until the measured duration reaches the target. Every round goes through `runN`, which calls `runtime.GC()` first ("by clearing garbage from previous runs"), resets the timer and calls your function **from the top**. You can watch it happen:

```
BenchmarkCallsN called with b.N = 1
BenchmarkCallsN called with b.N = 100
BenchmarkCallsN called with b.N = 10000
BenchmarkCallsN called with b.N = 1000000
BenchmarkCallsN called with b.N = 100000000
BenchmarkCallsN called with b.N = 1000000000
```

Two consequences. The **reported number is from the last round only** (`ns/op = duration / b.N`). And any setup you do before the loop runs **six times**, and is timed unless you call `b.ResetTimer()` after it. Forgetting that can be dramatic: a map lookup benchmark that first builds a 2-million-entry map reported 13–19 ns/op without `ResetTimer` and 8.6–10.7 ns/op with it (AMD Ryzen AI MAX+ 395). Same code, same lookup.

### `b.Loop` (Go 1.24+)

Go 1.24 added a better way to write the loop:

```go
func BenchmarkFoo(b *testing.B) {
    // setup: not measured
    for b.Loop() {
        Foo()
    }
}
```

`Loop` does its own ramp-up *inside a single call* of your function. `loopSlowPath` resets the timer on the first call (so setup is never measured) and calls `stopOrScaleBLoop`, which reuses `predictN`, every time the current target is reached. `launch` notices that `b.loop.n` is set and doesn't call your function again:

```
BenchmarkCallsLoop called with b.N = 1
  after loop b.N = 1000000000
```

The fast path is small enough to be inlined into your benchmark:

```go
func (b *B) Loop() bool {
    if b.loop.i < b.loop.n {
        b.loop.i++
        return true
    }
    return b.loopSlowPath()
}
```

Note that `b.N` is set to 0 while the loop runs (so don't use it inside), and gets the total iteration count once `Loop` returns false.

### Dead-code elimination: the benchmark that measures nothing

Here's the problem `b.Loop` was really designed to solve. If `Foo` is small, the compiler inlines it. If its result is never used and it has no side effects, the compiler deletes it. What's left is an empty loop. This is the disassembly of a `b.N` loop around `PopCount(uint64(i))` from the lab (`go tool objdump`):

```
before_test.go:14   XORL CX, CX
before_test.go:14   JMP 0x544167
before_test.go:14   INCQ CX
before_test.go:14   CMPQ 0x210(AX), CX     // i < b.N
before_test.go:14   JG 0x544164
before_test.go:17   RET
```

![Dead-code elimination: the compiler removes work whose result is never used](images/01-dce.webp)

No popcount anywhere. The benchmark reports **0.2 ns/op**, which is about one clock cycle per iteration on this CPU. That number is your alarm bell: almost nothing useful takes less than a nanosecond.

### What `b.Loop` actually guarantees

The `Loop` documentation says:

> Within the body of a "for b.Loop() { ... }" loop, arguments to and results from function calls and assigned variables within the loop are kept alive, preventing the compiler from fully optimizing away the loop body. Currently, this is implemented as a compiler transformation that wraps such variables with a runtime.KeepAlive intrinsic call.

That transformation lives in `cmd/compile/internal/bloop`. It runs **before inlining** (see `gc/main.go`: `bloop.Walk` is called right before `DevirtualizeAndInlinePackage`), and it rewrites every statement in the loop body. For a call with results, it assigns them to temporaries and keeps those alive:

```go
// preserveCallResults assigns the results of a call statement to temporary variables
// to ensure they remain alive.
```

You can see it with `-gcflags=-m=2`:

```
./before_test.go:23:6: function result will be kept alive
```

Three things are worth knowing, because they are *not* what most blog posts say:

1. **In Go 1.27, calls inside `b.Loop` are still inlined.** (The first implementation, in Go 1.24, kept the body alive by refusing to inline calls in it; the current compiler uses the KeepAlive rewrite instead.) `-gcflags=-m` prints `inlining call to Mix` for the call inside the `b.Loop` body.
2. **Keeping the result alive doesn't keep the computation alive.** If the inputs are constants, the inlined function is *constant-folded*, and KeepAlive happily keeps a constant alive. In the lab, `for b.Loop() { Mix(42) }` compiles to:

   ```
   before_test.go:23   MOVQ $0xa759ea27d4727622, CX   // Mix(42), computed at compile time
   before_test.go:23   MOVQ CX, 0x8(SP)               // the KeepAlive
   ```

   The same happens with `v := uint64(42)` declared before the loop: the compiler propagates the constant.
3. **It only applies to statements syntactically inside the braces**, and the condition must be literally `b.Loop()`. And it changes your code: assigned variables are kept alive, which can disable optimizations you meant to measure. For example, `k := string(buf); _ = m[k]` doesn't allocate in a normal function (the compiler reuses `buf`'s bytes for the lookup), but it allocates **48 B/op inside a `b.Loop` body**, because `k` must now be kept alive. When in doubt, put the code under test in a function and call it from the loop.

### The benchmark that changes its own input

The compiler isn't the only one who can lie to you. If the operation mutates its input, every iteration after the first measures a different thing. Sorting a slice in place is the classic: from the second iteration on, you are timing `slices.Sort` on already-sorted data, which pdqsort detects and finishes in linear time. The fix is to restore the input every iteration (`copy` from a pristine slice) and check that the restore is cheap compared to the operation.

What about `b.StopTimer()` / `b.StartTimer()` around the restore? They work, but look at what they do:

```go
func (b *B) StopTimer() {
    if b.timerOn {
        b.duration += highPrecisionTimeSince(b.start)
        runtime.ReadMemStats(&memStats)
        ...
```

`runtime.ReadMemStats` **stops the world**. Calling it twice per iteration is fine for a 400 µs sort, but terrible for fast operations: a ~1 ns `PopCount` wrapped in `StopTimer`/`StartTimer` reported 27.51 ns/op, and 1,000,000 iterations took 16.3 s of wall time. With the default `-benchtime=1s`, the same benchmark ran for **more than 8 minutes** before we killed it.

### The flags you'll use all the time

| Flag | What it does |
|---|---|
| `-bench=regexp` | Which benchmarks to run (`.` for all). Combine with `-run='^$'` to skip tests |
| `-benchmem` | Report `B/op` and `allocs/op` (from `runtime.ReadMemStats` in `StartTimer`/`StopTimer`). Same as calling `b.ReportAllocs()` |
| `-count=N` | Run every benchmark N times. You need several samples to do statistics |
| `-benchtime=2s` or `=1000x` | Target duration per benchmark, or an exact iteration count |
| `-cpu=1,4,8` | Run with each `GOMAXPROCS` value. That's the `-32` suffix in `BenchmarkFoo-32` |

`b.SetBytes(n)` adds a `MB/s` column, and `b.ReportMetric(v, "unit")` adds your own.

### Why `benchstat`

A single run tells you almost nothing: on a laptop, with a browser open (or, in this workshop, other people's benchmarks running), the same code can vary by 10% or more. [`benchstat`](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat) takes several samples per benchmark and reports the **median** with a **confidence interval** (`± 4%`), and compares two sets with a **Mann-Whitney U test**, a test that doesn't assume normally distributed timings. If the difference could be noise, it prints `~` and the p-value instead of a percentage. With `-count=6`, the smallest p-value it can reach is 0.002, and anything under 0.05 is considered significant.

The basic workflow, which later modules will use too:

```bash
go test -run '^$' -bench . -count 6 > old.txt
# ...change the code...
go test -run '^$' -bench . -count 6 > new.txt
benchstat old.txt new.txt
```

The labs use a variant: both implementations run in the same file as sub-benchmarks named `impl=before` and `impl=after`, and `benchstat -col /impl` turns that key into columns.

## The Lab

This lab is different from the others. The code under test is fine. **The benchmarks are broken**, and you fix them.

```
labs/01-benchmarking/
  lab.go             PopCount and Mix, the functions we measure (don't edit)
  before_test.go     three broken benchmarks (baseline, don't edit)
  after_test.go      your copy: fix these
  solution_test.go   reference solution (built with -tags solution)
  lab_test.go        correctness tests + the Benchmark* functions
```

| Benchmark | What it should measure | What's wrong |
|---|---|---|
| `PopCount` | One `PopCount` call | `b.N` loop, result discarded |
| `Mix` | One `Mix` call | `b.Loop`, but the input is a constant |
| `Sort` | Sorting 10,000 random ints | Sorts the same slice in place |

### Step 1: Measure the baseline

```bash
make bench LAB=01
```

Both columns run the same broken code, so you'd expect `~` everywhere. Often you'll get it, but not always. In one of our runs:

```
            │    before     │    after      vs base               │
PopCount-32   0.2051n ±  2%   0.2028n ± 2%        ~ (p=0.167 n=6)
Mix-32        0.4075n ±  2%   0.2197n ± 4%  -46.09% (p=0.002 n=6)
Sort-32        3.420µ ± 10%    3.387µ ± 6%        ~ (p=0.900 n=6)
```

Identical code, a "significant" 46% difference. At sub-nanosecond scale, a loop of three or four instructions is sensitive to things like where it lands in memory (code alignment) and what runs on the sibling hyper-thread. `benchstat` can tell you a difference is real; it can't tell you it's caused by *your change*. One more reason to distrust sub-nanosecond results.

Now look at the numbers and ask yourself which ones are believable. Sorting 10,000 ints in 3 µs is 0.3 ns per element. Is that plausible?

### Step 2: Find the evidence

Don't take my word for it: look at what the compiler did.

```bash
cd labs
go test -c -o /tmp/b01.test ./01-benchmarking/
go tool objdump -s 'benchPopCountBefore|benchMixBefore' /tmp/b01.test
go test -run '^$' -gcflags=-m=2 ./01-benchmarking/ 2>&1 | grep -E 'before_test.*(inlining|kept alive)'
```

You should find the empty loop and the precomputed `MOVQ $0xa759ea27d4727622` shown above. For `Sort`, profile it (we'll learn profiling properly in [Profiling 101](02-profiling.md)):

```bash
go test -run '^$' -bench 'Sort/impl=before' -cpuprofile /tmp/cpu.prof ./01-benchmarking/
go tool pprof -top /tmp/cpu.prof | head -15
```

Look for which pdqsort functions show up. On sorted input you'll mostly see the cheap "is it already sorted?" path (`partialInsertionSortOrdered`) instead of real partitioning work.

### Step 3: Fix the benchmarks

Edit `labs/01-benchmarking/after_test.go`:

1. **`benchPopCountAfter`**: switch to `for b.Loop()`. The result of the call is now kept alive.
2. **`benchMixAfter`**: give `Mix` an input the compiler can't know: declare `x` before the loop and increment it in the body (`Mix(x); x++`).
3. **`benchSortAfter`**: keep the random data pristine. Allocate a `work` slice once, and in every iteration `copy(work, data)` then sort `work`. To check that the copy doesn't dominate, write a quick benchmark that only copies (we measured 0.68 µs, under 0.2% of the sort).

`go test ./01-benchmarking/` checks the functions still work.

### Step 4: Compare

```bash
make bench LAB=01
```

Here are the results with the reference solution (`make solution LAB=01`) on an AMD Ryzen AI MAX+ 395:

```
            │    before    │                  after                  │
            │    sec/op    │    sec/op      vs base                  │
PopCount-32   0.2024n ± 3%    0.9342n ± 2%    +361.65% (p=0.002 n=6)
Mix-32        0.2233n ± 4%    0.6875n ± 1%    +207.79% (p=0.002 n=6)
Sort-32        3.006µ ± 2%   362.797µ ± 4%  +11969.10% (p=0.002 n=6)
```

For once, **"after" being slower is the good news**: those are the real costs.

### Step 5: Practice the baseline-file workflow

Let's use `benchstat` the way you'll use it on your own code. Save a baseline of your fixed `Sort` benchmark:

```bash
cd labs
go test -run '^$' -bench 'Sort/impl=after' -count 6 ./01-benchmarking/ > old.txt
```

Now, in `benchSortAfter`, replace `slices.Sort(work)` with `slices.SortFunc(work, cmp.Compare[int])` (add the `cmp` import), and run it again into `new.txt`:

```bash
go test -run '^$' -bench 'Sort/impl=after' -count 6 ./01-benchmarking/ > new.txt
benchstat old.txt new.txt
```

```
                   │   old.txt   │              new.txt               │
                   │   sec/op    │   sec/op     vs base               │
Sort/impl=after-32   395.0µ ± 2%   611.7µ ± 4%  +54.84% (p=0.002 n=6)
```

Then undo the change and run it once more. You should get something like `~ (p=0.589 n=6)`: the same code, measured twice, with differences that are just noise. Remove `old.txt` and `new.txt` when you're done.

## Understanding the Results

**PopCount**: 0.2 ns/op was the cost of incrementing a counter. The real popcount costs about 1 ns. With `b.Loop`, the inlined code is still there, but its result is now stored for the KeepAlive, so the compiler can't drop it.

**Mix**: 0.22 ns/op was the cost of the `b.Loop` bookkeeping plus storing a constant. Notice the honest number is still under a nanosecond: `Mix` is a handful of shifts, XORs and two multiplications, and iterations don't depend on each other (each uses `x`, not the previous result), so the CPU overlaps several of them. That's a measurement of **throughput**, not latency. If you want latency, feed each result into the next call (`x = Mix(x)`). Both are valid questions, so make sure you know which one you're asking.

**Sort**: a 120x difference. The "before" version measured pdqsort's best case on sorted data. Bugs like this survive code review because the number isn't obviously absurd: 3 µs "sounds fast". Always sanity-check: 10,000 elements × log₂(10,000) ≈ 130,000 comparisons can't run in 3 µs.

> **Rule of thumb:** if a result is under ~1 ns/op, or got much better after a change you didn't expect to matter, look at the assembly before you celebrate.

## Key Takeaways

- With a `b.N` loop, your function is called several times with growing `b.N`. Setup before the loop is timed unless you call `b.ResetTimer()`
- `b.Loop` runs your function once, never times the setup, and keeps call results and assigned variables alive through a compiler rewrite
- `b.Loop` does **not** stop inlining or constant folding. Inputs must not be compile-time constants
- The KeepAlive rewrite changes the code inside the loop. Put the code under test in a function
- Benchmarks that mutate their input must restore it every iteration. `StopTimer`/`StartTimer` stop the world, so avoid them in fast loops
- Use `-count 6` (or more) and `benchstat`. `~` means "can't tell from noise", and it's a perfectly good answer

## Going Further (optional)

1. Write a latency version of the `Mix` benchmark (`x = Mix(x)`). How does it compare to the throughput version, and why?
2. Run the `PopCount` benchmark with `-cpu 1,8,32`. Does `GOMAXPROCS` change the result of a single-goroutine benchmark? Now try `b.RunParallel`.
3. Add `b.SetBytes(10_000 * 8)` to the `Sort` benchmark and look at the new MB/s column.
