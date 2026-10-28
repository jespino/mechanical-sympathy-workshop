# Branch Prediction

> **Want to learn more?** The Go compiler's branch-to-conditional-move pass is [`cmd/compile/internal/ssa/branchelim.go`](https://github.com/golang/go/blob/go1.27.0/src/cmd/compile/internal/ssa/branchelim.go): it's short and very readable. The famous Stack Overflow question [Why is processing a sorted array faster than processing an unsorted array?](https://stackoverflow.com/questions/11227809) is the classic explanation. Agner Fog's [microarchitecture guide](https://www.agner.org/optimize/microarchitecture.pdf) documents branch predictors and misprediction penalties per CPU family. Dan Luu's [Branch prediction](https://danluu.com/branch-prediction/) is a gentle introduction to how predictors work.

A CPU doesn't wait to find out which way an `if` goes. It guesses, and keeps executing down the guessed path. When the guess is right, the branch is essentially free. When it's wrong, all the work done on the wrong path is thrown away. In this module we look at what that costs, when the Go compiler removes branches for you, and how to remove the ones it doesn't.

## Learning Objectives

By the end of this module, you will:

- Understand pipelining, speculative execution and the cost of a misprediction
- Reproduce the classic "sorted data is faster" effect in Go
- Know when the Go compiler turns an `if` into a conditional move (`CMOV`), and when it can't
- Read Go assembly with `go build -gcflags=-S` to check for branches
- Rewrite data-dependent branches without branches, and know when that's slower
- Count branch misses with `perf stat` (Linux)

## The Concept: Guessing the Future

### Pipelines and speculation

A modern core works on dozens of instructions at once: fetching, decoding, executing and retiring them in a deep pipeline, and with an out-of-order window of hundreds of instructions. To keep that machinery full, the front end has to know which instruction comes after a conditional jump **long before the condition is computed**. So it asks the **branch predictor**, a piece of hardware that remembers the history of every branch and of the branches leading to it, and fetches from the predicted target.

![The CPU guesses which way a branch goes and starts executing; a wrong guess throws that work away](images/17-pipeline.webp)

If the prediction was wrong, the core discovers it when the comparison finally executes, flushes everything fetched after the branch, and restarts from the right target. On current x86 cores that costs on the order of **15-20 cycles** of lost work, plus whatever useful work the out-of-order engine could have been doing in the meantime.

Predictors are very good at patterns: loop branches, branches that almost always go the same way, and even repeating sequences. What they can't predict is **data that is random**. A branch on a coin flip is mispredicted about half the time, no matter how clever the predictor is.

### Branchless code: conditional moves

The alternative is to compute both outcomes and *select* one, without a jump. On amd64 that's `CMOVcc` (conditional move) and `SETcc` (set a byte to 0 or 1 from a flag). There's no guessing, so nothing to mispredict, but the selection waits for the comparison, and both sides are always computed. A branch you could predict perfectly is cheaper than a conditional move. A branch you can't predict is much more expensive.

### What the Go compiler does

The `branchelim` SSA pass turns simple `if`s into `CondSelect` values, which become `CMOV` on amd64. The comment at the top of `branchelim.go` describes the shape it looks for:

```go
// branchelim tries to eliminate branches by
// generating CondSelect instructions.
//
// Search for basic blocks that look like
//
//	bb0            bb0
//	 | \          /   \
//	 | bb1  or  bb1   bb2    <- trivial if/else blocks
//	 | /          \   /
//	bb2            bb3
//
// where the intermediate blocks are mostly empty (with no side-effects);
// rewrite Phis in the postdominator as CondSelects.
```

Reading the rest of the file, the conditions are strict:

- **The arm must be tiny**: for a one-armed `if`, `const maxfuseinsts = 2` SSA values at most.
- **No side effects**: `canSpeculativelyExecute` rejects memory operations (so **no stores** in the arm), divisions and anything that can panic.
- **Only integers and pointers**: `canCondSelect` returns false for floats and for values bigger than a register.
- **On amd64, if/else costs**: `shouldElimIfElse` allows at most one `CMOV` per diamond when other values depend on the arms (`const maxcost = 2`).
- **Not for load addresses**: if the selected value is used to compute the address of a load, the branch stays (issue 26306), because a predicted branch lets the load start early.

So this function from the lab is already branchless:

```go
func SumAboveSimple(d []int32, t int32) int64 {
    var sum int64
    for _, v := range d {
        if v >= t {
            sum += int64(v)
        }
    }
    return sum
}
```

```bash
cd labs
go build -gcflags=-S ./17-branch-prediction/ 2>&1 | grep -A20 'SumAboveSimple STEXT'
```

```
MOVL    (AX)(DX*4), SI      // v := d[i]
MOVLQSX SI, R8
ADDQ    CX, R8              // R8 = sum + v (computed always)
CMPL    DI, SI              // t vs v
CMOVQLE R8, CX              // sum = R8 if t <= v
INCQ    DX
CMPQ    BX, DX
JGT     loop
```

The classic "sort the data first" trick does nothing for this loop in Go. But change the body slightly, so that it updates **two** variables or **stores** to memory, and the branch is back. That's what the lab is about.

> `go tool objdump -s 'SumAbove' ./x.test` shows the same code from a compiled binary, with addresses. Use `-gcflags=-S` while editing, `objdump` to check what actually shipped.

## The Lab

```
labs/17-branch-prediction/
  before.go      branchy loops (baseline, don't edit)
  after.go       your copy: remove the data-dependent branches
  solution.go    reference solution (built with -tags solution)
  lab_test.go    correctness test + benchmarks
```

| Function | What it does | Why the branch survives |
|---|---|---|
| `SumAbove(d, t)` | sum **and count** of the values `>= t` | two variables change in the arm |
| `Filter(d, out, t)` | copies the values `>= t` into `out` | a store in the arm |

Each benchmark runs on the **same** 1M values (uniform in `[0, 256)`, threshold 128) in two orders: `data=random` and `data=sorted`.

### Step 1: Measure the baseline

```bash
make bench LAB=17
```

Even before you change anything, look at the `before` column: `data=sorted` should be many times faster than `data=random`. Same values, same work, same result. The only difference is whether the branch predictor can guess right.

### Step 2: Find the branch (and count the misses)

Look at the assembly of `SumAboveBefore`:

```bash
cd labs
go build -gcflags=-S ./17-branch-prediction/ 2>&1 | grep -A20 'SumAboveBefore STEXT'
```

```
MOVL    (AX)(DX*4), R8      // v := d[i]
CMPL    DI, R8
JGT     next                // if t > v, skip: the data-dependent branch
MOVLQSX R8, R8
ADDQ    R8, CX              // sum += v
INCQ    SI                  // count++
JMP     next
```

Then, optionally, count the mispredictions.

> **perf is Linux-only.** On macOS and Windows, skip this part: the random-vs-sorted timings already prove the point. On macOS, Instruments' *CPU Counters* template can sample branch mispredictions if you want to go further.
>
> If `perf stat` says *"No supported events found"* or *"Access to performance monitoring … is limited"*, your distribution restricts it (Ubuntu and Debian default to `kernel.perf_event_paranoid=4`). Lower it until the next reboot with `sudo sysctl kernel.perf_event_paranoid=1`.

```bash
go test -c -o bp.test ./17-branch-prediction/
perf stat -e branches,branch-misses \
    ./bp.test -test.run '^$' -test.bench 'SumAbove/data=random/impl=before' -test.benchtime 200x
perf stat -e branches,branch-misses \
    ./bp.test -test.run '^$' -test.bench 'SumAbove/data=sorted/impl=before' -test.benchtime 200x
```

*(Run both and compare. Write your numbers here.)*

What to look for: `branches` should be about the same in both runs, while `branch-misses` should be dramatically higher for `data=random`. With 200 runs over 1M coin flips, expect on the order of 100M misses for random data, against a tiny number for sorted data. Once you've done Step 3, run `impl=after` on random data: the misses should collapse, even though the data is still random.

### Step 3: Fix it

Edit `labs/17-branch-prediction/after.go`:

1. **`SumAboveAfter`**: get a 0/1 value out of the comparison with an `if` that the compiler *can* convert (one variable, constant values):

   ```go
   b := 0
   if v >= t {
       b = 1
   }
   ```

   then update both accumulators with arithmetic: `count += b`, and `sum += int64(v) & -int64(b)` (`-1` is all ones, so the mask keeps `v`; `-0` is zero).
2. **`FilterAfter`**: store **every** value at `out[n]`, then advance `n` only if it passed. A rejected value is simply overwritten by the next one. (This is why `out` must have room for `len(d)` values: re-slice it with `out = out[:len(d)]` first, which also removes a bounds check, see [Bounds Check Elimination](09-bounds-check-elimination.md).)

Check the assembly again. You should see `SETLE` (or `SETGE`) and no conditional jump in the loop body other than the loop condition and the bounds check.

### Step 4: Compare

```bash
make bench LAB=17
```

Reference solution (`make solution LAB=17`) on an AMD Ryzen AI MAX+ 395:

```
                        │    before    │               after                │
                        │    sec/op    │   sec/op     vs base               │
SumAbove/data=random-32   3030.9µ ± 1%   481.4µ ± 2%  -84.12% (p=0.002 n=6)
SumAbove/data=sorted-32    314.8µ ± 3%   479.5µ ± 2%  +52.32% (p=0.002 n=6)
Filter/data=random-32     3199.7µ ± 1%   392.3µ ± 1%  -87.74% (p=0.002 n=6)
Filter/data=sorted-32      424.6µ ± 3%   406.4µ ± 3%   -4.29% (p=0.002 n=6)
```

## Understanding the Results

**Random data, before**: 3.0 ms for 1M elements, ~2.9 ns per element. **Sorted data, before**: 0.31 ms. That's **~10x** for the same instructions. With sorted data the branch goes one way for the first half and the other way for the second half: two mispredictions in total. With random data it's about 500,000 of them. (3.0 ms - 0.31 ms) / 500,000 ≈ **5.4 ns lost per misprediction** on this machine: the flush itself, plus the out-of-order work that couldn't be overlapped.

**After, random data**: 6.3x faster for `SumAbove`, 8.2x for `Filter`. And notice that the `after` times are **the same for random and sorted data**. Branchless code doesn't care about the data: that's the whole point.

**After, sorted data**: `SumAbove` is **slower**, by 52% (`Filter` too, once you remove the alignment artifact explained below). When the branch is perfectly predictable, the branchy loop skips the work for half of the elements for free, while the branchless loop always does the work for every element, and has a longer dependency chain through `SETcc`, `NEG`, `AND`, `ADD`. This isn't a flaw in the lab, it's the lesson: **branchless code trades the best case for a predictable worst case**. Use it when the condition is genuinely unpredictable (random, hashed or user-controlled data), and measure with realistic data.

> **A benchmarking trap we hit while building this lab.** With 32K elements instead of 1M, the random-order `SumAbove` was only ~3.6x slower than the sorted one, instead of ~10x. The benchmark runs the same function over the same data thousands of times, and modern predictors have enough history to **partially memorize** a sequence of 32K "random" outcomes. Production data doesn't repeat like that. If you benchmark branchy code, use inputs large enough (or varied enough) that the predictor can't learn them.

> **A second trap, found when moving the workshop to Go 1.27.** `FilterBefore` compiles to exactly the same instructions as with Go 1.26, yet `Filter/data=sorted/impl=before` went from ~250 µs to ~425 µs, which makes the branchless version look as fast as the predictable branch. The cause is **code alignment**: the linker aligns functions to 32 bytes on amd64 (`funcAlign` in `cmd/link/internal/amd64/l.go`), and in the Go 1.27 test binary `FilterBefore` happens to start 32 bytes past a 64-byte boundary, so its 32-byte hot loop straddles two 64-byte blocks of code, which the front end fetches and caches less efficiently. Nothing in your code changed: the binary got bigger elsewhere and the function moved. Rebuild with 64-byte alignment and the old picture comes back: `go test -tags solution -ldflags=-funcalign=64 -run '^$' -bench Filter ./17-branch-prediction/` gives 250.8 µs before and 405.9 µs after on sorted data (+62%). When a tight loop gets 1.7x slower and the assembly didn't change, suspect alignment before you suspect the compiler.

## Key Takeaways

- The CPU predicts every branch and executes speculatively: right guesses are free, wrong ones cost ~15-20+ cycles
- Data-dependent branches on random data are mispredicted ~50% of the time; sorted or skewed data is predicted well
- The Go compiler's `branchelim` pass emits `CMOV` for tiny, side-effect-free, integer `if`s: check with `go build -gcflags=-S`
- Stores, multiple updated variables, floats and load addresses keep the branch
- Rewrite with a 0/1 value from a comparison plus arithmetic (masks, `+= b`), or unconditional stores, plus the `min`/`max` builtins
- Branchless code is slower when the branch was predictable: choose based on your real data
- `perf stat -e branches,branch-misses` shows the misprediction count directly (Linux)

## Going Further (optional)

1. Replace the uniform data with a skewed distribution (e.g. 95% of values below the threshold). Where is the break-even point between `before` and `after`?
2. Write a clamp loop `out[i] = min(max(d[i], lo), hi)` and check the assembly. Does it branch?
3. Change `SumAboveSimple` to accumulate into a `float64`. Does the `CMOVQLE` survive? Check the `canCondSelect` function in `branchelim.go` to see why.
4. Change `size` in `lab_test.go` to `1 << 12`, `1 << 15` and `1 << 18`. At what size does the predictor stop memorizing the random data on your machine?
5. Go 1.27 adds an experimental portable [`simd`](https://pkg.go.dev/simd) package (build with `GOEXPERIMENT=simd`). Branchless code is one step from data-parallel code: write a `SumAbove` that loads `simd.Int32s` vectors with `simd.LoadInt32s`, compares them with `GreaterEqual` and accumulates with `Masked` and `Add`, so many elements are compared per instruction and there is nothing to predict. How does it compare with `SumAboveAfter`?
