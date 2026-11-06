# Bounds Check Elimination

> **Want to learn more?** Bounds checks are removed in the compiler's SSA stage. [The SSA Phase](https://internals-for-interns.com/posts/the-go-ssa/) on Internals for Interns introduces that stage and shows how to watch each pass with `GOSSAFUNC`. The [SSA optimization passes chapter](https://www.youtube.com/watch?v=qnmoAA0WRgE&t=1726s) of Jesús Espino's GopherCon UK talk *Understanding the Go Compiler* gives the overview.

Go checks slice indexes to prevent reads and writes outside the slice. The compiler can remove a check when it proves the access is safe. In this module we help it see that safety without removing it ourselves.

## Learning Objectives

By the end of this module, you will:

- Understand why bounds checks exist
- List the checks left by the compiler
- Write loops whose indexes are easy to prove safe
- Preserve behavior when slices are too short

## The Concept: Checks, and Proving Them Away

### When a check is unnecessary

In `for i := range s { use(s[i]) }`, the loop already guarantees a valid index. The compiler can remove the extra check for `s[i]`. It uses known lengths and comparisons to find these guarantees.

### Make safe access obvious

| Pattern | What the compiler knows |
|---|---|
| `for i := range s` | `i` is inside `s` |
| re-slice inputs to the same length | an index valid for one slice can be valid for the others |
| check the input lengths before looping | short input is rejected before processing begins |

A single check is usually cheap. Removing repeated checks can help in loops that do very little other work. Use this after a benchmark or profile identifies a busy loop.

### Length and capacity are different

`len(s)` is the number of elements currently in the slice. `cap(s)` is how far it can be extended. `s[:n]` can extend up to the capacity, so it does **not** prove that `n <= len(s)`. Check lengths explicitly when the function must reject short input.

## The Lab

You are printing a receipt. For each line, multiply a product's price by its quantity and subtract the line discount. Prices and discounts are in cents.

`LineTotalsBefore(dst, prices, quantities, discounts, n)` fills the first `n` output values. For example, two items at 100 cents with a 10-cent discount produce a line total of 190 cents.

Run the commands from the repository root.

### Step 1: Measure the baseline

```bash
make bench LAB=09
```

### Step 2: Find the cost

```bash
(cd labs && go build -gcflags='-d=ssa/check_bce/debug=1' ./09-bounds-check-elimination/ 2>&1 | grep before.go)
```

Each `Found IsInBounds` or `Found IsSliceInBounds` line is a remaining check. The baseline has four in `LineTotalsBefore`: one for each slice access.

### Step 3: Fix it

Edit `labs/09-bounds-check-elimination/after.go`. Keep `before.go` as the baseline.

1. Return immediately when `n <= 0`, matching the baseline's empty loop.
2. Check that all four slices have at least `n` elements. Keep the panic for short input, even if a short slice has spare capacity.
3. Re-slice to length `n` and loop with `range dst`:

```go
if n > len(dst) || n > len(prices) || n > len(quantities) || n > len(discounts) {
    panic("LineTotalsAfter: slice shorter than n")
}
dst, prices, quantities, discounts = dst[:n], prices[:n], quantities[:n], discounts[:n]
for i := range dst {
    dst[i] = prices[i]*quantities[i] - discounts[i]
}
```

### Step 4: Check and compare

```bash
(cd labs && go test ./09-bounds-check-elimination/)
make bench LAB=09
```

Reference solution (`make solution LAB=09`), on my machine:

| Benchmark | Before | After |
|---|---|---|
| `LineTotals` | about 1.9 µs | about 1.3 µs |

## Understanding the Results

The receipt calculations are unchanged, but the benchmark takes about 30% less time. The compiler can now prove the indexes are valid without repeating four checks for every line.

Run the check-listing command again with `grep after.go`. No output means no checks remain in that file. Checks outside a loop are fine: the goal is to avoid doing the same work for every element. Larger functions may see little benefit, so always measure.

## Key Takeaways

- Keep bounds safety; help the compiler prove it.
- `range` and clear length checks make indexes easier to reason about.
- Re-slicing checks capacity, so validate lengths when required.
- Use the compiler diagnostics and benchmark the result.

## Going Further (optional)

1. Replace `range dst` with `for i := 0; i < n; i++`. Do the checks return?
2. Give `prices` spare capacity but a length shorter than `n`. Why must the function still panic?
3. Add more work per receipt line. Does removing the checks still make as much difference?
