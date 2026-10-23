# Struct Layout and Size Classes

> **Want to learn more?** Read how the compiler lays out structs in [`cmd/compile/internal/types/size.go`](https://github.com/golang/go/blob/go1.27.0/src/cmd/compile/internal/types/size.go) (look for `calcStructOffset` and `CalcStructSize`), the allocator's size class table in [`internal/runtime/gc/sizeclasses.go`](https://github.com/golang/go/blob/go1.27.0/src/internal/runtime/gc/sizeclasses.go), and the [`fieldalignment`](https://pkg.go.dev/golang.org/x/tools/go/analysis/passes/fieldalignment) analyzer. The Go spec section on [size and alignment guarantees](https://go.dev/ref/spec#Size_and_alignment_guarantees) is short and worth a read.

This is the first module of Part III, where we stop looking only at the compiler and start looking at the memory underneath it. We start with something you control completely: the order of the fields in your structs. Go never reorders fields for you. Put them in the wrong order and the compiler adds invisible padding, the allocator rounds the result up again, and every loop over your data reads more memory than it needs to.

## Learning Objectives

By the end of this module, you will:

- Know Go's alignment rules and read them with `unsafe.Sizeof`, `unsafe.Alignof` and `unsafe.Offsetof`
- Understand where padding comes from and how ordering fields large→small removes it
- Know the trailing zero-size field gotcha
- Know how the allocator rounds every small object up to a **size class**
- Use the `fieldalignment` analyzer to find wasteful structs automatically
- Measure the effect of a smaller struct on allocations and on iteration speed

## The Concept: Padding, Alignment and Size Classes

### Alignment rules

Every type has an **alignment**: its address must be a multiple of that number. On amd64 and arm64, a value of size 1, 2, 4 or 8 bytes is aligned to its own size (`bool`, `uint16`, `uint32`, `int64`, `float64`, pointers…). A struct is aligned to the largest alignment among its fields, and an array to the alignment of its element.

The compiler places fields **in the order you declare them**, and before each field it inserts just enough padding to reach that field's alignment. Here is the core of `calcStructOffset` in `cmd/compile/internal/types/size.go`:

```go
for _, f := range fields {
    CalcSize(f.Type)
    offset = RoundUp(offset, int64(f.Type.align))
    ...
    f.Offset = offset
    offset += f.Type.width
```

Then `CalcStructSize` rounds the total up to the struct's alignment, so that in an array, every element is aligned too:

```go
size = RoundUp(size, int64(maxAlign))
```

So padding appears in two places: **before a field** that needs more alignment than the current offset gives, and **at the end** of the struct.

### Seeing it

Take the lab's particle. Its fields are in a "logical" order, with each flag next to the value it describes:

```go
type ParticleBefore struct {
    Active bool
    X      float64
    Kind   uint16
    Y      float64
    Alive  bool
    Z      float64
    ID     uint32
}
```

![Field order decides padding: ParticleBefore takes 56 bytes, the same fields sorted by alignment take 32](images/09-padding.webp)

The lab includes a small program that prints the layout using `reflect` (which reports the same offsets as `unsafe.Offsetof`):

```bash
cd labs
go run ./09-struct-layout/cmd/sizes
```

```
ParticleBefore: size=56 align=8 (heap size class 64)
    0  Active   bool     size=1 align=1
    1  padding  7 bytes
    8  X        float64  size=8 align=8
   16  Kind     uint16   size=2 align=2
   18  padding  6 bytes
   24  Y        float64  size=8 align=8
   32  Alive    bool     size=1 align=1
   33  padding  7 bytes
   40  Z        float64  size=8 align=8
   48  ID       uint32   size=4 align=4
   52  padding  4 bytes
```

The fields themselves add up to 1+8+2+8+1+8+4 = **32 bytes**. The struct is **56**: 24 bytes, 43% of it, are padding. Sort the fields from the largest alignment to the smallest and the padding disappears:

```
ParticleAfter: size=32 align=8 (heap size class 32)
    0  X        float64  size=8 align=8
    8  Y        float64  size=8 align=8
   16  Z        float64  size=8 align=8
   24  ID       uint32   size=4 align=4
   28  Kind     uint16   size=2 align=2
   30  Active   bool     size=1 align=1
   31  Alive    bool     size=1 align=1
```

> **Why doesn't Go just reorder the fields for me?** Because field order is observable: it affects `unsafe.Offsetof`, `reflect`, binary layouts shared with C via cgo or `syscall`, and the memory you get when you convert between struct types. The spec promises nothing about layout, but the gc compiler has always kept declaration order, and a lot of code depends on that.

### The trailing zero-size field gotcha

A zero-size field (`struct{}`, `[0]int`) is normally free. **Except at the end of a struct.** From `CalcStructSize`:

```go
// For non-zero-sized structs which end in a zero-sized field, we
// add an extra byte of padding to the type. This padding ensures
// that taking the address of a zero-sized field can't manufacture a
// pointer to the next object in the heap. See issue 9401.
if size > 0 && fields[len(fields)-1].Type.width == 0 {
    size++
}
```

One extra byte, then rounding to the struct's alignment, and an `int64` plus a marker field doubles in size. The `sizes` program shows it:

```
Trailing: size=16 align=8 (heap size class 16)
    0  N        int64    size=8 align=8
    8  End      struct {} size=0 align=1
    8  padding  8 bytes

Leading: size=8 align=8 (heap size class 8)
    0  Start    struct {} size=0 align=1
    0  N        int64    size=8 align=8
```

You'll meet this with `_ struct{}` "no copy" or "no compare" markers and with `[0]func()` fields. Put them first.

### Size classes: the allocator rounds up too

When you heap-allocate a small object, `mallocgc` doesn't hand out exactly `Sizeof` bytes. Objects up to 32 KB come from **size classes**: spans of memory cut into equal-size slots. The table is generated into `internal/runtime/gc/sizeclasses.go`:

```
// class  bytes/obj  bytes/span  objects  tail waste  max waste  min align
//     1          8        8192     1024           0     87.50%          8
//     2         16        8192      512           0     43.75%         16
//     3         24        8192      341           8     29.24%          8
//     4         32        8192      256           0     21.88%         32
//     5         48        8192      170          32     31.52%         16
//     6         64        8192      128           0     23.44%         64
//     7         80        8192      102          32     19.07%         16
```

The noscan path in `runtime/malloc.go` (`mallocgcSmallNoscan`) looks up the class and replaces the requested size with the class size:

```go
sizeclass = gc.SizeToSizeClass8[divRoundUp(size, gc.SmallSizeDiv)]
...
size = uintptr(gc.SizeClassToSize[sizeclass])
spc := makeSpanClass(sizeclass, true)
```

So a 33-byte object costs 48 bytes, and our 56-byte particle costs **64**. The reordered 32-byte particle lands exactly on a class. Sometimes saving a single byte moves you down a class. Sometimes saving 20 bytes changes nothing because you stay in the same class. `fieldalignment` reports both numbers.

> **New in Go 1.27: the lookup moves to compile time.** When the size is known at compile time and under 80 bytes, the compiler now calls a size-specialized routine from `runtime/malloc_generated.go` with the class baked in as a constant. Run `go build -gcflags=-S ./09-struct-layout/` and look at `SpawnBefore`: it calls `runtime.mallocgcSmallNoScanSC6` (class 6, 64 bytes); once you have reordered the fields (Step 3), `SpawnAfter` calls `runtime.mallocgcSmallNoScanSC4` (class 4, 32 bytes). The allocation is cheaper, but the rounding is exactly the same: the class is still chosen from the same table.

### A word on pointers and the GC

Did you notice `makeSpanClass(sizeclass, true)` above? Spans are split not only by size but also by **noscan**: whether the type contains pointers. A pointer-free object (like our particle: only numbers and bools) goes into a noscan span, and the GC never has to look inside it. Adding a single `string` or `*T` field changes that. We'll come back to this in [Helping the Garbage Collector](14-gc-pressure.md).

## The Lab

```
labs/09-struct-layout/
  before.go      ParticleBefore and its functions (baseline, don't edit)
  after.go       your copy: make ParticleAfter smaller
  solution.go    reference solution (built with -tags solution)
  lab_test.go    correctness test + benchmarks
  cmd/sizes/     prints the layout of both structs
```

There are two benchmarks, each measuring one consequence of the struct's size:

| Benchmark | What it does | What a smaller struct changes |
|---|---|---|
| `Spawn` | allocates 10,000 particles, one `&Particle{}` each | bytes per allocation (size class) |
| `SumActive` | sums X+Y+Z over a `[]Particle` of 1,000,000 elements | bytes the CPU reads from memory |

### Step 1: Measure the baseline

From the repository root:

```bash
make bench LAB=09
```

`after.go` starts as a copy of `before.go`, so both columns should be the same (`~`).

### Step 2: Find the waste

Print the layout and check the size:

```bash
cd labs
go run ./09-struct-layout/cmd/sizes
```

Then let a tool find it for you. `fieldalignment` lives in `golang.org/x/tools` and runs straight from the module cache:

```bash
go run golang.org/x/tools/go/analysis/passes/fieldalignment/cmd/fieldalignment@latest ./09-struct-layout/
```

```
.../09-struct-layout/after.go:14:20: ParticleAfter has size 56 (allocator size class 64) but the optimal size is 32 leading to a waste of 32 bytes (50%)
.../09-struct-layout/before.go:11:21: ParticleBefore has size 56 (allocator size class 64) but the optimal size is 32 leading to a waste of 32 bytes (50%)
```

Note that it compares the **size class** (64) against the optimal size (32): half of every allocation is wasted. It also has a `-fix` flag that rewrites the struct for you, but read the warning in its docs first: it drops comments on the fields, and it also optimizes for the GC's "pointer bytes", which may give an order you don't expect.

> `fieldalignment` is not part of `go vet` because a smaller struct isn't always better (see "Understanding the Results"). Treat it as a report, not a linter you must silence.

### Step 3: Fix it

Edit `labs/09-struct-layout/after.go` and reorder the fields of `ParticleAfter`: the three `float64` first, then `uint32`, then `uint16`, then the two `bool`s. Don't touch the functions: the struct literals use field names, so they still compile. Run `go run ./09-struct-layout/cmd/sizes` again and check you got 32.

### Step 4: Compare

```bash
make bench LAB=09
```

Here are the results with the reference solution (`make solution LAB=09 COUNT=8`) on an AMD Ryzen AI MAX+ 395:

```
             │    before     │                after                │
             │    sec/op     │    sec/op     vs base               │
Spawn-32        161.6µ ± 22%   111.4µ ± 13%  -31.08% (p=0.000 n=8)
SumActive-32   1166.8µ ±  2%   646.1µ ±  3%  -44.63% (p=0.000 n=8)

             │     before     │                 after                 │
             │      B/op      │     B/op      vs base                 │
Spawn-32       705.0Ki ± 0%     392.5Ki ± 0%  -44.33% (p=0.000 n=8)
SumActive-32     0.000 ± 0%       0.000 ± 0%        ~ (p=1.000 n=8)

             │    before     │                after                │
             │   allocs/op   │  allocs/op   vs base                │
Spawn-32       10.00k ± 0%     10.00k ± 0%       ~ (p=1.000 n=8)
```

## Understanding the Results

**Spawn** made exactly the same number of allocations, but each one shrank from the 64-byte class to the 32-byte class. The arithmetic is exact: 10,000 × 64 B + 81,920 B for the `[]*Particle` slice = 705.0 KiB, and 10,000 × 32 B + 81,920 B = 392.5 KiB. The time improvement is smaller and noisier (allocating a small noscan object is cheap either way, and even cheaper since Go 1.27's size-specialized allocation), but half the bytes also means half the heap growth, so the GC runs half as often. That's where the long-term win is.

**SumActive** is the more surprising one: the code didn't change at all, it's **45% faster**, and it allocates nothing. The loop is limited by how fast memory can feed it, not by the additions. Before, the slice was 56 MB; after, 32 MB. Both are bigger than this machine's 32 MB L3 cache, so most of the data streams from RAM, and the time is roughly proportional to the bytes read: 56 → 32 MB is a 43% reduction, and we measured 45%. With 64-byte cache lines, a line used to hold 1.14 particles and now holds exactly 2, so particles never straddle two lines. You'll learn exactly why in the next module, [CPU Caches and Data Locality](10-cpu-caches.md).

> **Smaller isn't always better.** Reordering can separate fields that are used together, and with concurrent writers, packing two hot fields into the same cache line can make things *slower* (false sharing, see [Contention and False Sharing](15-contention-false-sharing.md)). Order fields for size by default, and for access patterns when a profile tells you to.

## Key Takeaways

- Go keeps fields in declaration order and pads each one to its alignment
- Ordering fields from the largest alignment to the smallest removes most padding
- A zero-size field **at the end** of a struct adds padding: put markers first
- Heap objects are rounded up to a **size class**: 56 bytes cost 64, 33 bytes cost 48
- `unsafe.Sizeof`, `cmd/sizes`-style layout dumps and `fieldalignment` show you the waste
- A smaller element means more elements per cache line, so loops over big slices get faster for free
- Pointer-free structs land in noscan spans that the GC never scans

## Going Further (optional)

1. Add a `Name string` field to `ParticleAfter`. Where should it go? What happens to the size and to the size class? Is the type still noscan?
2. Change `sumN` in `lab_test.go` to `10_000` so the whole slice fits in L2 (1 MB). How does the `SumActive` speedup change? Why?
3. Add `_ struct{}` as the last field of `ParticleAfter` and run `cmd/sizes`. Then move it to the top.
4. Run `fieldalignment` on a project you work on. How many structs does it report, and are any of them allocated millions of times?
