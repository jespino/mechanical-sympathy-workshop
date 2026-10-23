# Strings and Bytes

> **Want to learn more?** Read [`runtime/string.go`](https://github.com/golang/go/blob/go1.27.0/src/runtime/string.go) (look for `slicebytetostring`, `stringtoslicebyte`, `concatstrings` and `tmpBuf`), the places where the compiler skips the copy in [`cmd/compile/internal/walk/order.go`](https://github.com/golang/go/blob/go1.27.0/src/cmd/compile/internal/walk/order.go), and [`strings/builder.go`](https://github.com/golang/go/blob/go1.27.0/src/strings/builder.go).

Strings are everywhere: log lines, HTTP headers, map keys, JSON, metric names. Go makes them easy to use, and that ease hides copies. Every conversion between `string` and `[]byte` can allocate and copy, and every `+` builds a brand new string. Sometimes the compiler removes the copy for you, and sometimes it can't. In this module we look at how strings are represented, which conversions cost what (measured, not guessed), and how to build and format text without making garbage.

## Learning Objectives

By the end of this module, you will:

- Know what a string header is, and why immutability forces conversions to copy
- Know the runtime functions behind `string(b)`, `[]byte(s)` and `+`, and the 32-byte stack buffer they can use
- Know exactly which conversions the compiler turns into free reinterpretations, verified with `-benchmem`
- Replace `+=` loops with `strings.Builder` + `Grow`, and `fmt.Sprintf` with `strconv.Append*`
- Avoid the allocation hidden in `m[string(b)]++`
- Know what `unsafe.String` and `unsafe.StringData` do, and why you should almost never need them

## The Concept: Immutable Bytes

### Two headers

A string is a two-word header: a pointer and a length (`stringStruct` in `runtime/string.go`). A slice has a third word, the capacity:

```go
type stringStruct struct {   type slice struct {
    str unsafe.Pointer           array unsafe.Pointer
    len int                      len   int
}                                cap   int
                             }
```

![A string header has a pointer and a length; a slice header adds a capacity. Converting between them copies the bytes](images/04-headers.webp)

The important difference is not the extra word. It's that **the bytes behind a string can never change**. That's what makes strings safe to share: substrings (`s[3:10]`) and assignments just copy the header, never the bytes, and a string can be a map key or be passed between goroutines without locks.

The flip side: a `[]byte` *can* change. So converting between them must copy, otherwise someone could mutate the bytes of a "string" that others are holding.

### What the runtime does

`string(b)` compiles to a call to `slicebytetostring`:

```go
func slicebytetostring(buf *tmpBuf, ptr *byte, n int) string {
    if n == 0 {
        return ""
    }
    ...
    if n == 1 {
        p := unsafe.Pointer(&staticuint64s[*ptr])
        ...
        return unsafe.String((*byte)(p), 1)
    }

    var p unsafe.Pointer
    if buf != nil && n <= len(buf) {
        p = unsafe.Pointer(buf)
    } else {
        p = mallocgc(uintptr(n), nil, false)
    }
    memmove(p, unsafe.Pointer(ptr), uintptr(n))
    return unsafe.String((*byte)(p), n)
}
```

Three fast paths are visible here:

- Empty and **one-byte strings never allocate**: single bytes point into a static table, `staticuint64s`.
- `buf` is a `tmpBuf`, a 32-byte array (`const tmpStringBufSize = 32`, "There is no fundamental theory behind this number"). The compiler passes one, on the caller's stack, **when escape analysis proves the result doesn't escape**. Short, local strings are then copied but not allocated.
- Everything else: `mallocgc` + `memmove`.

`[]byte(s)` is `stringtoslicebyte`, the mirror image (same `tmpBuf` trick), and `a + b + c` is `concatstrings`, which sums the lengths, allocates once (or uses `tmpBuf`) and copies every operand. A concatenation is one allocation, but a `+=` in a loop is one allocation **per iteration**, each copying everything accumulated so far.

### When the compiler skips the copy

The copy is only needed if someone could observe the bytes changing. When the compiler can prove nobody can, it emits `OBYTES2STRTMP` / `OSTR2BYTESTMP`: a "conversion" that just reuses the other value's pointer. The cases are listed in `walk/order.go`, `walk/switch.go`, the SSA rules, and (for `[]byte(s)`) escape analysis:

- **Map lookups**: `m[string(b)]`. From `mapKeyReplaceStrConv`: "the allocation of backing bytes for the string can be avoided by reusing the []byte backing array"
- **Comparisons**: `string(b) == "..."`, `<`, etc. "String comparison does not memorize the strings for later use, so it is safe"
- **`switch string(b)`**, when all the cases are side-effect free
- **Concatenation operands**, but only when the concatenation also has a non-empty literal: `"<" + string(b) + ">"` (the `haslit && hasbyte` check). The result itself is still a new string
- **`for i, c := range []byte(s)`**: "It is safe because the storage cannot be mutated"
- **`[]byte(s)` that is never mutated** (Go 1.22+). Escape analysis tracks whether the bytes may be written: "If the result of a string->[]byte conversion is never mutated, then it can simply reuse the string's memory directly." `-gcflags=-m` prints `zero-copy string->[]byte conversion` when it applies

Let's check all of it. The lab includes a program that benchmarks each case with `testing.Benchmark`, using `//go:noinline` helpers so we measure the conversion and nothing else:

```bash
cd labs
go run ./04-strings-bytes/cmd/conversions
```

```
case                                ns/op     B/op  allocs/op
string(b), result escapes           12.27       48          1
string(b), local, 13 bytes           3.74        0          0
string(b), local, 44 bytes          11.14       48          1
string(b), 1 byte                    2.83        0          0
m[string(b)] lookup                  8.10        0          0
m[string(b)]++                      29.22       48          1
string(b) == "..."                   3.89        0          0
switch string(b)                     4.72        0          0
"<" + string(b) + ">"               18.71       48          1
s + string(b)                       29.89      144          2
for range []byte(s)                  8.00        0          0
[]byte(s), read only                 1.83        0          0
[]byte(s), mutated                  12.65       48          1
bytes.IndexByte([]byte(s))          15.91       48          1
[]byte(s), result escapes           13.09       48          1
```

(AMD Ryzen AI MAX+ 395. The 44-byte strings take a 48-byte size class.) A few results worth staring at:

- **The `tmpBuf` limit is real**: the same local conversion is free at 13 bytes and allocates at 44.
- **`m[string(b)]` is free, `m[string(b)]++` is not.** `++` is an assignment: if the key is new, the map must store it, so the compiler has to make a real string, every time, even when the key already exists.
- **`s + string(b)` costs two allocations**, while `"<" + string(b) + ">"` costs one: without a literal operand, the conversion isn't optimized away.
- **`bytes.IndexByte([]byte(s))` copies.** The zero-copy rule works through calls to Go functions, because escape analysis knows what they do with their parameters. `bytes.IndexByte` is implemented in assembly, so the compiler must assume it could write. (For this specific case, just use `strings.IndexByte`.)

### Building strings: `strings.Builder`

`strings.Builder` is a `[]byte` that you append to, plus one trick in `String()`:

```go
func (b *Builder) String() string {
    return unsafe.String(unsafe.SliceData(b.buf), len(b.buf))
}
```

No copy: the Builder hands its buffer over as the string's bytes. That's safe only because the Builder never modifies bytes it has already written (it only appends, and `grow` copies into a new array), and because `copyCheck` panics if you copy a non-zero Builder by value. Without `Grow`, the buffer still grows like a slice (`grow` allocates `2*cap + n`), so if you can compute the final size, call `Grow(n)` first and you get exactly one allocation.

### Formatting numbers: `fmt` vs `strconv`

`fmt.Sprintf("%s %g %d\n", name, value, ts)` is convenient, but it parses the format string at run time, receives its arguments as `...any` (so each non-pointer value is **boxed** into an interface, which usually allocates), and returns a new string. `strconv.AppendInt(dst, v, 10)`, `strconv.AppendFloat(dst, v, 'g', -1, 64)` and `strconv.AppendQuote` write directly into a byte slice you own. When `dst` has room, they don't allocate at all.

### `unsafe.String` and `unsafe.StringData`

Since Go 1.20, `unsafe.String(ptr, len)` builds a string from a pointer and a length, and `unsafe.StringData(s)` returns the pointer behind a string. Together with `unsafe.Slice`/`unsafe.SliceData`, they let you convert without copying, exactly as `strings.Builder` does internally. The catch: **you take over the compiler's job of proving the bytes never change.** If you turn a `[]byte` into a string this way and later write to that slice (or the buffer it came from gets reused, as with `bufio` or network buffers), you've created a string that changes. That breaks map keys, and causes bugs that show up far from where they were introduced. Reach for it only when a profile proves the copy matters, the buffer's lifetime is fully under your control, and you've first checked whether one of the compiler's free cases above already covers you.

## The Lab

```
labs/04-strings-bytes/
  before.go           naive implementations (baseline, don't edit)
  after.go            your copy: make this one fast
  solution.go         reference solution (built with -tags solution)
  lab_test.go         correctness test + benchmarks
  cmd/conversions/    the conversion table above
```

Three functions from a small metrics exporter:

| Function | What it does | The problem |
|---|---|---|
| `RenderLabels(labels)` | Renders `{env="prod",host="web-1",...}` | `+=` in a loop |
| `AppendSamples(dst, samples)` | Appends `name value timestamp\n` lines to a buffer | `fmt.Sprintf` per line |
| `CountTokens(tokens [][]byte)` | Counts byte-slice tokens in a `map[string]int` | `counts[string(t)]++` |

### Step 1: Measure the baseline

```bash
make bench LAB=04
```

Look at `allocs/op` in particular: 20 for rendering 10 labels, about 4 per line for `AppendSamples`, and about one per token for `CountTokens`.

### Step 2: Find the evidence

Ask the compiler what it thinks of the conversions in `before.go`:

```bash
cd labs
go build -gcflags=-m ./04-strings-bytes/ 2>&1 | grep before.go
```

```
04-strings-bytes/before.go:28:32: l.Name + "=\"" + l.Value + "\"" escapes to heap
04-strings-bytes/before.go:38:38: s.Name escapes to heap
04-strings-bytes/before.go:38:46: s.Value escapes to heap
04-strings-bytes/before.go:38:55: s.Timestamp escapes to heap
04-strings-bytes/before.go:49:17: string(t) escapes to heap
```

Every one of those is an allocation in the loop. `s.Value escapes to heap` is odd at first sight: a `float64` can't escape. What escapes is the *interface* it gets wrapped in to be passed to `Sprintf`.

Then ask the allocation profiler where the allocations come from. `-memprofilerate=1` records every single allocation instead of sampling:

```bash
go test -run '^$' -bench 'AppendSamples/impl=before' -memprofile mem.prof -memprofilerate 1 ./04-strings-bytes/
go tool pprof -sample_index=alloc_objects -disasm 'AppendSamplesBefore' 04-strings-bytes.test mem.prof | grep CALL
```

```
317000     317000     CALL runtime.convTstring(SB)   ;... before.go:38
157643     157643     CALL runtime.convT64(SB)       ;... before.go:38
159204     159204     CALL runtime.convT64(SB)       ;... before.go:38
     .     317036     CALL fmt.Sprintf(SB)           ;... before.go:38
```

That's the boxing: `convTstring` for the name, `convT64` for the float and for the int64, each allocating before `fmt.Sprintf` is even called. Then `Sprintf` allocates the result. Four allocations per line. Remove `mem.prof` and `04-strings-bytes.test` when done.

### Step 3: Fix it

Edit `labs/04-strings-bytes/after.go`:

1. **`RenderLabelsAfter`**: compute the final length (`2` for the braces, plus `len(name) + len(value) + 4` per label for `=`, two quotes and a comma), `Grow` a `strings.Builder` to that size, and write the pieces with `WriteString` / `WriteByte`.
2. **`AppendSamplesAfter`**: append the parts directly: `append(dst, s.Name...)`, `append(dst, ' ')`, `strconv.AppendFloat(dst, s.Value, 'g', -1, 64)` (the same output as `%g`), `strconv.AppendInt(dst, s.Timestamp, 10)`, `'\n'`.
3. **`CountTokensAfter`**: make updating an existing token a pure lookup. Use a `map[string]*int`: if `counters[string(t)]` returns a pointer, increment through it. Only insert (and allocate the key) the first time a token is seen. Convert to a `map[string]int` at the end.

`go test ./04-strings-bytes/` checks that the output is byte-for-byte the same.

### Step 4: Compare

```bash
make bench LAB=04
```

Results with the reference solution (`make solution LAB=04`) on an AMD Ryzen AI MAX+ 395:

```
                 │    before    │               after                │
                 │    sec/op    │   sec/op     vs base               │
RenderLabels-32    509.95n ± 7%   74.86n ± 6%  -85.32% (p=0.002 n=6)
AppendSamples-32   124.07µ ± 3%   35.83µ ± 1%  -71.12% (p=0.002 n=6)
CountTokens-32     226.76µ ± 2%   68.54µ ± 1%  -69.77% (p=0.002 n=6)

                 │    before     │                  after                   │
                 │     B/op      │     B/op      vs base                    │
RenderLabels-32      2608.0 ± 0%     224.0 ± 0%   -91.41% (p=0.002 n=6)
AppendSamples-32    78.24Ki ± 0%    0.00Ki ± 0%  -100.00% (p=0.002 n=6)
CountTokens-32     51.320Ki ± 0%   2.578Ki ± 0%   -94.98% (p=0.002 n=6)

                 │    before     │                  after                  │
                 │   allocs/op   │  allocs/op   vs base                    │
RenderLabels-32      20.000 ± 0%    1.000 ± 0%   -95.00% (p=0.002 n=6)
AppendSamples-32     3.999k ± 0%   0.000k ± 0%  -100.00% (p=0.002 n=6)
CountTokens-32     10007.00 ± 0%    43.00 ± 0%   -99.57% (p=0.002 n=6)
```

## Understanding the Results

**RenderLabels**: 20 allocations = 10 label concatenations + 9 commas + the closing brace. Each `+=` allocates a new string and copies the whole prefix, so the bytes add up quadratically: 2,608 bytes allocated to produce a 209-byte result. With `Grow`, it's one 224-byte allocation (the next size class up), and `String()` doesn't copy it.

**AppendSamples**: from 4,000 allocations to **zero**. The benchmark reuses one buffer across iterations (`buf = fn(buf[:0], samples)`), as a real exporter would, and once that buffer is big enough, nothing in the `strconv` path allocates. The time drops 3.5x because we skip parsing the format string, the interface boxing and the reflection-driven `fmt` machinery.

**CountTokens**: 10,000 tokens but only 17 distinct words. The before version allocated a key for every token. The after version allocates 17 keys and 17 counters; the remaining 9 allocations are the internal storage of the two maps. The extra pass converting `map[string]*int` to `map[string]int` costs less than what we saved. If you control the API, return the `map[string]*int` (or a slice of counts) and skip that pass too.

> **Rule of thumb:** a conversion only costs something when its result *outlives the expression that uses it*, or is *written to*. Lookups, comparisons and reads are free. Storing and mutating are not.

## Key Takeaways

- A string is an immutable `(ptr, len)`. Sharing and slicing it is free; converting to or from `[]byte` copies, because one side can mutate
- Non-escaping conversions and concatenations of up to 32 bytes use a stack `tmpBuf`; 0- and 1-byte strings never allocate
- The compiler removes the copy for map lookups, comparisons, `switch`, concatenations with a literal, `range []byte(s)`, and `[]byte(s)` that is never written
- `m[string(b)]++` and `m[string(b)] = v` always allocate the key. Look up first, store pointers if you need to update
- `+=` in a loop is quadratic copying. Use `strings.Builder` and `Grow` to the final size
- `strconv.Append*` into a reused buffer beats `fmt.Sprintf`: no format parsing, no boxing, no new string
- `unsafe.String` moves the immutability proof from the compiler to you. Measure first

## Going Further (optional)

1. In `RenderLabelsAfter`, remove the `Grow` call. How many allocations do you get now, and why that number? (Hint: `grow` asks for `2*cap + n`.)
2. Rewrite `AppendSamplesBefore` with `fmt.Appendf(dst, ...)` instead of `Sprintf`. Which allocations disappear, and which remain?
3. Run `go build -gcflags=-m ./04-strings-bytes/cmd/conversions` and match each `zero-copy string->[]byte conversion` and `does not escape` message to a row of the table.
