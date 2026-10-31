# Escape Analysis

In the previous modules we reduced the number of allocations by asking for the right amount of memory up front. Now we go one step further: we stop allocating on the heap at all. Go has no `new` vs `malloc` choice in the language. You write `&T{}` or `make([]byte, n)`, and the **compiler** decides where that memory lives. This module is about how it decides, how to read its decisions, and how to write code that lets it pick the stack.

## Learning Objectives

By the end of this module, you will:

- Understand the cost of stack and heap allocations
- Read escape analysis output with `-gcflags=-m` and `-m=2`
- Recognize common causes of heap allocations
- Reduce allocations by returning values, avoiding boxing and reusing buffers

## The Concept: Stack or Heap?

Every goroutine has a **stack**. Each function call gets its own space on it for its local variables (its *stack frame*), and when the function returns, that space is thrown away all at once. Allocating on the stack is basically free, freeing is free too, and the garbage collector never has to look at it.

The **heap** is where everything else goes. Allocating there takes real work from the runtime, and later the garbage collector has to find out that the memory is no longer used and free it. We'll measure that cost in [GC Pressure](12-gc-pressure.md).

In Go you don't choose between the two. You write `&T{}` or `make([]byte, n)`, and the compiler decides. The rule is simple:

![Values that don't outlive their function stay on the stack; values referenced after the function returns escape to the heap](images/05-stack-heap.webp)

- If a value is only used while the function runs, it can live on the **stack**.
- If it can still be used after the function returns (for example, because you return a pointer to it), it has to **escape** to the **heap**, because the stack frame will be gone by then.

The part of the compiler that works this out is called **escape analysis**. It follows every pointer through your code, including into the functions you call, to see where it can end up.

### The usual suspects

These are the most common reasons a value escapes:

1. **Returning a pointer** to a local variable.
2. **Storing it somewhere that lives longer**: a global, a map, a slice or a struct that is already on the heap, or a channel.
3. **Interfaces**: passing a value as an interface to something the compiler can't see through, like `fmt.Printf(format string, a ...any)`. ([Interfaces and Boxing](08-interfaces-boxing.md) is all about this.)
4. **Closures** that capture variables and are themselves kept, for example by passing them to `go`.
5. **Size**: too big for the stack, or not known at compile time.

### Asking the compiler

`go build -gcflags=-m` prints the compiler's decisions. These are the messages you'll see most:

| Message | Meaning |
|---|---|
| `x does not escape` | `x` stays on the stack |
| `moved to heap: x` | the local variable `x` lives on the heap |
| `&T{...} escapes to heap` | that allocation goes to the heap |
| `leaking param: p` | what `p` points to may outlive the call |

Use `-m=2` instead to see *why* something escapes. We'll use both in the lab.

## The Lab

```
labs/05-escape-analysis/
  before.go         naive implementations (baseline, don't edit)
  after.go          your copy: make this one fast
  solution.go       reference solution (built with -tags solution)
  lab_test.go       correctness test + benchmarks
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

- **Line 39**: returning `*Sample` puts it on the heap. Its `Name` keeps the original string's bytes alive without copying them.
- **Line 59**: passing the three fields to `fmt` as `any` causes three allocations per sample.
- **Lines 66/85**: the map lookup avoids copying `string(buf)`, but the temporary byte buffer still needs one heap allocation per name.

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

1. **`parseSampleAfter`**: return `(Sample, bool)` instead of a pointer to keep the sample off the heap.
2. **`EncodeAfter`**: replace `fmt.Appendf` with `append`, `strconv.AppendFloat` and `strconv.AppendInt` to avoid boxing.
3. **`CountKnownAfter`**: reuse a `var scratch [128]byte` buffer. Pass `scratch[:0]` to an `appendNormalized(dst []byte, name string) []byte` helper that copies the name with `append(dst, name...)` and normalizes it in place. Look up the result with `known[string(key)]`.

Run `go test ./05-escape-analysis/` to check correctness, then `-gcflags=-m` again to see which heap allocations disappeared.

### Step 4: Compare

```bash
make bench LAB=05
```

Here are the results with the reference solution (`make solution LAB=05`) on my machine:

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

**Sum** avoids allocating a `Sample` on the heap for every line by returning it by value. Reading the numbers from the text still takes most of the time, but removing the allocation helps and gives the garbage collector less work later.

**Encode** is the biggest win. Each `fmt` call reads the format string and wraps the arguments as interface values (boxing). `strconv.Append*` writes the numbers directly into the output buffer, avoiding that extra work.

**CountKnown** avoids allocating a temporary buffer for each name by using a fixed-size array. For names of 32 bytes or less, Go already avoids that allocation, but our names are longer. How we fill the buffer matters too: copying the whole name with one `append` is about 28% faster than before, while appending byte by byte was 12% slower. Fewer allocations alone don't guarantee faster code.

## Key Takeaways

- Heap allocations add work for the runtime and garbage collector.
- Escape analysis helps the compiler decide where memory lives.
- Use `-gcflags=-m` to see its decisions and `-m=2` for more detail.
- Return small structs by value, avoid unnecessary boxing and reuse buffers.
- Confirm the improvement with `allocs/op` and benchmark timings.

## Going Further (optional)

1. Change `parseSampleAfter` back to returning a pointer, but make the `Sample` a local variable (`var s Sample; ...; return &s, true`). What does `-m` say now? (Hint: `moved to heap: s`.)
2. Shorten the names in `testNames` to less than 32 bytes and re-run `CountKnown`. What happens to the "before" allocations? Why?
3. In `BenchmarkEncode`, pass `nil` instead of `buf[:0]` on each iteration. How do `B/op` and `allocs/op` change when you stop reusing the output buffer?
