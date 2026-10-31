# sync.Pool

A temporary buffer can be cheap to use but expensive to create on every request. `sync.Pool` lets goroutines reuse temporary objects across calls. We will pool a logging buffer and measure the allocations and GC work it avoids.

## Learning Objectives

By the end of this module, you will:

- Use `Get`, `Put` and `New` to reuse temporary objects
- Understand that pooled objects may disappear at any time
- Reset objects and avoid retaining oversized buffers
- Measure pooling in sequential and parallel benchmarks

## The Concept: How `sync.Pool` Works

### Get, use, return

`Get` takes an available object, or calls `New` to create one. After using it, `Put` makes it available to another call. A pool is safe to share between goroutines, but each borrowed object should have one owner at a time.

The runtime keeps local storage for its Go execution resources, called **Ps**. This helps most pool operations avoid competing over one shared queue.

![sync.Pool keeps a private slot and a shared queue per P, and other Ps can steal from the shared queues](images/11-pool.webp)

### Objects are not kept forever

The garbage collector can remove pooled objects. The current implementation keeps a previous round of objects in a **victim cache**, but that is an implementation detail, not a promise that an object will survive.

![A pooled object survives one GC in the victim cache and is dropped at the second](images/11-victim.webp)

You can observe the current behavior with:

```bash
(cd labs && go run ./11-sync-pool/cmd/victim)
```

Always make `New` able to recreate an object. Use pools for temporary buffers, not resources such as open files or connections.

### Rules for safe reuse

- Store pointers, such as `*bytes.Buffer`. Passing a slice value through `any` can itself allocate.
- Reset the object before reuse so old data doesn't enter the next result.
- Return buffers only up to a reasonable size; one large request shouldn't keep a huge buffer around.
- After `Put`, stop using the object and any slices that refer to its storage.

## The Lab

`WriteEventBefore` builds a JSON log line in a new `bytes.Buffer` on every call. `WriteEvent` measures one goroutine; `WriteEventParallel` measures concurrent calls. The extra `GCs/Mop` column reports GC cycles per million operations.

Run the commands from the repository root.

### Step 1: Measure the baseline

```bash
make bench LAB=11
```

### Step 2: Find the cost

```bash
(cd labs && go test -run '^$' -bench 'WriteEventParallel/impl=before' -memprofile mem.prof ./11-sync-pool/ && go tool pprof -sample_index=alloc_space -top mem.prof)
```

Look for `bytes.growSlice` and buffer growth. The baseline makes seven allocations per call, mostly to build the temporary line.

### Step 3: Fix it

Edit `labs/11-sync-pool/after.go`. Keep `before.go` as the baseline.

1. Import `sync` and declare a package-level `sync.Pool` whose `New` returns `new(bytes.Buffer)`.
2. In `WriteEventAfter`, get the buffer with `pool.Get().(*bytes.Buffer)`, reset it, encode the event and write it.
3. Return it to the pool after the write, including error paths, unless `buf.Cap()` exceeds `64*1024`. Do not use its bytes after returning it.

### Step 4: Check and compare

```bash
(cd labs && go test ./11-sync-pool/)
make bench LAB=11
```

Reference solution (`make solution LAB=11`), on my machine:

| Benchmark | Before | After |
|---|---|---|
| `WriteEvent` | 879.1 ns | 563.4 ns |
| `WriteEventParallel` | 274.60 ns | 48.19 ns |
| Allocations per call | 7 | 0 |

## Understanding the Results

The pool avoids repeatedly creating and growing buffers. The benchmark reaches zero allocations per call after warm-up, with less GC work. This doesn't guarantee zero allocations forever: the pool can lose objects and create replacements.

The parallel benchmark improves most because many goroutines stop allocating at once. The remaining work is mostly formatting and writing the event.

It is safe to return the buffer after `w.Write` completes because an `io.Writer` must not retain the supplied bytes. If an object already stays on the stack, pooling it is usually unnecessary.

## Key Takeaways

- Pool temporary objects that profiles show are expensive to allocate repeatedly.
- Store pointers and reset objects before reuse.
- Objects can disappear from the pool at any time.
- Limit retained buffer sizes and stop using objects after `Put`.

## Going Further (optional)

1. Run `make bench LAB=11 CPU=1,2,4,8`. How does pooling change parallel performance?
2. Add an occasional large event. Compare memory use with and without the size limit.
3. Run `cmd/victim` and observe what happens after forced GC cycles.
