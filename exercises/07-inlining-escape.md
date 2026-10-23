# Inlining × Escape Analysis

> **Want to learn more?** Look at how [`crypto/internal/fips140/aes.New`](https://github.com/golang/go/blob/go1.27.0/src/crypto/internal/fips140/aes/aes.go) and the `Bytes` methods in [`crypto/internal/fips140/nistec`](https://github.com/golang/go/blob/go1.27.0/src/crypto/internal/fips140/nistec/p384.go) are split, and read the order of the passes in [`cmd/compile/internal/gc/main.go`](https://github.com/golang/go/blob/go1.27.0/src/cmd/compile/internal/gc/main.go): inlining runs **before** escape analysis.

The last two modules each showed one compiler pass on its own. This one is about how they work together, and it's one of the most useful tricks in performance-sensitive Go. In short: **escape analysis looks at a function after inlining**. When a constructor is inlined, the allocation it contains moves into the caller, and the caller may be able to keep it on the stack. We'll use that to make a "`New` returns a pointer" API allocation-free.

## Learning Objectives

By the end of this module, you will:

- Understand why the order "inline, then escape analysis" matters
- Know why `func NewX() *X` usually allocates, and when it doesn't
- Apply the **outlined constructor** pattern: a tiny inlinable `New` that allocates, plus a separate `init` that does the work
- Read `-m` output where the same allocation "escapes" in one place and "does not escape" in another
- Find this pattern in the standard library

## The Concept: Allocations Move With Inlining

The compiler's middle end runs, in order: devirtualization and **inlining** (interleaved), then **escape analysis**, then the backend. By the time escape analysis looks at a function, every call that was inlined has been replaced by the callee's body.

Consider a constructor:

```go
func NewDecoder(block []byte) (*Decoder, error) {
	// ... validate the header, check a CRC ...
	return &Decoder{src: block[headerSize:], left: count, t: first}, nil
}
```

Analyzed on its own, `&Decoder{...}` flows into the result `~r0`, so it **escapes**: the function returns and its frame is gone. If `NewDecoder` is not inlined, that's final. Every call allocates, no matter what the caller does with the pointer.

But if `NewDecoder` **is** inlined into this caller:

```go
for _, block := range blocks {
	d, err := NewDecoder(block)
	...
	for { t, ok := d.Next(); ... }
}
```

...then the `&Decoder{...}` is now an expression inside the caller's loop. Its "result" is just the local variable `d`. If `d` is only used for method calls whose parameter tags say "`d` does not escape" (module 05), the `Decoder` can live on the caller's stack. Zero allocations, with the exact same API.

So for a constructor that returns a pointer, there are two requirements:

1. The constructor must be **inlinable** (cost ≤ 80, see [Inlining](06-inlining.md)).
2. The **caller** must not let the pointer escape (no storing it in the heap, returning it, or passing it to an interface method).

The second one is up to the users of your API. The first one is up to you.

### The outlined constructor pattern

Real constructors validate, compute, and call other functions, so they rarely fit in 80. The fix is to split them, exactly like the fast path / slow path split from module 06, but this time the "fast path" is the allocation:

```go
// Tiny and inlinable: the allocation happens in the caller.
func NewDecoder(block []byte) (*Decoder, error) {
	d := &Decoder{}
	if err := d.init(block); err != nil {
		return nil, err
	}
	return d, nil
}

// All the work. It only writes through d, so "d does not escape".
func (d *Decoder) init(block []byte) error { ... }
```

![When a small constructor is inlined, its allocation moves into the caller's frame and can stay on the stack](images/07-outlined.webp)

`init` is not inlined (it's too big), but it doesn't need to be: what matters is that its parameter tag says `d does not escape`, so passing a pointer to a stack object is fine.

This is not a made-up trick. The Go standard library does it, and says so in comments:

```go
// crypto/internal/fips140/aes/aes.go
func New(key []byte) (*Block, error) {
	// This call is outline to let the allocation happen on the parent stack.
	return newOutlined(&Block{}, key)
}

// newOutlined is marked go:noinline to avoid it inlining into New, and making New
// too complex to inline itself.
//
//go:noinline
func newOutlined(b *Block, key []byte) (*Block, error) {
```

```go
// crypto/internal/fips140/nistec/p384.go
func (p *P384Point) Bytes() []byte {
	// This function is outlined to make the allocations inline in the caller
	// rather than happen on the heap.
	var out [1 + 2*p384ElementLength]byte
	return p.bytes(&out)
}
```

And `crypto/internal/fips140/sha256.New` is just `d := new(Digest); d.Reset(); return d`. The second example works for returned slices too: the fixed-size array is allocated in the caller (after inlining), and the outlined function only fills it.

Note the `//go:noinline` on `newOutlined`. Without it, the compiler might inline `newOutlined` into `New` (if it were small enough), and `New` would become too expensive to be inlined anywhere. Inlining is decided bottom-up, so a helper that gets inlined into your wrapper adds its cost to the wrapper.

## The Lab

```
labs/07-inlining-escape/
  before.go      naive implementation (baseline, don't edit)
  after.go       your copy: make this one fast
  solution.go    reference solution (built with -tags solution)
  lab_test.go    correctness test + benchmarks
```

We go back to the timestamp blocks of module 06, now with a proper header: magic bytes, version, count, the first timestamp, and a CRC-32 of the data. `NewDecoderBefore(block)` validates all of it and returns a `*DecoderBefore`, which has a `Next()` iterator. `MaxGapBefore` scans 1,000 small blocks (16 timestamps each) to find the largest gap between two samples, so it creates 1,000 decoders.

### Step 1: Measure the baseline

```bash
make bench LAB=07
```

You'll see **1,000 allocs/op**, one per block, and 78 KiB/op: the `Decoder` is 72 bytes, rounded up to the 80-byte size class.

### Step 2: Ask the compiler

```bash
cd labs
go build -gcflags=-m=2 ./07-inlining-escape/ 2>&1 | grep 'inline NewDecoderBefore' | cut -c1-110
go build -gcflags=-m ./07-inlining-escape/ 2>&1 | grep 'before.go' | grep -E ': .*(escape|leak)'
```

```
07-inlining-escape/before.go:44:6: cannot inline NewDecoderBefore: function too complex: cost 138 exceeds budg
07-inlining-escape/before.go:44:23: leaking param: block
07-inlining-escape/before.go:48:17: string(block[:2]) does not escape
07-inlining-escape/before.go:59:9: &DecoderBefore{...} escapes to heap
07-inlining-escape/before.go:64:7: d does not escape
07-inlining-escape/before.go:85:7: leaking param: d to result ~r0 level=1
07-inlining-escape/before.go:89:19: leaking param content: blocks
```

The constructor costs 138, so it isn't inlined, and `&DecoderBefore{...}` escapes through its return value. Look at `MaxGapBefore` though: the decoder is only used locally, and `(*DecoderBefore).Next` reports `d does not escape`. If the allocation were in `MaxGapBefore`, it could stay on the stack.

### Step 3: Fix it

Edit `labs/07-inlining-escape/after.go`:

1. Move the body of `NewDecoderAfter` into a method `func (d *DecoderAfter) init(block []byte) error`. Replace `return nil, ErrX` with `return ErrX`, and the final `return &DecoderAfter{...}, nil` with `*d = DecoderAfter{...}` and `return nil`.
2. Make `NewDecoderAfter` the tiny wrapper shown above.
3. Don't touch `MaxGapAfter`. The point is that callers don't change.

Check:

```bash
go build -gcflags=-m=2 ./07-inlining-escape/ 2>&1 | grep -E 'inline (NewDecoderAfter|\(\*DecoderAfter\).init)' | cut -c1-110
go build -gcflags=-m ./07-inlining-escape/ 2>&1 | grep -E 'DecoderAfter\{\}'
```

With the reference solution:

```
07-inlining-escape/solution.go:32:6: cannot inline (*DecoderAfter).init: function too complex: cost 136 exceed
07-inlining-escape/solution.go:22:6: can inline NewDecoderAfter with cost 80 as: func([]byte) (*DecoderAfter,
07-inlining-escape/solution.go:23:7: &DecoderAfter{} escapes to heap
07-inlining-escape/solution.go:80:28: &DecoderAfter{} does not escape
```

The same `&DecoderAfter{}` appears **twice** with opposite verdicts, and both are right:

- Line 23 is the standalone `NewDecoderAfter` function. It still exists (someone could call it through a function value, or from a package where it isn't inlined), and in that copy the decoder does escape.
- Line 80 is the copy inlined into `MaxGapAfter`. There it does not escape, so it's a stack allocation.

Also note the cost: **80**, right at the limit. Writing it as `d := new(DecoderAfter); err := d.init(block); return d, err` costs 72, but returns a non-nil decoder on error. Pick the semantics you want, and check the cost.

### Step 4: Compare

```bash
make bench LAB=07
```

Here are the results with the reference solution (`make solution LAB=07`) on an AMD Ryzen AI MAX+ 395:

```
          │   before    │               after                │
          │   sec/op    │   sec/op     vs base               │
MaxGap-32   65.09µ ± 1%   51.29µ ± 3%  -21.20% (p=0.000 n=8)

          │    before    │                after                │
          │     B/op     │    B/op      vs base                │
MaxGap-32   78.12Ki ± 0%   0.00Ki ± 0%  -100.00% (p=0.000 n=8)

          │   before    │                after                │
          │  allocs/op  │  allocs/op   vs base                │
MaxGap-32   1.000k ± 0%   0.000k ± 0%  -100.00% (p=0.000 n=8)
```

## Understanding the Results

All 1,000 allocations are gone, and the time drops by about a fifth, even though each block still has its header parsed and its CRC computed. On a quieter run (pinned to one core with `taskset`) we measured 94 µs → 49 µs. The "before" number varies more from run to run because it includes allocation and GC work, and those depend on what else the machine is doing.

The API didn't change: `NewDecoderAfter` still returns `(*DecoderAfter, error)`. Callers that keep the decoder around (store it in a struct, return it) still get a heap allocation, which is correct. Callers that use it locally, which is the common case for iterators, readers, hashers and builders, get it for free.

### When it doesn't work

- **The constructor returns an interface.** `crypto/sha256.New()` returns a `hash.Hash`. In a purely local `h := sha256.New(); h.Write(p); h.Sum(buf[:0])` the compiler still sees the concrete type after inlining, and the devirtualizer (improved in Go 1.26, unchanged in 1.27) turns the calls into direct ones: `-m` says `devirtualizing h.Write to *sha256.Digest` and `new(sha256.Digest) does not escape`, 0 allocations. But pass `h` to any non-inlined function that takes a `hash.Hash` and the calls are dynamic again, escape analysis can't follow them (see the [next module](08-interfaces-boxing.md)), and the `Digest` goes to the heap. The inner `fips140/sha256.New()` returns `*Digest` and doesn't depend on that.
- **The caller lets it escape**, for example by storing the decoder in a long-lived struct or sending it on a channel.
- **The value is too big** for the stack (64 KiB for `&T{}`, see module 05).
- **The wrapper is over budget.** It's easy to push a "tiny" wrapper over 80 by adding one more check. Protect it with a test that uses `testing.AllocsPerRun`:

```go
func TestNewDecoderDoesNotAllocate(t *testing.T) {
	block := makeBlock(...)
	if n := testing.AllocsPerRun(100, func() { MaxGapAfter([][]byte{block}) }); n != 0 {
		t.Fatalf("got %v allocs, want 0", n)
	}
}
```

## Key Takeaways

- Inlining runs **before** escape analysis, so escape decisions are made on the inlined code
- A non-inlined constructor that returns a pointer **always** allocates. An inlined one allocates in the caller, where it may stay on the stack
- **Outlined constructor**: keep `New` tiny (allocate + call `init`), do the work in `init(d *T)` which doesn't let `d` escape
- The standard library does this in `crypto/internal/fips140` (`aes.New`/`newOutlined`, `nistec` `Bytes`/`bytes`, `sha256.New`)
- The same allocation can be reported as escaping in the function and not escaping at an inlined call site. Both are true
- Returning interfaces defeats it as soon as the compiler can't devirtualize the calls. Test the allocation count if you depend on it

## Going Further (optional)

1. Add one more validation to your `NewDecoderAfter` wrapper (for example, `if len(block) == 0 { return nil, ErrHeader }`) instead of in `init`. Does it still inline? What does the benchmark say?
2. Make `MaxGapAfter` store every decoder in a slice (`var all []*DecoderAfter; all = append(all, d)`). What does `-m` say about `&DecoderAfter{}` now?
3. Write the same benchmark for `crypto/sha256`: compare `sha256.New()` + `Write` + `Sum(buf[:0])` used locally, the same but with the `Write` done in a `//go:noinline` helper that takes a `hash.Hash`, and `sha256.Sum256(data)`. Count the allocations and explain the difference (look for `devirtualizing` in `-m`).
