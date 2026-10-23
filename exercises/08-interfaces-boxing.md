# Interfaces, Boxing and Generics

> **Want to learn more?** Read [`runtime/iface.go`](https://github.com/golang/go/blob/go1.27.0/src/runtime/iface.go) (the `convT*` functions and `staticuint64s`), `dataWord` in [`cmd/compile/internal/walk/convert.go`](https://github.com/golang/go/blob/go1.27.0/src/cmd/compile/internal/walk/convert.go), the devirtualizer in [`cmd/compile/internal/devirtualize/devirtualize.go`](https://github.com/golang/go/blob/go1.27.0/src/cmd/compile/internal/devirtualize/devirtualize.go), and the [GC shape stenciling design](https://github.com/golang/proposal/blob/master/design/generics-implementation-gcshape.md).

Interfaces are everywhere in Go: `io.Reader`, `error`, `any`, `fmt.Stringer`, every plug-in point of every library. They are also the place where the three previous modules come together. Putting a value into an interface can allocate (escape analysis), calling a method through an interface can't be inlined (inlining), and because the compiler can't see the callee, everything you pass to it escapes. In this module we look at what an interface is in memory, when it costs something, and what to do about it, including whether generics help (spoiler: not the way you'd think).

## Learning Objectives

By the end of this module, you will:

- Know the memory layout of interface values (`iface`, `eface`, `itab`)
- Understand **boxing**: when converting a value to an interface allocates, and when it doesn't
- Know why dynamic dispatch blocks inlining and makes arguments escape
- Know when the compiler **devirtualizes** an interface call
- Understand Go's generics implementation (**GC shape stenciling** with dictionaries) and its performance consequences
- Remove boxing with a concrete "tagged value" type, the way `log/slog` does

## The Concept: Two Words and a Table

An interface value is always two machine words (`runtime/runtime2.go`):

```go
type iface struct {   // an interface with methods, like io.Reader
	tab  *itab
	data unsafe.Pointer
}

type eface struct {   // the empty interface, any
	_type *_type
	data  unsafe.Pointer
}
```

![An interface value is two words: an itab pointer and a data pointer. Storing a non-pointer value boxes it on the heap](images/08-iface.webp)

The first word says what's inside. For `any` it's just the type descriptor. For a non-empty interface it's an **itab** (`internal/abi/iface.go`):

```go
type ITab struct {
	Inter *InterfaceType
	Type  *Type
	Hash  uint32     // copy of Type.Hash. Used for type switches.
	Fun   [1]uintptr // variable sized. fun[0]==0 means Type does not implement Inter.
}
```

`Fun` is the method table: one code pointer per interface method, for this concrete type. A call `r.Read(p)` compiles to "load `tab`, load `tab.Fun[i]`, call it indirectly, passing `data` as the receiver". The itab for each (interface, type) pair is built once (at compile time when possible, or on first use and then cached in a hash table) so the dispatch itself is cheap: two loads and an indirect call.

### Boxing: where does `data` point?

The second word is **always a pointer**. If the value is itself a pointer (or pointer-shaped: a map, a chan, a func, a struct with one pointer field), it goes straight into `data`. Anything else must be stored somewhere, and `data` points to it. That's boxing.

The compiler decides where the box lives in `walk/convert.go` (`dataWord`), trying to avoid an allocation, in this order:

1. **Zero-sized** values point to a global, `zerobase`.
2. **bool and single-byte** values point into `runtime.staticuint64s`, a table of the numbers 0–255.
3. **Constants and zero values** of composite literals point to read-only static data.
4. **Read-only globals** are used directly.
5. If the interface value **does not escape** (and is ≤ 1024 bytes), the box is a **stack temporary**.

Only if none of those apply does it call the runtime, picking a specialized function by size and shape (`convT16`, `convT32`, `convT64`, `convTstring`, `convTslice`, or the generic `convT`/`convTnoptr`). Here's `convT64` from `runtime/iface.go`:

```go
func convT64(val uint64) (x unsafe.Pointer) {
	if val < uint64(len(staticuint64s)) {
		x = unsafe.Pointer(&staticuint64s[val])
	} else {
		x = mallocgc(8, uint64Type, false)
		*(*uint64)(x) = val
	}
	return
}
```

So: boxing an `int` that is 0–255 is free even at run time, a larger one is an 8-byte allocation. Boxing a non-empty `string` allocates 16 bytes for the string header (`convTstring`), not for its bytes. A `time.Duration` is an `int64`, so it's `convT64` too. Values of 2, 4 or 8 bytes go through `convT16/32/64`, which only have the small-number shortcut for values below 256.

**Rule 5 is the key one**: boxing only allocates when the interface value escapes. And interface values escape a lot, because of the next point.

### Dynamic dispatch is a wall

When the compiler sees `h.Handle(msg, fields)` with `h` of interface type, it doesn't know which function will run. So:

- It **can't inline** the call. The method's body isn't known.
- Escape analysis **assumes the worst** for all arguments: they leak to the heap. Every pointer you pass, and every boxed value reachable from it, escapes.

That second point is what makes interface-heavy APIs allocate. A value boxed into `any` that is only used locally is boxed on the stack. The same value passed to a `Handler` interface goes to the heap.

### Devirtualization

If the compiler *can* prove the concrete type behind an interface, it rewrites the call into a direct call (and then may inline it). This is static devirtualization, and Go 1.26 improved the analysis that finds the concrete type (`go126ImprovedConcreteTypeAnalysis` in `devirtualize.go`, still `true` and unchanged in Go 1.27). For example:

```go
func Count(vals []float64, min float64) (n int) {
	var f Filter = Threshold{min}
	for _, v := range vals {
		if f.Keep(v) {
			n++
		}
	}
	return n
}
```

```
./d.go:11:12: devirtualizing f.Keep to Threshold
./d.go:11:12: inlining call to Threshold.Keep
./d.go:9:26: Threshold{...} does not escape
```

This works when the concrete type is visible in the same function, possibly after inlining. It doesn't work when the interface comes from a parameter of a non-inlined function, a struct field or a slice of interfaces. In those cases there's **profile-guided devirtualization**, which adds a "if it's the usual type, call it directly" check based on a profile (see [Profile-Guided Optimization](18-pgo.md)).

### Generics: GC shape stenciling

Go generics are not C++ templates. The compiler doesn't generate one copy of a generic function per type argument. It generates one copy per **GC shape**: all pointer types share one shape, and other types are grouped by their underlying type. Each instantiation also receives a hidden **dictionary** argument with the type information, including the method pointers of the type arguments.

The consequence: inside the generic function, a method call on a type parameter is an indirect call through the dictionary, much like an interface call. The devirtualizer explicitly gives up on it:

```go
// If typ is a shape type, then it was a type argument originally
// and we'd need an indirect call through the dictionary anyway.
// We're unable to devirtualize this call.
if typ.IsShape() {
	return
}
```

Let's measure. The lab has a demo that calls the same tiny method 10,000 times in four ways:

```bash
cd labs
go run ./08-interfaces-boxing/cmd/dispatch
```

```
interface                       10.71 µs/op
generic, value type arg         10.61 µs/op
generic, pointer type arg       10.51 µs/op
concrete type                    4.10 µs/op
```

The generic versions are no faster than the interface, both with a value type argument and a pointer one (the three indirect versions move around by ±20% from run to run, the concrete one doesn't). Only the concrete type gets `Keep` inlined (`-m` shows `inlining call to Threshold.Keep` only for `countConcrete`). Generics are great for type-safe containers and algorithms over `==`/`<`/arithmetic (those operations are compiled for the shape directly). They are **not** a tool for getting rid of method dispatch costs. Go 1.27's new **generic methods** (methods with their own type parameters) are compiled the same way: `func (s *Stats) Count[F Filter](vals []float64, f F)` becomes `(*Stats).Count[go.shape.struct { Min float64 }]` plus a dictionary, and in a quick benchmark it ran as fast as the generic function, ~4x slower than a concrete method. (And a generic method can't implement an interface method, so it doesn't replace interfaces as a plug-in point either.)

## The Lab

```
labs/08-interfaces-boxing/
  before.go         naive implementations (baseline, don't edit)
  after.go          your copy: make this one fast
  solution.go       reference solution (built with -tags solution)
  lab_test.go       correctness test + benchmarks
  cmd/dispatch/     the demo program above
```

There are two parts:

1. **`Log`**: a tiny structured logger in the style of `log/slog`. Fields are `{Key string; Value any}`, and the logger sends them to a pluggable `Handler` interface. We log 1,000 HTTP requests with 5 fields each.
2. **`Stats`**: count, sum and max of the values kept by a `Filter` interface, over 10,000 values.

### Step 1: Measure the baseline

```bash
make bench LAB=08
```

`Log` does **5,000 allocations** per 1,000 requests. `Stats` allocates nothing, but keep an eye on its time.

### Step 2: Find the boxes

```bash
cd labs
go build -gcflags=-m ./08-interfaces-boxing/ 2>&1 | grep before.go | grep -E 'escape|leak'
```

The interesting lines:

```
08-interfaces-boxing/before.go:45:42: fields does not escape
08-interfaces-boxing/before.go:71:41: leaking param: fields
08-interfaces-boxing/before.go:78:9: ... argument escapes to heap
08-interfaces-boxing/before.go:79:10: v escapes to heap
08-interfaces-boxing/before.go:80:10: v escapes to heap
08-interfaces-boxing/before.go:81:7: v escapes to heap
08-interfaces-boxing/before.go:82:7: v escapes to heap
08-interfaces-boxing/before.go:83:12: v escapes to heap
08-interfaces-boxing/before.go:102:34: leaking param: f
```

- Line 45: `(*TextHandler).Handle` itself doesn't keep `fields`. If the compiler could see it, nothing would escape.
- Line 71: but `Info` calls `l.h.Handle(msg, fields)` through the interface, so `fields` leaks.
- Lines 78–83 (inlined into `LogRequestsBefore`): the variadic `[]Field` slice escapes, and so does every boxed value `v`.

Let's count the allocations per request: the slice of 5 `Field`s (5 × 32 = 160 bytes), `"GET"` and the path (`convTstring`, 16 bytes each), `Bytes` (≥ 256, `convT64`, 8 bytes), `Latency` (`convT64`, 8 bytes). That's 5 allocations and 208 bytes. And `Status`? It's `200`, below 256, so it points into `staticuint64s` and costs nothing. A `404` would cost an allocation.

For `Stats`, line 102 says `leaking param: f`, and the profile would show the `Threshold.Keep` method as a separate function called 10,000 times (through an autogenerated `(*Threshold).Keep` wrapper, because the itab's methods receive the boxed `data` pointer).

### Step 3: Fix it

Edit `labs/08-interfaces-boxing/after.go`.

**Part 1, step 1: remove the boxing.** Replace `Value any` with a small concrete type that can hold every kind of value we log without an interface:

```go
type Value struct {
	kind Kind   // KindString, KindInt, KindDuration
	num  uint64 // ints and durations
	str  string // strings
}
```

Update `StringAfter`, `IntAfter`, `DurationAfter` and the `switch` in `TextHandlerAfter.Handle` (switch on `kind` instead of the dynamic type). This is exactly what `log/slog.Value` does (more compactly: it stores the kind, or a pointer to the string's bytes, in an `any` field whose contents never need boxing).

Run the benchmark: **5 allocations per request become 1**, but the bytes go *up* (208 → 240 per request), because the remaining allocation is the `[]FieldAfter` slice and a `FieldAfter` is now 48 bytes. We measured that intermediate step: 5,000 → 1,000 allocs/op, 203 KiB → 234 KiB.

**Part 1, step 2: stop the slice from escaping.** The slice escapes because it's passed to an interface method. Pass a **value** instead: a `Record` with the fields copied into a fixed-size array:

```go
type Record struct {
	Msg   string
	n     int
	front [6]FieldAfter
	back  []FieldAfter // only for the rare entry with more than 6 fields
}

type HandlerAfter interface {
	Handle(r Record)
}
```

`Info` copies `fields` into `r.front` and calls `l.h.Handle(r)`. A struct passed by value is copied into the call's arguments, so only its *contents* (the strings) can leak, never the variadic slice itself. This is how `log/slog.Record` works too: its `front [nAttrsInline]Attr` array (5 attrs) exists exactly to avoid this allocation.

**Part 2: `Stats`.** Try making `StatsAfter` generic first:

```go
func StatsAfter[F FilterAfter](vals []float64, f F) (n int, sum, hi float64)
```

Measure. Then try the concrete type: `func StatsAfter(vals []float64, t ThresholdAfter) ...`. The benchmark calls `StatsAfter(vals, ThresholdAfter{Min: minFlag})`, which compiles with all three signatures.

### Step 4: Compare

```bash
make bench LAB=08
```

Here are the results with the reference solution (`make solution LAB=08`) on an AMD Ryzen AI MAX+ 395:

```
         │    before    │               after                │
         │    sec/op    │   sec/op     vs base               │
Log-32     108.25µ ± 3%   62.44µ ± 2%  -42.32% (p=0.000 n=8)
Stats-32    26.88µ ± 0%   10.30µ ± 0%  -61.70% (p=0.000 n=8)
geomean     53.94µ        25.35µ       -53.00%

         │      before      │                after                │
         │       B/op       │    B/op     vs base                 │
Log-32     208029.00 ± 0%     14.50 ± 3%  -99.99% (p=0.000 n=8)
Stats-32       0.000 ± 0%     0.000 ± 0%        ~ (p=1.000 n=8) ¹
geomean                   ²               -99.17%               ²
¹ all samples are equal
² summaries must be >0 to compute geomean

         │    before     │                  after                  │
         │   allocs/op   │  allocs/op   vs base                    │
Log-32     5.000k ± 0%     0.000k ± 0%  -100.00% (p=0.000 n=8)
Stats-32    0.000 ± 0%      0.000 ± 0%         ~ (p=1.000 n=8) ¹
geomean                ²                ?                      ² ³
¹ all samples are equal
² summaries must be >0 to compute geomean
³ ratios must be >0 to compute geomean
```

## Understanding the Results

**Log**: 5,000 allocations and 203 KiB per 1,000 requests drop to zero (the ~15 B/op left are the output buffer growing now and then), and the time drops by ~40%. We still go through the `Handler` interface for every entry. We just stopped giving it things that need the heap. Note that we're now copying a ~330-byte `Record` per call, and it's still much faster than allocating. Copying memory that's hot in the cache is cheap; allocating it costs `mallocgc`, then GC work.

**Stats**: 2.6x faster with the concrete type. The comparison `v >= t.Min` is inlined into the loop, which is 10,000 indirect calls fewer. With the generic version you should see little or no gain, as in the `cmd/dispatch` demo.

When should you keep the interface? Most of the time! An interface call costs a couple of nanoseconds. It only matters when it's in a hot loop with little work per call, or when it causes allocations through escapes. When it does matter, the options are:

- Use the **concrete type** in the hot path (or a type switch that picks a concrete fast path for the common types)
- Move the interface boundary **outside the loop**: one call per batch instead of one per element (`io.Reader` reads a `[]byte`, not a byte at a time)
- Pass **values** instead of pointers/slices through the interface, so nothing escapes
- Use a profile and let **PGO** devirtualize the hot call sites ([module 18](18-pgo.md))

## Key Takeaways

- An interface value is two words: a type/itab pointer and a data pointer
- Non-pointer values are **boxed**. Zero-size values, bools, bytes and integers 0–255 use static storage, constants use read-only data, and non-escaping boxes go on the stack. Everything else is a `convT*` heap allocation
- Interface method calls can't be inlined, and **all their arguments escape**
- The compiler **devirtualizes** when it can see the concrete type (improved in Go 1.26). PGO handles more cases
- Go generics use **GC shape stenciling + dictionaries**: method calls on type parameters are still indirect, and generics are no faster than interfaces for that
- A concrete tagged value (like `slog.Value`) and passing records by value (like `slog.Record`) avoid boxing allocations across an interface

## Going Further (optional)

1. Change `Status` in `testRequests` to `1000 + i%10`. How many allocations per request does the "before" version do now? Why?
2. In the solution, change `Handle(r Record)` to `Handle(r *Record)`. What does `-m` say about the `Record` in `Info`?
3. Write a `StatsAfter` that takes a `[]FilterAfter` (several filters, all must keep the value). Then try changing the interface to `KeepAll(vals []float64, keep []bool)`, one call per batch. How does that compare?
4. Run `go build -gcflags=-m=2` on `cmd/dispatch` and look for `devirtualizing`. Then remove the `//go:noinline` from `countIface`. What changes, and why?
