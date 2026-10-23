# Goroutines, Channels and the Scheduler

> **Want to learn more?** Read `newproc1`, `gfget` and `gfput` in [`runtime/proc.go`](https://github.com/golang/go/blob/go1.27.0/src/runtime/proc.go), `startingStackSize` in [`runtime/stack.go`](https://github.com/golang/go/blob/go1.27.0/src/runtime/stack.go), and `chansend`/`chanrecv` in [`runtime/chan.go`](https://github.com/golang/go/blob/go1.27.0/src/runtime/chan.go). The [`go tool trace` documentation](https://pkg.go.dev/cmd/trace) explains the trace viewer.

Goroutines and channels are Go's best-known feature, and they're cheap: much cheaper than OS threads and locks. But "cheap" is not "free". A goroutine costs hundreds of nanoseconds to start, and a channel operation tens to hundreds of nanoseconds, depending on contention. If the work you hand to each goroutine, or send through each channel operation, is just a few nanoseconds, the coordination costs more than the work. In this module we measure those costs, look at them in an execution trace, and fix two very common patterns by **batching**: sending chunks instead of items, and splitting work into ranges instead of spawning one goroutine per item.

## Learning Objectives

By the end of this module, you will:

- Know what `go f()` does in the runtime: `newproc`, the per-P free list of `g`s, initial stacks and `runnext`
- Know what a channel send/receive does: the channel lock, direct hand-off to a waiting receiver, `gopark`/`goready`
- Measure the per-item overhead of a channel pipeline and of goroutine-per-item code
- Capture and read an execution trace from a benchmark with `go tool trace`
- Batch work so that coordination cost is amortized over many items

## The Concept: What Coordination Costs

### Starting a goroutine

`go f()` compiles to a call to `runtime.newproc`, which runs `newproc1` on the system stack:

```go
mp := acquirem() // disable preemption because we hold M and P in local vars.
pp := mp.p.ptr()
newg := gfget(pp)
if newg == nil {
	newg = malg(stackMin)
	casgstatus(newg, _Gidle, _Gdead)
	allgadd(newg) // publishes with a g->status of Gdead so GC scanner doesn't look at uninitialized stack.
}
```

1. **Reuse a dead `g`.** When a goroutine exits, `gfput` puts its `g` struct (and, if it's the standard size, its stack) on the P's `gFree` list. With 64 or more entries there, it moves a batch to a global list. `gfget` takes one back. So most `go` statements in a busy program allocate *no* new `g`.
2. **Otherwise allocate one** with a stack of `stackMin = 2048` bytes, and register it in `allgs`, the list of all goroutines. That list never shrinks.
3. **Set up the frame** so that the goroutine starts at `fn` and "returns" into `goexit`.
4. **`runqput(pp, newg, true)`**: the new goroutine goes into the P's `runnext` slot, so it runs next on this P, and `wakep()` may wake an idle P to steal work.

**Stacks start at 2 KB, but not always.** Since Go 1.19, `GODEBUG=adaptivestackstart=1` is the default (`runtime1.go`). At each GC, `gcComputeStartingStackSize` sets `startingStackSize` to the average stack size scanned, so if your goroutines usually grow to 8 KB, new ones start at 8 KB and skip the copying. Stacks that are too small still grow by copying (`morestack` → `newstack`), and stacks that are too big get shrunk by the GC.

The lab has a demo that starts 100,000 goroutines that block on a channel:

```bash
cd labs
go run ./16-goroutines-channels/cmd/gocost
```

```
started 100000 goroutines in 86ms (861 ns each)
goroutine stacks:   2058 bytes per goroutine
total from OS:      2889 bytes per goroutine
```

About **0.9 µs and 3 KB** per *new* goroutine on this machine: the 2 KB stack, plus the `g` struct and runtime bookkeeping. That's tiny compared to an OS thread (8 MB of reserved stack, microseconds for a syscall to create it), but it's still almost 1,000 nanoseconds. A reused `g` from the free list is much cheaper, as we'll see in the lab.

### A channel operation

A channel is an `hchan` (`runtime/chan.go`): a ring buffer, two wait queues of `sudog`s (one per blocked sender and receiver), and a lock.

![Every channel send takes the channel lock and may wake a goroutine; batching pays that cost once per chunk instead of once per item](images/16-channel.webp)

```go
type hchan struct {
	qcount   uint           // total data in the queue
	dataqsiz uint           // size of the circular queue
	buf      unsafe.Pointer // points to an array of dataqsiz elements
	...
	recvq    waitq  // list of recv waiters
	sendq    waitq  // list of send waiters
	...
	lock mutex
}
```

`chansend` does, in order:

1. **`lock(&c.lock)`**. Every operation on the channel, from any core, takes this lock. A channel shared by 32 goroutines is a contended lock with a cache line bouncing between cores, just like in [Contention and False Sharing](15-contention-false-sharing.md).
2. **If a receiver is waiting** (`c.recvq.dequeue()`), copy the value **directly onto its stack** (`sendDirect`), skipping the buffer, and `goready` it: put it on a run queue.
3. **Else, if the buffer has room**, copy the value into the buffer and return.
4. **Else, block**: get a `sudog`, queue it on `sendq`, and `gopark`. The goroutine is descheduled until a receiver comes, and that receiver will `goready` us.

So an unbuffered send to a waiting worker is: lock, copy, unlock, and wake a goroutine (a run queue push, maybe waking a P and an OS thread). Then the worker needs to be scheduled before it can process the item, and when it's done it parks again in `chanrecv`. Each step is fast, but there are many steps, and they all touch shared state. Buffered channels skip the park/wake when the buffer is neither full nor empty, but they still take the lock on every operation.

### Seeing it: `go tool trace`

The execution tracer records every goroutine creation, block, unblock, syscall and GC, with timestamps. You can capture a trace from any benchmark:

```bash
go test -run '^$' -bench 'Sum/impl=before' -benchtime 20x -trace trace.out ./16-goroutines-channels/
go tool trace trace.out
```

The second command opens a web UI (on a random localhost port; to pick one, use `-http=:6060`, which since Go 1.27 also listens only on localhost: write `-http=0.0.0.0:6060` to expose it). In **View trace by proc** you'll see each P switching between goroutines every few hundred nanoseconds, like a barcode, instead of long solid bars of work. The same data is also available as pprof profiles:

```bash
go tool trace -pprof=sync trace.out > sync.prof    # time blocked on channels/mutexes
go tool trace -pprof=sched trace.out > sched.prof  # time runnable but waiting for a P
go tool pprof -top sync.prof
```

## The Lab

```
labs/16-goroutines-channels/
  before.go      SumBefore and MapBefore (baseline, don't edit)
  after.go       your copy: make them fast
  solution.go    reference solution (built with -tags solution)
  lab_test.go    correctness test + benchmarks
  cmd/gocost/    the goroutine cost demo above
```

`work(x)` is a small integer mixer that takes about 11 ns per item: the size of many real per-item tasks. Both functions process 100,000 items with `GOMAXPROCS` worth of parallelism:

- **`SumBefore`** is a classic worker pool: `GOMAXPROCS` workers read items from an **unbuffered `chan uint64`**, one item per send, and each sends its partial sum back at the end.
- **`MapBefore`** computes `out[i] = work(items[i])` with **one goroutine per item** (`wg.Go`).

The `Sum` benchmark also has an `impl=sequential` column: a plain loop on one goroutine, the number to beat. Both benchmarks report `ns/item`.

### Step 1: Measure the baseline

```bash
make bench LAB=16
```

Look at the `sequential` column first. Then look at `before`. Using 32 threads is **slower** than using one.

### Step 2: Look at the trace

```bash
cd labs
go test -run '^$' -bench 'Sum/impl=before' -benchtime 20x -trace trace.out ./16-goroutines-channels/
go tool trace -pprof=sync trace.out > sync.prof
go tool pprof -top sync.prof
```

```
      flat  flat%   sum%        cum   cum%
 6459.97ms 71.52% 71.52%  6459.97ms 71.52%  runtime.chanrecv2
 2407.41ms 26.65% 98.17%  2407.41ms 26.65%  runtime.chanrecv1
  164.97ms  1.83%   100%   164.97ms  1.83%  runtime.chansend1
         0     0%   100%  6459.97ms 71.52%  labs/16-goroutines-channels.SumBefore.func1
```

The workers (`SumBefore.func1`) spend most of their time blocked in `chanrecv2` (the `for x := range jobs` receive) **waiting for work**. The single producer can't push items fast enough: each send costs more than the ~11 ns of work it hands out. Then open `go tool trace trace.out` and look at the procs: short slivers of work separated by scheduling.

### Step 3: Fix it

Edit `labs/16-goroutines-channels/after.go`:

1. **`SumAfter`**: make `jobs` a `chan []uint64` and send chunks: `jobs <- items[i:min(i+1024, len(items))]`. A sub-slice is only a new slice header pointing into the same array. Nothing is copied. Workers loop over each chunk. A small buffer (`GOMAXPROCS`) on `jobs` lets the producer run ahead.
2. **`MapAfter`**: start `GOMAXPROCS` goroutines, not `len(items)`. Give each one a contiguous block `[start, end)` of indices. Each goroutine writes its own part of `out`, so the only synchronization needed is the final `Wait`.

### Step 4: Compare

```bash
make bench LAB=16
```

Results with the reference solution (`make solution LAB=16`) on an AMD Ryzen AI MAX+ 395 (16 cores, 32 threads, shared with other workloads while measuring):

```
        │     before     │               after                │              sequential               │
        │    sec/item    │  sec/item    vs base               │   sec/item    vs base                 │
Sum-32    147.450n ± 13%   2.143n ± 3%  -98.55% (p=0.000 n=8)   11.645n ± 3%  -92.10% (p=0.000 n=8)
Map-32    283.250n ±  7%   2.320n ± 9%  -99.18% (p=0.000 n=8)

        │     before      │                after                │
        │      B/op       │     B/op      vs base               │
Map-32    7815.3Ki ±   0%   787.3Ki ± 0%  -89.93% (p=0.000 n=8)

        │    before     │               after               │
        │   allocs/op   │ allocs/op   vs base               │
Map-32    200002.00 ± 0%   66.00 ± 0%  -99.97% (p=0.000 n=8)
```

(`benchstat` shows our `ns/item` metric as `sec/item`. The `sec/op` table is the same data multiplied by 100,000, and the B/op and allocs/op for `Sum` are a few goroutines' worth of noise, so they're left out.)

## Understanding the Results

**The channel pipeline was 13x slower than a plain loop.** 147 ns per item with 32 threads versus 11.6 ns with one. Every item paid for a lock on a shared channel, a copy, and usually a park/wake pair, all serialized through one producer and one lock. Adding workers didn't help, because the bottleneck was the channel, not the work.

**Sending 1,024 items per operation made it 69x faster**: 2.1 ns per item, **5.4x faster than sequential**. The channel overhead is the same per operation, but now it's divided by 1,024. Each worker spends its time in a tight loop over contiguous memory, which is what CPUs are good at.

**Goroutine-per-item cost 280 ns per item and 2 allocations**: the closure passed to `wg.Go`, and the closure `wg.Go` itself passes to `go`. That's about 80 bytes per item, ~8 MB of garbage per call for the GC. It's cheaper than the 0.9 µs of `cmd/gocost` because finished goroutines go back to the `gFree` list and are reused right away, but it's still 25x the cost of the work. With one goroutine per P, each owning a range, it's 2.3 ns per item and 66 allocations per call (the 787 KB left is the `out` slice itself).

Why not 32x faster than sequential with 32 threads? With 100,000 items × 11 ns, the whole job is only ~1.1 ms of work, so starting the workers and waiting for the slowest one matters. There are also only 16 physical cores (SMT threads share execution units), and other workloads were running on the machine. With more cores you'll see a bigger gap between before and after, since the before versions get *worse* with more contention.

> **Rule of thumb:** make each unit of coordination (a goroutine, a channel operation, a lock acquisition) carry at least **microseconds** of work. If a task is nanoseconds, batch tasks until it isn't. And measure against the sequential version: parallelism that is slower than one core is more common than you'd think.

### When goroutine-per-thing *is* right

Goroutine-per-connection or per-request is idiomatic, and correct: each one does milliseconds of work, mostly waiting on I/O, and the runtime parks it in the netpoller for free while it waits. Batching matters when the work per goroutine or per message is small and CPU-bound. For bounded parallelism over a big input, a fixed number of workers over chunks (or `errgroup` with `SetLimit`) is the pattern. And when goroutines pile up blocked forever, Go 1.27's **goroutine leak profile** (`goroutineleak` in `runtime/pprof`, `/debug/pprof/goroutineleak` in `net/http/pprof`) reports the ones the GC can prove will never be woken up.

## Key Takeaways

- `go f()` → `newproc`: reuses a dead `g` from the per-P free list when it can, puts the new goroutine in `runnext`
- New goroutines start with a 2 KB stack, adapted at each GC to the average stack size (`adaptivestackstart`, on by default). Here a new blocked goroutine cost ~0.9 µs and ~3 KB
- Every channel operation takes the channel's lock. Sending to a waiting receiver copies straight to its stack and `goready`s it, and blocking means `gopark`
- Per-item channel sends and goroutine-per-item cost 100–300 ns per item, so with small tasks they're slower than a plain loop
- Batch: send `[]T` chunks (sub-slices cost nothing), give each worker a contiguous range
- `go test -trace` + `go tool trace` (and `-pprof=sync|sched`) shows where goroutines block and wait

## Going Further (optional)

1. Change `chunkSize` to 16, 64, 256, 4096 and 65536. Plot `ns/item`. Where does it stop improving, and why does a very big chunk get worse again?
2. Make `jobs` in `SumBefore` buffered (`make(chan uint64, 1024)`) in a copy. How much does it help? Why doesn't it fix the problem?
3. Run the `Map` benchmark with `-cpu 1,4,16`. How does goroutine-per-item scale?
4. Run `cmd/gocost` with `GODEBUG=adaptivestackstart=0` after making the goroutines use a deep recursive function before blocking. What changes?
