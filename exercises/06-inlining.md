# Inlining

> **Want to learn more?** Read [`cmd/compile/internal/inline/inl.go`](https://github.com/golang/go/blob/go1.27.0/src/cmd/compile/internal/inline/inl.go) (look for the budget constants at the top, `CanInline` and `hairyVisitor.doNode`), and see how [`unicode/utf8.DecodeRune`](https://github.com/golang/go/blob/go1.27.0/src/unicode/utf8/utf8.go) is written to fit the budget.

In [Escape Analysis](05-escape-analysis.md) we saw a line of `-m` output that said `inlining call to normalizeBefore`. Inlining is the compiler copying a function's body into its caller, instead of emitting a call. It is the optimization that makes many others possible, and Go is quite conservative about it. In this module we look at the rules the compiler uses to decide, and we take a hot function that is *just* too big and make it fit.

## Learning Objectives

By the end of this module, you will:

- Know what a function call costs, and what inlining gains beyond removing it
- Understand Go's inlining **budget** (80) and the cost model that counts IR nodes
- Know what still prevents inlining in Go 1.27
- Read `-gcflags=-m=2` "can inline ... with cost N" and "cannot inline ... cost N exceeds budget 80"
- Apply the **fast path / slow path split** to get a hot function inlined

## The Concept: What Inlining Buys

A function call in Go is not expensive in absolute terms, a few nanoseconds at most. But in a tight loop that does only a few nanoseconds of real work per iteration, the call *is* the work. A call means:

- moving arguments into registers (and results back), and maybe spilling live values to the stack around the call
- the `CALL`/`RET` themselves, plus a function prologue that checks whether the stack needs to grow
- a wall for the optimizer: the caller's loop can't keep values in registers across the call, can't eliminate bounds checks based on what the callee checked, can't see that a branch is never taken

![Inlining replaces a call with the callee's body; it only happens when the function's cost fits in the budget of 80](images/06-inline.webp)

Inlining removes all of that. The callee's body becomes part of the caller, and the whole thing is optimized together. As we'll see in the [next module](07-inlining-escape.md), that includes escape analysis.

The downside is code size: a function inlined in 50 places is copied 50 times, which costs binary size, compile time and instruction cache (since Go 1.27, identical closures inside those copies can at least share one body). That's why the compiler only inlines small functions.

### The budget

Here are the knobs, at the top of `inline/inl.go`:

```go
const (
	inlineMaxBudget       = 80
	inlineExtraAppendCost = 0
	// default is to inline if there's at most one call. -l=4 overrides this by using 1 instead.
	inlineExtraCallCost  = 57              // 57 was benchmarked to provided most benefit with no bad surprises; ...
	inlineParamCallCost  = 17              // calling a parameter only costs this much extra ...
	inlineExtraPanicCost = 1               // do not penalize inlining panics.
	inlineExtraThrowCost = inlineMaxBudget // with current (2018-05/1.11) code, inlining runtime.throw does not help.

	inlineBigFunctionNodes      = 5000                 // Functions with this many nodes are considered "big".
	inlineBigFunctionMaxCost    = 20                   // Max cost of inlinee when inlining into a "big" function.
	inlineClosureCalledOnceCost = 10 * inlineMaxBudget // if a closure is just called once, inline it.
)
```

The "cost" of a function is computed by the `hairyVisitor`, which walks the function's IR (the compiler's syntax tree) and charges **1 per node**, with a list of exceptions. The important ones:

- A call to a function that **can't be inlined** costs **57** extra. With a budget of 80, that leaves about 20 nodes for everything else.
- A call to a function that **can** be inlined costs that function's own inline cost instead. That's **mid-stack inlining**: `A` calls `B` calls `C`, and if `C` is cheap, it gets folded into `B`, which may still fit into `A`.
- `panic` costs 1. Calls into `encoding/binary` / `internal/byteorder` load and store helpers are treated as cheap, since they compile to single instructions.
- Into a "big" caller (5000+ nodes), only functions with cost ≤ 20 are inlined.
- Closures get twice the budget, and a closure called just once gets 800.

### What prevents inlining in Go 1.27

Some constructs make a function non-inlinable no matter its cost. In `hairyVisitor.doNode`, the hard stops are:

```go
case ir.ORECOVER:
	v.reason = "call to recover"
	return true
...
case ir.OGO, ir.ODEFER, ir.OTAILCALL:
	v.reason = "unhandled op " + n.Op().String()
	return true
```

