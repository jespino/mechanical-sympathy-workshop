# Inlining

> **Want to learn more?** [The IR](https://internals-for-interns.com/posts/the-go-ir/#function-inlining-eliminating-calls) on Internals for Interns explains the cost model and the inlining process. The [IR optimizations chapter](https://www.youtube.com/watch?v=qnmoAA0WRgE&t=1206s) of Jesús Espino's GopherCon UK talk *Understanding the Go Compiler* shows where this stage sits in the compiler.

In [Escape Analysis](05-escape-analysis.md) we saw a line of `-m` output that said `inlining call to normalizeBefore`. Inlining is the compiler copying a function's body into its caller, instead of emitting a call. It is the optimization that makes many others possible, and Go is quite conservative about it. In this module we look at the rules the compiler uses to decide, and we take a frequently called function and make its common case small enough to inline.

## Learning Objectives

By the end of this module, you will:

- Know what a function call costs, and what inlining gains beyond removing it
- Understand the inlining **budget** (80) and check a function's cost
- Know what prevents inlining
- Read `-gcflags=-m=2` "can inline ... with cost N" and "cannot inline ... cost N exceeds budget 80"
- Apply the **fast path / slow path split** to get a hot function inlined

## The Concept: What Inlining Buys

Calling a function takes a little extra work beyond running its code. That usually doesn't matter, but it adds up when a small function is called thousands of times in a loop.

**Inlining** replaces the call with the function's code. This removes the call overhead and lets the compiler optimize both pieces together, for example by removing checks it can now prove are unnecessary. In the [next module](07-inlining-escape.md), we'll see how this can also avoid heap allocations.

The tradeoff is more code: inlining a function in many places copies its body into each one. To limit that growth, the compiler normally only inlines small functions.

### A quick example: adding two numbers

These functions do the same addition. The `//go:noinline` directive forces the second one to remain a function call:

```go
func add(a, b int) int { return a + b }

//go:noinline
func addNoInline(a, b int) int { return a + b }
```

Benchmarking the two in a tight loop, the call costs about **0.6 ns** more than the inlined version on my machine (about 0.83 ns vs 0.20 ns per addition). That includes the call itself and the optimizations it prevents. It is small, but adds up when a function does very little work and is called often. In the lab we'll see it add up on realistic code.

### The budget

The compiler gives each function a **cost score** based on the code it contains. The usual inlining budget is **80**: a function that fits within it can be inlined, unless something else prevents it.

This score measures code complexity, **not execution time or lines of code**. Calling a function that can't be inlined adds **57 points**, so even a short function can exceed the budget.

You don't need to calculate the score yourself. Use `-gcflags=-m=2` to ask the compiler. In the lab, we'll split a function with a cost of **161** into a small wrapper with a cost of **70** and a helper for the remaining work.

### What prevents inlining

A small function can still be blocked from inlining if it contains `defer`, `go` or `recover`. Adding `//go:noinline` also tells the compiler to keep it as a function call.

Loops, closures and `select` don't automatically prevent inlining. When in doubt, ask the compiler.

### Reading the compiler's answer

Use `go build -gcflags=-m=2` to see the compiler's decisions. Look for three messages:

- `can inline F`: the function is eligible for inlining.
- `inlining call to F`: this particular call was replaced with the function's code.
- `cannot inline F`: the function isn't eligible; the message explains why, such as exceeding the budget or containing `defer`.

The shorter flag, `-gcflags=-m`, shows which functions can be inlined and which calls were inlined. Use `-m=2` for costs and reasons.

### PGO

A CPU profile shows the compiler which code runs most often. With **profile-guided optimization (PGO)**, it can give those frequently used calls a larger inlining budget, up to **2000**. We'll try this in [module 15](15-pgo.md).

## The Lab

You are calculating a shopping-cart total. Each item has a price in cents and an optional coupon:

```go
type Item struct {
    PriceCents int
    Coupon     string
}
```

`TotalBefore` calls `priceBefore` for each of 10,000 items. Most have no coupon: their final price is simply `max(cents, 0)`. Items with a coupon need more work: trim spaces, convert the code to uppercase, and apply `SAVE10`, `SAVE20` or `HALF`. Unknown codes leave the price unchanged.

The function already checks for an empty coupon first. But its other branches make it too large to inline, so even the simple case pays for a function call.

### Step 1: Measure the baseline

Run these commands from the repository root:

```bash
make bench LAB=06
```

### Step 2: Ask the compiler

```bash
(cd labs && go build -gcflags=-m=2 ./06-inlining/ 2>&1 | grep 'cannot inline priceBefore')
```

Look for `cost 161 exceeds budget 80`. The cost includes all branches, even though most calls don't use the coupon-handling code.

### Step 3: Move coupon handling to a helper

Edit `labs/06-inlining/after.go`:

1. Move everything after the empty-coupon check into `func priceWithCoupon(cents int, coupon string) int`. Keep the coupon rules unchanged.
2. Make `priceAfter` the small wrapper below.
3. Leave `TotalAfter` unchanged.

```go
func priceAfter(cents int, coupon string) int {
    if coupon == "" {
        return max(cents, 0)
    }
    return priceWithCoupon(cents, coupon)
}
```

Now the compiler can copy the common case directly into the total's loop. Only items with a coupon need to call the larger helper.

### Step 4: Check and compare

```bash
(cd labs && go test ./06-inlining/)
(cd labs && go build -gcflags=-m=2 ./06-inlining/ 2>&1 | grep -E 'can inline priceAfter|inlining call to priceAfter')
make bench LAB=06
```

The tests cover ordinary prices, coupon codes, whitespace, rounding and negative prices. The compiler should report `can inline priceAfter with cost 70` and `inlining call to priceAfter`.

With the reference solution (`make solution LAB=06`), on my machine:

| Measurement | Before | After |
|---|---|---|
| Time per 10,000 items | about 14 µs | about 4.6 µs |
| Allocations per run | 0 | 0 |

## Understanding the Results

The reference solution is about **3× faster** on this workload. It applies the same rules and keeps the existing empty-coupon check. The change makes that common case small enough to inline into the loop. As a check, disabling inlining on `priceAfter` takes the time back to about 14 µs.

The benchmark gives coupons to 1% of items. Coupon processing is still handled by a normal call; it has not been removed. Lowercase codes can also allocate during uppercasing, so the timing comparison uses uppercase codes while the tests still cover lowercase input.

Inlining is most useful for small operations repeated many times. If every item needs expensive coupon processing, avoiding the call matters much less.

## Key Takeaways

- The compiler considers the whole function when calculating its inlining cost.
- Move less common work to a helper to keep the common case small.
- Use `-gcflags=-m=2` to check that a call really was inlined.
- Benchmark realistic inputs and keep behavior unchanged.

## Going Further (optional)

1. Add `//go:noinline` above `priceAfter` and benchmark again. How much of the improvement remains?
2. Change `testCart` so half the items have a coupon, then all of them. How does the benefit change?
3. Add a coupon rule to `priceWithCoupon`. Does `priceAfter` still inline?
