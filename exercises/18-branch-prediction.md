# Branch Prediction

A CPU often guesses which path an `if` will take so it can keep working before the condition is known. Predictable branches are cheap; wrong guesses waste work. We will compare sorted and random data, then try code that does not need a data-dependent jump.

## Learning Objectives

By the end of this module, you will:

- Explain why data order can affect branch-heavy code
- Compare predictable and unpredictable inputs
- Check whether the compiler emits a conditional jump
- Try branchless code and recognize its tradeoffs

## The Concept: Guessing the Future

### Guessing the next path

A CPU works on several instructions at once. At a branch, it predicts the next path and starts work there. If it guessed incorrectly, it discards that work and starts on the correct path.

![The CPU guesses which way a branch goes and starts executing; a wrong guess throws that work away](images/18-pipeline.webp)

A condition that is almost always true is easy to predict. A condition based on random values is much harder. Sorted input can group all the false outcomes followed by all the true ones, making the same calculation easier to predict.

### Branchless does not mean free

The compiler can replace some small `if` expressions with instructions that select a value without jumping. On amd64, these include **conditional moves**, shown as `CMOV` in assembly.

Not every `if` becomes branchless. Updating multiple values or writing to memory can leave a conditional jump. Check the generated code instead of assuming.

Branchless code may do extra work for every element. It can help with unpredictable input and lose to an easily predicted branch. The data you benchmark matters.

## The Lab

`SumAbove` sums and counts product prices at or above a minimum price. `Filter` builds the list of products in that price range. We use prices in whole currency units: `[10, 200, 150]` with a minimum of 128 gives a sum of 350 and keeps `[200, 150]`. Both functions run on the same million prices in random and sorted order. Compare these input orders before changing the code.

Run the commands from the repository root.

### Step 1: Measure the baseline

```bash
make bench LAB=18
```

### Step 2: Find the cost

```bash
(cd labs && go build -gcflags=-S ./18-branch-prediction/ 2>&1 | grep -A20 'SumAboveBefore STEXT')
```

Look for a jump such as `JGT` after the comparison with the threshold. That is the data-dependent branch, separate from the jump that repeats the loop. Instruction names depend on the CPU architecture.

Optional on Linux, use `perf stat -e branches,branch-misses` with a compiled benchmark to compare random and sorted input. Skip counters if they are unavailable.

### Step 3: Fix it

Edit `labs/18-branch-prediction/after.go`. Keep `before.go` as the baseline.

1. In `SumAboveAfter`, turn the comparison into a 0/1 value, then update both results with arithmetic using the example below. For `b == 1`, `-int64(b)` is all one bits and keeps the value; for `b == 0`, the mask makes it zero.
2. In `FilterAfter`, make `out` long enough with `out = out[:len(d)]` (the caller provides enough capacity). Write every value at `out[n]`, then increase `n` by 0 or 1 depending on the comparison. A rejected value is overwritten by the next one. Return `n`, the number of kept values.
3. Inspect the assembly again. Look for the data-dependent jump disappearing; the loop itself still needs control flow.

```go
b := 0
if v >= t {
    b = 1
}
count += b
sum += int64(v) & -int64(b)
```

### Step 4: Check and compare

```bash
(cd labs && go test ./18-branch-prediction/)
make bench LAB=18
```

Reference solution (`make solution LAB=18`), on my machine:

| Benchmark | Before | After |
|---|---|---|
| `SumAbove`, random | 3030.9 µs | 481.4 µs |
| `SumAbove`, sorted | 314.8 µs | 479.5 µs |
| `Filter`, random | 3199.7 µs | 392.3 µs |
| `Filter`, sorted | 424.6 µs | 406.4 µs |

## Understanding the Results

**Random input** improves substantially because the CPU no longer makes a hard-to-predict choice for each value. The new timings are similar for sorted and random data.

**Sorted input** shows the tradeoff: `SumAboveAfter` is slower than before. The original branch was easy to predict and skipped unnecessary additions. The branchless version does that arithmetic on every iteration.

`Filter` improves only slightly on sorted input in this run. Small assembly-layout differences can affect tight loops, so don't treat that row as proof that branchless code always wins.

Repeated small random inputs can become partly predictable to the CPU. Use varied or sufficiently large inputs, and choose an implementation based on the data your application actually processes.

## Key Takeaways

- Predictable branches are often cheap; wrong predictions waste work.
- The compiler already removes some simple branches.
- Branchless code can do extra work and be slower on predictable data.
- Measure both realistic data distributions and generated code.

## Going Further (optional)

1. Make 95% of values fall below the threshold. Which version is faster?
2. Compare `SumAboveSimple` in `before.go` with `SumAboveBefore`. Which one already uses conditional selection on your CPU?
3. Try smaller input sizes. Does repeatedly using the same random sequence affect the results?