Plus the `//go:noinline` directive, and a few runtime-internal cases (functions that call `GetCallerPC`/`GetCallerSP`, some `//go:` pragmas). Things that *used to* prevent inlining in older Go versions and don't anymore: `for` loops, `select`, type switches, closures, labeled `break`/`continue`. We checked all of them with Go 1.27: they inline fine as long as they fit the budget. A lot of old blog posts are out of date here, so when in doubt, ask the compiler.

### Reading the compiler's answer

`-gcflags=-m` prints `can inline F` and `inlining call to F`. `-m=2` adds the cost of everything, and the reason for the functions that didn't make it:

```
./q.go:31:6: can inline small with cost 4 as: func(int) int { return x * 2 }
./q.go:3:6: can inline sel with cost 14 as: func(chan int) int { select statement }
./q.go:28:6: cannot inline def: unhandled op DEFER
./q.go:29:6: cannot inline rec: call to recover
```

Note that "cannot inline" and the costs are only printed at `-m=2`, not at `-m`.

### PGO

Since Go 1.21, a CPU profile passed with `-pgo` raises the budget to **2000** for call sites that are hot in the profile (`inlineHotMaxBudget`). That's covered in [Profile-Guided Optimization](18-pgo.md). Without a profile, the 80 rule is all there is.

## The Lab

```
labs/06-inlining/
  before.go      naive implementation (baseline, don't edit)
  after.go       your copy: make this one fast
  solution.go    reference solution (built with -tags solution)
  lab_test.go    correctness test + benchmarks
```

We decode a block of timestamps, the way a time series database stores them: each entry is a *delta-of-delta* (the change in the interval between samples), zigzag-encoded and written as a varint. For metrics scraped every 10 seconds the delta-of-delta is almost always tiny, so **almost every varint is a single byte**. `DecodeTimestampsBefore` calls `uvarintBefore` once per timestamp, 10,000 times per block.

### Step 1: Measure the baseline

```bash
make bench LAB=06
```

### Step 2: Find the call

The profile shows `uvarintBefore` as its own function, which already tells you it wasn't inlined:

```bash
cd labs
go test -run '^$' -bench 'Decode/impl=before' -cpuprofile cpu.prof ./06-inlining/
go tool pprof -top cpu.prof | grep -E 'flat%|uvarint|DecodeTimestamps'
```

```
      flat  flat%   sum%        cum   cum%
     670ms 65.05% 65.05%     1030ms   100%  labs/06-inlining.DecodeTimestampsBefore
     360ms 34.95%   100%      360ms 34.95%  labs/06-inlining.uvarintBefore
```

Now ask the compiler why:

```bash
go build -gcflags=-m=2 ./06-inlining/ 2>&1 | grep -E 'inline.*(uvarint|DecodeTimestamps)' | cut -c1-110
```

```
06-inlining/before.go:16:6: cannot inline uvarintBefore: function too complex: cost 82 exceeds budget 80
06-inlining/before.go:42:6: cannot inline DecodeTimestampsBefore: function too complex: cost 115 exceeds budget 80
```

**82.** Two nodes over. The function is a loop with a few validity checks (too long, overflow, non-minimal encoding), and every one of them is real: we can't just delete them.

### Step 3: Fix it with a fast path / slow path split

The trick is used all over the standard library: keep a tiny function that handles the **common case** inline, and move everything else to a separate function that it calls. From `sync.Once`:

```go
if !o.done.Load() {
	// Outlined slow-path to allow inlining of the fast-path.
	o.doSlow(f)
}
```

`sync.Mutex.Lock`, `sync.RWMutex.RLock` and `unicode/utf8.DecodeRune` do the same. The common case here is "the first byte is below `0x80`", and then the value is that byte.

But remember the cost model: the call to the slow path alone costs 57 + its arguments. The fast path has about 20 nodes to spend, so its *exact shape* matters. Look at `utf8.DecodeRune`:

```go
func DecodeRune(p []byte) (r rune, size int) {
	// Inlineable fast path for ASCII characters; see #48195.
	// This implementation is weird but effective at rendering the
	// function inlineable.
	for _, b := range p {
		if b < RuneSelf {
			return rune(b), 1
		}
		break
	}
	r, size = decodeRuneSlow(p)
	return
}
```

`go build -gcflags=-m=2 unicode/utf8` reports `can inline DecodeRune with cost 79`. One node to spare.

Edit `labs/06-inlining/after.go`:

