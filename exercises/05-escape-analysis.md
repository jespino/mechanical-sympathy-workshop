# Escape Analysis

> **Want to learn more?** Read the big comment at the top of [`cmd/compile/internal/escape/escape.go`](https://github.com/golang/go/blob/go1.27.0/src/cmd/compile/internal/escape/escape.go), the size limits in [`cmd/compile/internal/ir/cfg.go`](https://github.com/golang/go/blob/go1.27.0/src/cmd/compile/internal/ir/cfg.go), and `HeapAllocReason` in [`escape/utils.go`](https://github.com/golang/go/blob/go1.27.0/src/cmd/compile/internal/escape/utils.go).

In the previous modules we reduced the number of allocations by asking for the right amount of memory up front. Now we go one step further: we stop allocating on the heap at all. Go has no `new` vs `malloc` choice in the language. You write `&T{}` or `make([]byte, n)`, and the **compiler** decides where that memory lives. This module is about how it decides, how to read its decisions, and how to write code that lets it pick the stack.

## Learning Objectives

By the end of this module, you will:

- Know why stack allocation is (almost) free and heap allocation is not
- Understand what escape analysis does, and read `-gcflags=-m` and `-m=2` output
- Recognize the common reasons a value escapes: returned pointers, interfaces, storing into the heap, closures, unknown sizes
- Know the stack size limits in Go 1.27, including the stack allocation of variable-sized slices (new in 1.25 and 1.26)
- Use the "return values, not pointers" and "caller-provided buffer" (`AppendX`) patterns

## The Concept: Stack or Heap?

Each goroutine has a stack. A function's local variables live in its **stack frame**, which is created by moving the stack pointer when the function is called and thrown away when it returns. Allocating on the stack is one subtraction for the whole frame, and freeing is free. The GC never has to look at a dead frame.

![Values that don't outlive their function stay on the stack; values referenced after the function returns escape to the heap](images/05-stack-heap.webp)

The heap is different. A heap allocation goes through `runtime.mallocgc` (size classes, per-P caches, sometimes locks and page allocation). Since Go 1.27, a small `new(T)`/`&T{}` (up to 80 bytes) calls a size-specialized copy of it instead, like `mallocgcSmallScanNoHeaderSC5`: cheaper, but the same kind of work. The memory has to be found and freed later by the garbage collector, and all of that is work your program pays for (we'll measure GC costs in [GC Pressure](14-gc-pressure.md)).

So why doesn't Go put everything on the stack? Because a stack frame dies when its function returns. If a pointer to a local value can **outlive the frame**, the value must go to the heap. The compiler pass that proves which values can safely stay on the stack is **escape analysis**.

### How the compiler decides

Here's how `escape.go` describes its own job:

```go
// The two key invariants we have to ensure are: (1) pointers to stack
// objects cannot be stored in the heap, and (2) pointers to a stack object
// cannot outlive that object (e.g., because the declaring function
// returned and destroyed the object's stack frame, ...).
//
// We implement this with a static data-flow analysis of the AST.
// First, we construct a directed weighted graph where vertices
// (termed "locations") represent variables allocated by statements
// and expressions, and edges represent assignments between variables
// (with weights representing addressing/dereference counts).
```

Every variable, `new`, `make`, composite literal and so on becomes a **location**. Every assignment becomes an edge, weighted by how many `&` and `*` are applied (`p = &q` is -1, `p = q` is 0, `p = *q` is 1). There's a special **heap location**, and function results are locations too. Then the analysis walks the graph: if the address of a location can reach the heap, or a result, or something that lives longer, that location escapes.

The analysis is **interprocedural**: for each function it records how its parameters flow, as *parameter tags*. That's what the `-m` messages about parameters mean:

| Message | Meaning |
|---|---|
| `x does not escape` | the value (or the param's pointee) stays in the frame |
| `moved to heap: x` | a local *variable* `x` lives on the heap |
| `&T{...} escapes to heap` | that allocation is a heap allocation |
| `leaking param: p` | what `p` points to may reach the heap (or outlive the call) |
| `leaking param content: p` | not `p` itself, but things *reachable from* it, leak |
| `leaking param: p to result ~r0 level=0` | `p` flows into the return value |

Callers use those tags. If `f(p *T)` says "p does not escape", a caller can pass `&local` and keep `local` on its stack.

### The usual suspects

These are the most common reasons a value escapes:

1. **Returning a pointer** to a local, or storing one in a struct that is returned.
2. **Storing into the heap**: assigning a pointer into a global, a map, a slice or a struct that is itself on the heap, or sending it on a channel.
3. **Interfaces**: converting a value to an interface and passing it to something the compiler can't see through, like `fmt.Printf(format string, a ...any)`. Calls through an interface are opaque to escape analysis, so their arguments are assumed to leak. ([Interfaces, Boxing and Generics](08-interfaces-boxing.md) is all about this.)
4. **Closures** that capture variables by reference and themselves escape (for example, passed to `go` or stored).
5. **Size**: too large, or not known at compile time.

### Size limits in Go 1.27

Even a value that doesn't escape is put on the heap if it's too big. The limits are in `ir/cfg.go`:

```go
// MaxStackVarSize is the maximum size variable which we will allocate on the stack.
// This limit is for explicit variable declarations like "var x T" or "x := ...".
MaxStackVarSize = int64(128 * 1024)

// MaxImplicitStackVarSize is the maximum size of implicit variables that we will allocate on the stack.
//   p := new(T)          allocating T on the stack
//   p := &T{}            allocating T on the stack
//   s := make([]T, n)    allocating [n]T on the stack
//   s := []byte("...")   allocating [n]byte on the stack
MaxImplicitStackVarSize = int64(64 * 1024)
```

What about `make([]byte, n)` when `n` is a variable? For a long time the answer was "always the heap". **Go 1.25 changed that.** If the slice doesn't escape, the compiler now reserves a small buffer on the stack and uses it when the size fits (from the comment in `escape/utils.go`):

```go
// For non-constant sizes, we do a hybrid approach:
//
// if cap <= K {
//     var backing [K]E
//     s = backing[:len:cap]
// } else {
//     s = makeslice(E, len, cap)
// }
```

`K` is 32 *bytes* divided by the element size (`Debug.VariableMakeThreshold = 32` in `base/flag.go`). Go 1.26 extends the same idea to `append`: the first growth of a non-escaping slice can use a 32-byte stack buffer, and a slice that is built with `append` and then returned (its only escape) gets moved to the heap once, at the `return` (`cmd/compile/internal/slice/slice.go` and `runtime.moveSlice`). The lab has a demo:

```bash
cd labs
go run ./05-escape-analysis/cmd/stackmake
```

```
n=   8   make([]byte, n): 0 allocs   make([]byte, 1024): 0 allocs
n=  32   make([]byte, n): 0 allocs   make([]byte, 1024): 0 allocs
n=  33   make([]byte, n): 1 allocs   make([]byte, 1024): 0 allocs
n=  64   make([]byte, n): 1 allocs   make([]byte, 1024): 0 allocs
n=1024   make([]byte, n): 1 allocs   make([]byte, 1024): 0 allocs
```

Both `make` calls are reported as `does not escape` by `-m`. That's the catch: **"does not escape" doesn't always mean "no allocation"**. A non-escaping `make` with a run-time size still hits the heap above 32 bytes. A constant size stays on the stack all the way up to 64 KiB.

## The Lab

```
labs/05-escape-analysis/
  before.go         naive implementations (baseline, don't edit)
  after.go          your copy: make this one fast
  solution.go       reference solution (built with -tags solution)
  lab_test.go       correctness test + benchmarks
  cmd/stackmake/    the demo program above
```

Three functions process a tiny text metrics format (`name value timestamp`). Each one allocates for a different reason:

| Function | What escapes, and why |
|---|---|
| `Sum(lines)` | `parseSample` returns a `*Sample` |
| `Encode(dst, samples)` | `fmt.Appendf` takes `...any`, and the arguments escape |
| `CountKnown(known, names)` | `normalize` makes a buffer of unknown size, and returns a new string |

### Step 1: Measure the baseline

```bash
make bench LAB=05
```

Look at the `allocs/op` table: each benchmark processes 1,000 items, so you'll see roughly 1,000 (`Sum`, `CountKnown`) and 3,000 (`Encode`) allocations per operation.

### Step 2: Ask the compiler

```bash
cd labs
go build -gcflags=-m ./05-escape-analysis/ 2>&1 | grep before.go
```

```
05-escape-analysis/before.go:22:24: leaking param: line
05-escape-analysis/before.go:39:9: &Sample{...} escapes to heap
05-escape-analysis/before.go:43:16: leaking param content: lines
05-escape-analysis/before.go:57:19: leaking param: dst to result ~r0 level=0
05-escape-analysis/before.go:57:31: leaking param content: samples
05-escape-analysis/before.go:59:20: ... argument does not escape
05-escape-analysis/before.go:59:41: s.Name escapes to heap
05-escape-analysis/before.go:59:49: s.Value escapes to heap
05-escape-analysis/before.go:59:58: s.Time escapes to heap
05-escape-analysis/before.go:65:22: name does not escape
05-escape-analysis/before.go:66:13: make([]byte, len(name)) does not escape
05-escape-analysis/before.go:77:16: string(buf) escapes to heap
05-escape-analysis/before.go:85:27: inlining call to normalizeBefore
05-escape-analysis/before.go:85:27: make([]byte, len(name)) does not escape
05-escape-analysis/before.go:85:27: string(buf) does not escape
```

Let's read it:

- **Line 39**: the `*Sample` escapes because it's returned. `line` "leaks" because `Name` is a substring of `line` stored in that heap `Sample`. A leaking *string* parameter costs nothing by itself (no copy is made); it just means the string's bytes must stay alive.
- **Line 59**: the `...any` slice itself stays on the stack (`... argument does not escape`), but each value boxed into it escapes: `fmt` stores its arguments where the compiler can't follow them. Three boxed values, three allocations per sample.
- **Lines 66/85**: `normalizeBefore` was inlined into `CountKnownBefore` (more on inlining in the next module), and there `string(buf)` is only used as a map key, so it doesn't escape. But `make([]byte, len(name))` has a size unknown at compile time, and our names are ~48 bytes, more than the 32-byte stack buffer. "Does not escape", and still one heap allocation per name.

Want the *why* for a specific line? `-m=2` prints the data-flow path:

```bash
go build -gcflags=-m=2 ./05-escape-analysis/ 2>&1 | grep -A3 'before.go:39:9: &Sample{...} escapes to heap in'
```

```
05-escape-analysis/before.go:39:9: &Sample{...} escapes to heap in parseSampleBefore:
05-escape-analysis/before.go:39:9:   flow: ~r0 ← &{storage for &Sample{...}}:
05-escape-analysis/before.go:39:9:     from &Sample{...} (spill) at 05-escape-analysis/before.go:39:9
05-escape-analysis/before.go:39:9:     from return &Sample{...}, true (return) at 05-escape-analysis/before.go:39:2
```

> **Tip:** `-m` output is per package and can be long. Filter it with `grep` by file or line, and remember that the same line can appear twice: once in the function itself, and once more where it was inlined.

### Step 3: Fix it

Edit `labs/05-escape-analysis/after.go`:

1. **`parseSampleAfter`**: return `(Sample, bool)` instead of `(*Sample, bool)`. A 40-byte struct is cheap to copy. Returning by value means the result is written into the caller's frame.
2. **`EncodeAfter`**: drop `fmt.Appendf` and write the fields with `append` and `strconv.AppendFloat(dst, v, 'g', -1, 64)` / `strconv.AppendInt`. These take concrete types, so nothing is boxed.
3. **`CountKnownAfter`**: turn `normalize` into an append-style helper, `appendNormalized(dst []byte, name string) []byte`, and call it with a fixed-size stack buffer: `var scratch [128]byte` and `appendNormalized(scratch[:0], name)`. The array has a constant size, so it lives on the stack, and `append` only moves to the heap if a name is longer than 128 bytes. Then look up `known[string(key)]`: the compiler recognizes `m[string(b)]` and doesn't allocate a string for it. Inside the helper, copy the whole name with one `append(dst, name...)` and rewrite the copy in place: appending byte by byte does a capacity check per byte, and in Go 1.27 that loop is slower than the small allocation you're trying to remove.

Check with `go test ./05-escape-analysis/` and run `-gcflags=-m` again: the `&Sample{...}`, `s.Name`/`s.Value`/`s.Time` and `string(buf)` lines for `after.go` should be gone (the `append escapes to heap` lines left are `dst` growing, which is the caller's buffer).

### Step 4: Compare

```bash
make bench LAB=05
```

Here are the results with the reference solution (`make solution LAB=05`) on an AMD Ryzen AI MAX+ 395:

```
              │    before     │               after                │
              │    sec/op     │   sec/op     vs base               │
Sum-32           43.19µ ±  4%   32.45µ ± 5%  -24.87% (p=0.000 n=8)
Encode-32       106.47µ ±  5%   34.79µ ± 1%  -67.33% (p=0.000 n=8)
CountKnown-32    46.10µ ± 12%   33.21µ ± 4%  -27.96% (p=0.000 n=8)
geomean          59.63µ         33.47µ       -43.87%

              │    before    │                  after                  │
              │     B/op     │    B/op      vs base                    │
Sum-32          31.22Ki ± 0%   0.00Ki ± 0%  -100.00% (p=0.000 n=8)
Encode-32       31.29Ki ± 0%   0.00Ki ± 0%  -100.00% (p=0.000 n=8)
CountKnown-32   46.88Ki ± 0%   0.00Ki ± 0%  -100.00% (p=0.000 n=8)
geomean         35.78Ki                     ?                      ¹ ²
¹ summaries must be >0 to compute geomean
² ratios must be >0 to compute geomean

              │   before    │                  after                  │
              │  allocs/op  │  allocs/op   vs base                    │
Sum-32           999.0 ± 0%      0.0 ± 0%  -100.00% (p=0.000 n=8)
Encode-32       2.999k ± 0%   0.000k ± 0%  -100.00% (p=0.000 n=8)
CountKnown-32   1.000k ± 0%   0.000k ± 0%  -100.00% (p=0.000 n=8)
geomean         1.442k                     ?                      ¹ ²
¹ summaries must be >0 to compute geomean
² ratios must be >0 to compute geomean
```

## Understanding the Results

**Sum** shows the price of one small heap allocation: a 40-byte `Sample` per line (48 bytes after rounding to a size class). Parsing still dominates the time (`strconv.ParseFloat` isn't cheap), and yet removing the allocation is a solid win. And there's a part you can't see in this benchmark: every one of those objects is work for the GC later.

**Encode** is the biggest win. `fmt` is convenient, but every call parses the format string at run time and boxes each argument. The `strconv.Append*` functions are what `fmt` uses inside anyway, minus the format parsing and the boxing.

**CountKnown** is the Go 1.25+ subtlety. With names of 32 bytes or less, the "before" code would have been allocation-free already. Real-world names (like Prometheus metric names) are often longer. A fixed-size array is the classic way out, and you'll find it all over the standard library (`strconv` and `fmt` use `[64]byte`-style buffers internally). It's also a reminder that removing an allocation only pays if the replacement does less work: with Go 1.27's faster small allocations, a byte-by-byte `append` version of the solution is *slower* than "before" (we measured +12%), while the one-`append` copy is ~28% faster.

### The `AppendX` pattern

`EncodeBefore` and `EncodeAfter` both have the signature `func(dst []byte, ...) []byte`. That's not a coincidence: this is the standard library's way of avoiding allocations across an API boundary (`strconv.AppendInt`, `time.Time.AppendFormat`, `binary.AppendUvarint`, `fmt.Appendf`, `utf8.AppendRune`...). The function never allocates a buffer of its own. The **caller** decides where the memory comes from: a reused buffer, a stack array (`buf[:0]`), or `nil` if they don't care. A function that returns a fresh `[]byte` or `string` takes that choice away.

The same idea applies to readers: `io.Reader.Read(p []byte)` takes the caller's buffer instead of returning a new one.

## Key Takeaways

- Stack allocation is basically free, heap allocation costs `mallocgc` time **and** GC time
- The compiler decides with **escape analysis**, a data-flow graph of locations and assignments
- `-gcflags=-m` tells you what escapes, `-m=2` tells you why
- Common escapes: returned pointers, storing into the heap, interface arguments (`fmt`), escaping closures, sizes that are too big or unknown
- Since Go 1.25, a non-escaping `make` with a run-time size stays on the stack only up to **32 bytes**. Since 1.26 `append` gets the same 32-byte buffer
- "does not escape" ≠ "doesn't allocate": always confirm with `allocs/op`
- Return small structs by value, and let callers provide buffers (`AppendX(dst, ...)`)

## Going Further (optional)

1. Change `parseSampleAfter` back to returning a pointer, but make the `Sample` a local variable (`var s Sample; ...; return &s, true`). What does `-m` say now? (Hint: `moved to heap: s`.)
2. Shorten the names in `testNames` to less than 32 bytes and re-run `CountKnown`. What happens to the "before" allocations? Why?
3. Run the demo with `go run -gcflags='-d=variablemakethreshold=0' ./05-escape-analysis/cmd/stackmake`. This sets the 32-byte threshold to 0 and disables the Go 1.25 optimization, so you see what older Go versions did. Try `=64` too, then think about why the Go team didn't simply pick a big number (hint: every such `make` reserves that much stack, used or not).
