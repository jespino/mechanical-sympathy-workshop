# sync.Pool

> **Want to learn more?** Read [`sync/pool.go`](https://github.com/golang/go/blob/go1.27.0/src/sync/pool.go) and the lock-free deque behind it in [`sync/poolqueue.go`](https://github.com/golang/go/blob/go1.27.0/src/sync/poolqueue.go). Then look at two real users: `pp.free` in [`fmt/print.go`](https://github.com/golang/go/blob/go1.27.0/src/fmt/print.go) and `getBufferedEncoder`/`putBufferedEncoder` in [`encoding/json/jsontext/pools.go`](https://github.com/golang/go/blob/go1.27.0/src/encoding/json/jsontext/pools.go) (since Go 1.27, `encoding/json` is backed by the v2 implementation, which pools its encoders there).

In [Pre-allocation](03-preallocation.md) we removed allocations by asking for the right size once. That works when an object lives inside one function call. But a lot of hot code needs a *temporary* object on every call: a buffer to format a log line, an encoder state, a scratch slice. Each call allocates it, uses it for a microsecond, and drops it on the floor for the GC. `sync.Pool` lets you hand that object back and reuse it on the next call. In this module we look at how the pool is built, why it's so fast, why it forgets things at every GC, and how to use it without shooting yourself in the foot.

## Learning Objectives

By the end of this module, you will:

- Know how `sync.Pool` stores objects per P, and why `Get`/`Put` are almost always lock-free
- Understand the victim cache: why pooled objects survive exactly one GC, and not more
- Know why you should pool pointers, not values (staticcheck's SA6002)
- Avoid the classic pitfalls: forgetting to reset, keeping huge buffers forever, mixing sizes
- Measure the effect of pooling on allocs/op, on the number of GC cycles and on parallel throughput

## The Concept: How `sync.Pool` Works

### One small cache per P

Remember the scheduler's P: the "processor" a goroutine must hold to run Go code, one per `GOMAXPROCS`. A `Pool` keeps one `poolLocal` per P (`sync/pool.go`):

```go
type Pool struct {
	noCopy noCopy

	local     unsafe.Pointer // local fixed-size per-P pool, actual type is [P]poolLocal
	localSize uintptr        // size of the local array

	victim     unsafe.Pointer // local from previous cycle
	victimSize uintptr        // size of victims array

	New func() any
}

type poolLocalInternal struct {
	private any       // Can be used only by the respective P.
	shared  poolChain // Local P can pushHead/popHead; any P can popTail.
}

type poolLocal struct {
	poolLocalInternal

	// Prevents false sharing on widespread platforms with
	// 128 mod (cache line size) = 0 .
	pad [128 - unsafe.Sizeof(poolLocalInternal{})%128]byte
}
```

![sync.Pool keeps a private slot and a shared queue per P, and other Ps can steal from the shared queues](images/11-pool.webp)

Note the padding: each P's slot is pushed onto its own cache lines so that two cores using the pool never fight over the same line. We'll study exactly why that matters in [Contention and False Sharing](16-contention-false-sharing.md).

Here is `Put` (race detector code removed):

```go
l, _ := p.pin()
if l.private == nil {
	l.private = x
} else {
	l.shared.pushHead(x)
}
runtime_procUnpin()
```

`pin()` calls `runtime_procPin`, which disables preemption and returns the current P's id. While pinned, the goroutine can't be moved to another P, so nobody else can touch `l.private`: storing into it needs **no lock and no atomic**. That's the fast path. If `private` is taken, the object goes to `shared`, a `poolChain`: a linked list of lock-free ring buffers (`poolqueue.go`) where only the owning P pushes and pops at the head, and other Ps may only pop at the tail.

`Get` mirrors it: take `private`, else pop the head of the local `shared` chain, else `getSlow`. The slow path **steals** from the tail of other Ps' chains, and only if all of them are empty, looks in the victim cache. If all of that fails, `Get` calls `New`.

So in steady state, when every goroutine Gets and Puts on the same P, the pool costs a pin, a pointer load and a pointer store. No contention between cores at all.

### The victim cache: two GCs to die

Pooled objects are not kept forever. At the start of every GC cycle, while the world is stopped, the runtime calls `clearpools()` (`runtime/mgc.go`), which calls `poolCleanup`:

```go
// Drop victim caches from all pools.
for _, p := range oldPools {
	p.victim = nil
	p.victimSize = 0
}

// Move primary cache to victim cache.
for _, p := range allPools {
	p.victim = p.local
	p.victimSize = p.localSize
	p.local = nil
	p.localSize = 0
}
```

![A pooled object survives one GC in the victim cache and is dropped at the second](images/11-victim.webp)

An object Put in the pool survives the next GC in the *victim* cache, and is dropped at the GC after that. Before Go 1.13 there was no victim cache and every GC emptied every pool, which caused allocation spikes right after each collection. The demo calls `runtime.GOMAXPROCS(1)`: in the victim cache, `getSlow` only checks the `private` slot of the *current* P, so if the goroutine moved to another P during `runtime.GC()` it would miss the object. Run the demo:

```bash
cd labs
go run ./11-sync-pool/cmd/victim
```

```
Put, 0 x runtime.GC(), Get -> found=true
Put, 1 x runtime.GC(), Get -> found=true
Put, 2 x runtime.GC(), Get -> found=false
Put, 3 x runtime.GC(), Get -> found=false
```

That is the contract: a `Pool` is a **cache**, not a free list. You can never rely on getting back what you put, and you shouldn't use it for objects that are expensive to *lose* (connections, file handles). It's for objects that are cheap to recreate but annoying to allocate millions of times per second.

### Pool pointers, not values

`Put` takes an `any`. Converting a value to an interface needs a pointer to the data, so a non-pointer value is boxed (copied to the heap) on every `Put`. Putting a `[]byte` (a 3-word slice header) allocates 24 bytes each time, which is the allocation you were trying to avoid. A quick `testing.AllocsPerRun` on Go 1.27:

```
p.Put(b)   // b is a []byte    -> 1 alloc per Put/Get
p.Put(pb)  // pb is a *[]byte  -> 0 allocs
```

Staticcheck reports this as **SA6002**. Store `*bytes.Buffer`, `*[]byte`, `*myStruct`.

### Pitfalls

1. **Reset what you get.** The object comes back with whatever the last user left in it. `encoding/json` resets every encoder it takes from its pool (`e.s.reset(...)` in `getBufferedEncoder`). Forgetting this leaks data between requests.
2. **Don't keep huge objects.** If one request in a million formats a 50 MB document, its buffer grows to 50 MB and goes back to the pool. Now one P holds 50 MB until two GCs pass, and if big requests keep coming, forever. `fmt` guards against this in `pp.free`:

   ```go
   // Proper usage of a sync.Pool requires each entry to have approximately
   // the same memory cost. [...]
   // See https://golang.org/issue/23199.
   if cap(p.buf) > 64*1024 {
   	p.buf = nil
   } else {
   	p.buf = p.buf[:0]
   }
   ```

   `encoding/json` used to have *no* such guard: up to Go 1.26, `Marshal` always did `defer encodeStatePool.Put(e)`, whatever size the buffer grew to. In Go 1.27 it is backed by the v2 implementation, whose `putBufferedEncoder` always recycles buffers up to 4 KB, and drops a bigger one after it's been less than 25% used five times in a row (see https://go.dev/issue/27735).
3. **Don't mix sizes.** A pool of "buffers of any size" hands a 64-byte buffer to someone who needs 1 MB (which then grows it, allocating anyway) and a 1 MB buffer to someone who needs 64 bytes (wasting memory). If sizes vary a lot, use one pool per size class (powers of two, for example).
4. **Never use an object after Put.** Another goroutine may already own it. Things like `return buf.Bytes()` after `Put(buf)` are data races waiting to happen.

## The Lab

```
labs/11-sync-pool/
  before.go      naive WriteEvent (baseline, don't edit)
  after.go       your copy: make this one fast
  solution.go    reference solution (built with -tags solution)
  lab_test.go    correctness test + benchmarks
  cmd/victim/    the victim-cache demo above
```

`WriteEventBefore(w, e)` formats a structured log event as a JSON line and writes it to an `io.Writer`. It builds the line in a fresh `bytes.Buffer` on every call. The line is about 260 bytes, so the buffer grows several times along the way. There are two benchmarks: `WriteEvent` calls it from one goroutine, and `WriteEventParallel` calls it from `GOMAXPROCS` goroutines with `b.RunParallel`, like a busy server would. Both report an extra column, `GCs/Mop`: GC cycles per million calls.

### Step 1: Measure the baseline

```bash
make bench LAB=11
```

Both columns should be the same for now. Note the 7 allocations and ~1 KB per call.

### Step 2: Find the allocations

```bash
cd labs
go test -run '^$' -bench 'WriteEventParallel/impl=before' -memprofile mem.prof ./11-sync-pool/
go tool pprof -sample_index=alloc_space -top mem.prof
```

```
      flat  flat%   sum%        cum   cum%
 3859.91MB 83.22% 83.22%  3859.91MB 83.22%  bytes.growSlice
  479.02MB 10.33% 93.55%   479.02MB 10.33%  strconv.appendQuotedWith
  285.02MB  6.15% 99.70%  4144.93MB 89.37%  bytes.(*Buffer).grow
```

The buffer growing (64 → 128 → 256 → 512 bytes) is almost everything. The rest is `strconv.AppendQuote` growing `AvailableBuffer()` when it's too small for the value. A CPU profile (`-cpuprofile`) of the same benchmark shows `runtime.mallocgc` at about a third of the time (since Go 1.27, small allocations go to size-specialized variants, so you'll also see names like `runtime.mallocgcSmallNoScanSC6` under it), plus `gcBgMarkWorker` and `gcAssistAlloc` for the collections all that garbage triggers.

### Step 3: Fix it

Edit `labs/11-sync-pool/after.go`:

1. Declare a package-level `sync.Pool` whose `New` returns `new(bytes.Buffer)`.
2. In `WriteEventAfter`: `Get` a buffer, `Reset` it, encode, write.
3. `Put` it back, **unless** `buf.Cap()` is above 64 KB.

Why is it safe to reuse the buffer after `w.Write(buf.Bytes())`? Because the `io.Writer` contract says: *"Write must not modify the slice data, even temporarily. Implementations must not retain p."* The writer is done with our bytes when `Write` returns.

### Step 4: Compare

```bash
make bench LAB=11
```

Results with the reference solution (`make solution LAB=11`) on an AMD Ryzen AI MAX+ 395 (16 cores, 32 threads, shared with other workloads while measuring):

```
                      │    before    │                after                │
                      │    sec/op    │    sec/op     vs base               │
WriteEvent-32            879.1n ± 2%   563.4n ±  1%  -35.92% (p=0.000 n=8)
WriteEventParallel-32   274.60n ± 6%   48.19n ± 10%  -82.45% (p=0.000 n=8)

                      │   before    │                after                 │
                      │   GCs/Mop   │ GCs/Mop   vs base                    │
WriteEvent-32           342.8 ±  1%   0.0 ± 0%  -100.00% (p=0.000 n=8)
WriteEventParallel-32   256.6 ± 17%   0.0 ± 0%  -100.00% (p=0.000 n=8)

                      │    before    │                  after                   │
                      │     B/op     │     B/op      vs base                    │
WriteEvent-32           1.047Ki ± 0%   0.000Ki ± 0%  -100.00% (p=0.000 n=8)
WriteEventParallel-32   1.047Ki ± 0%   0.000Ki ± 0%  -100.00% (p=0.000 n=8)

                      │   before   │                 after                  │
                      │ allocs/op  │ allocs/op   vs base                    │
WriteEvent-32           7.000 ± 0%   0.000 ± 0%  -100.00% (p=0.000 n=8)
WriteEventParallel-32   7.000 ± 0%   0.000 ± 0%  -100.00% (p=0.000 n=8)
```

## Understanding the Results

**Zero allocations, zero GCs.** The before version triggers a GC every ~3,000 calls. The pooled version reuses the same handful of buffers forever, so the benchmark doesn't produce garbage at all and the GC never runs.

**Single goroutine: -36%.** The work that remains is the formatting itself. What we removed is seven calls to `mallocgc`, the copies done by `growSlice`, and the GC work (which, as in module 03, mostly runs on *other* cores and doesn't fully show up in this goroutine's time).

**Parallel: -82%.** This is where the per-P design shines. With 32 goroutines allocating ~1 KB per call, the allocator and the GC are shared resources: more GC cycles, mark assists stealing time from our goroutines, and more cache misses on freshly allocated memory. The pool's fast path touches only the current P's `private` slot, so it scales with cores. How big the parallel gain is depends a lot on your core count and on what else is running. Try `-cpu 1,4,16` to see it change.

> **When not to pool.** If the object is small and doesn't escape, the compiler already puts it on the stack ([Escape Analysis](05-escape-analysis.md)), and that's cheaper than any pool. If allocations aren't showing up in your profiles, a pool only adds complexity and a new class of bugs (use-after-Put). Pool what the profile tells you to pool.

## Key Takeaways

- `sync.Pool` keeps a `private` slot and a lock-free `shared` deque **per P**: in steady state `Get`/`Put` touch no shared memory
- When the local P is empty, `Get` steals from other Ps, then checks the victim cache, then calls `New`
- Every GC moves the pools to the victim cache and drops the old victims: objects survive one GC, not two
- It's a cache, not a free list: never depend on getting an object back
- Pool pointers (`*bytes.Buffer`, `*[]byte`), not values: boxing a value allocates on every `Put`
- Always reset, cap the size of what you put back (like `fmt` does with 64 KB), and never touch an object after `Put`

## Going Further (optional)

1. Remove the 64 KB check from your solution and add a benchmark where 1 in 1,000 events has a 1 MB `Message`. Use `GODEBUG=gctrace=1` or `runtime.ReadMemStats` to see how much heap stays in use.
2. Run `WriteEventParallel` with `-cpu 1,2,4,8,16`. How do the before and after curves differ?
3. Replace the pool with a `chan *bytes.Buffer` of capacity 64 used as a free list. Benchmark it in parallel. Why is it slower than `sync.Pool`? (Hint: [Goroutines, Channels and the Scheduler](13-goroutines-channels.md).)