1. Rename the current loop to `uvarintSlow` (same body).
2. Write a new `uvarintAfter(buf []byte) (x uint64, n int)` with the `DecodeRune` shape: a `range` loop that returns `uint64(b), 1` for a byte below `0x80` and otherwise `break`s, then `x, n = uvarintSlow(buf)` and `return`.
3. Check: `go build -gcflags=-m=2 ./06-inlining/ 2>&1 | grep uvarint`.

You should see:

```
06-inlining/solution.go:22:6: cannot inline uvarintSlow: function too complex: cost 82 exceeds budget 80
06-inlining/solution.go:9:6: can inline uvarintAfter with cost 79 as: func([]byte) (uint64, int) { ...
06-inlining/solution.go:49:23: inlining call to uvarintAfter
```

How fragile is this? Here are the costs of variants with the *same behavior* that we measured with Go 1.27:

| Fast path shape | Cost |
|---|---|
| `range` + `break`, then `x, n = uvarintSlow(buf); return` | **79** ✓ |
| `if len(buf) > 0 && buf[0] < 0x80 {...}`, then `x, n = ...; return` | 81 ✗ |
| `range` + `break`, then `return uvarintSlow(buf)` | 85 ✗ |

The cost model counts IR nodes, not instructions, and some Go constructs are rewritten into more nodes than others (`return f()` with two results goes through extra temporaries). This is the dark side of a fixed budget: if you depend on a function being inlined, **add a test or a CI check** (the Go repo itself has `TestIntendedInlining` in `cmd/compile/internal/test/inl_test.go` for exactly that).

### Step 4: Compare

```bash
make bench LAB=06
```

Here are the results with the reference solution (`make solution LAB=06`) on an AMD Ryzen AI MAX+ 395:

```
          │   before    │               after                │
          │   sec/op    │   sec/op     vs base               │
Decode-32   19.32µ ± 2%   10.19µ ± 2%  -47.26% (p=0.000 n=8)

          │   before   │             after             │
          │    B/op    │    B/op     vs base           │
Decode-32   0.000 ± 0%   0.000 ± 0%  ~ (p=1.000 n=8) ¹
¹ all samples are equal

          │   before   │             after             │
          │ allocs/op  │ allocs/op   vs base           │
Decode-32   0.000 ± 0%   0.000 ± 0%  ~ (p=1.000 n=8) ¹
¹ all samples are equal
```

## Understanding the Results

The decoder is about **2x faster**, and we didn't change the algorithm: the slow path is the exact same code as before.

Is that really inlining, or is the fast path simply less work than the full loop? We checked by adding `//go:noinline` to the solution's `uvarintAfter`, keeping everything else the same. The result on the same machine: **about -6%** instead of -47%. Almost all of the gain comes from the call disappearing and from what the compiler can do once the fast path is part of the loop (keeping `t`, `delta` and the slice in registers, and folding the `n <= 0` check, which is always false on the fast path).

Note that `DecodeTimestampsAfter` itself still isn't inlinable (cost 137). It doesn't need to be: it's called once per block. **Inlining matters for the callee in the hot loop, not for the function that contains it.**

> **When not to bother:** if the function does a lot of work per call (hashing a kilobyte, a syscall, a map lookup on a big map), the call overhead is noise. Inlining matters for small functions called millions of times, like accessors, decoders, comparators and predicates.

## Key Takeaways

- Inlining removes the call, and lets the optimizer see caller and callee as one function
- Go inlines functions whose IR cost is ≤ **80**. A non-inlinable call costs **57** of that
- `recover`, `defer`, `go` and `//go:noinline` still prevent inlining in Go 1.27. Loops, closures and `select` don't
- `-gcflags=-m=2` prints the cost of every function and why it can't be inlined
- **Fast path / slow path split**: a tiny inlinable function for the common case, calling an outlined function for the rest
- The exact shape of the code changes the cost: verify with `-m=2`, and protect important inlining decisions with a test
- PGO can raise the budget for hot call sites (module 18)

## Going Further (optional)

1. Add `//go:noinline` to your `uvarintAfter` and re-run the benchmark. How much of the gain is left?
2. Change `testBlock` so that the intervals are irregular (for example `t += int64(rand.IntN(100_000))`). Most varints are now multi-byte. What happens to the speedup? Why?
3. Build with `-gcflags=-l` (disables inlining) and compare `before` against itself. Which standard library calls in this package stop being inlined? (`-m` will tell you.)
