# Goroutines, Channels and the Scheduler

> **Want to learn more?** On Internals for Interns, [The Scheduler](https://internals-for-interns.com/posts/go-runtime-scheduler/) covers the GMP model and how a goroutine finds work, and [Slices, Maps, and Channels](https://internals-for-interns.com/posts/go-runtime-slices-maps-channels/#channels) shows what a channel is inside. The [scheduler chapter](https://www.youtube.com/watch?v=arH3jp_x8yQ&t=1852s) of Jesús Espino's GopherCon UK talk *Understanding the Go Runtime* gives the overview.

Goroutines and channels make concurrent work easier to organize, but starting workers and sending messages still takes time. When each task is tiny, that coordination can cost more than the task itself. We will group work into chunks and compare it with a plain loop.

## Learning Objectives

By the end of this module, you will:

- Understand why concurrency can be slower for small tasks
- Use an execution trace to find waiting goroutines
- Send batches instead of individual items
- Split work between a bounded number of workers

## The Concept: What Coordination Costs

### Starting and scheduling work

`go f()` asks the runtime to arrange a goroutine, give it stack space and schedule it. Go reuses resources where possible, but these steps still have a cost. Creating one goroutine for every tiny calculation can waste more time than it saves.

You can measure the cost of starting many blocked goroutines with:

```bash
(cd labs && go run ./13-goroutines-channels/cmd/gocost)
```

### Sending through a channel

A send may copy a value into a buffer or pass it to a waiting receiver. If the operation cannot proceed, the goroutine waits and must be scheduled again later. Concurrent channel use also requires synchronization.

![Every channel send takes the channel lock and may wake a goroutine; batching pays that cost once per chunk instead of once per item](images/13-channel.webp)

A buffered channel can reduce waiting, but it doesn't remove the cost of sending each item. Sending a slice of 1,024 items shares that coordination cost across the whole batch. A sub-slice points to the original data; it doesn't copy its elements.

### When one goroutine per task makes sense

A goroutine per request or connection is often a good design: each task does meaningful work or waits for I/O. For small CPU calculations, start with a plain loop, then try a fixed number of workers over chunks. Measure both.

## The Lab

You are running a 10% sale. Each input is a product price in cents, and the calculation is `cents - cents/10` (rounding the discount down). Both exercises process 100,000 prices. `TotalPricesBefore` adds up the discounted prices, sending one item at a time through an unbuffered channel. `DiscountPricesBefore` produces the discounted price list with one goroutine per item. The `TotalPrices` benchmark also includes `impl=sequential`, the plain-loop baseline. Compare `ns/item`, the time per item.

Run the commands from the repository root.

### Step 1: Measure the baseline

```bash
make bench LAB=13
```

### Step 2: Find the cost

```bash
(cd labs && go test -run '^$' -bench 'TotalPrices/impl=before' -benchtime 20x -trace trace.out ./13-goroutines-channels/)
```

From `labs/`, open `go tool trace trace.out`. Look for workers repeatedly waiting on the jobs channel. The producer spends so much time handing out individual items that the workers cannot stay busy.

### Step 3: Fix it

Edit `labs/13-goroutines-channels/after.go`. Keep `before.go` as the baseline.

1. In `TotalPricesAfter`, change `jobs` to `chan []uint64` and send chunks of up to 1,024 elements: `items[i:min(i+1024, len(items))]`. Give the channel a small buffer, such as `runtime.GOMAXPROCS(0)`.
2. Have each worker process all items in its chunk before receiving another. Keep the existing way of combining partial sums.
3. In `DiscountPricesAfter`, start `runtime.GOMAXPROCS(0)` workers. Give each a separate contiguous range of indexes in `out`, then wait for all workers. Read the input without modifying it.

### Step 4: Check and compare

```bash
(cd labs && go test ./13-goroutines-channels/)
make bench LAB=13
```

Reference solution (`make solution LAB=13`), on my machine:

| Benchmark | Before | After | Sequential |
|---|---|---|---|
| `TotalPrices` time per item | about 180 ns | about 0.84 ns | about 0.38 ns |
| `DiscountPrices` time per item | about 265 ns | about 0.92 ns | — |
| `DiscountPrices` allocations per run | about 200,000 | 66 | — |

## Understanding the Results

The original concurrent versions are slower than the plain loop because coordination dominates the small calculation. Adding workers doesn't fix that bottleneck.

Batching makes `TotalPrices` much faster than the original channel-per-price version. Giving `DiscountPrices` a bounded set of workers avoids almost all per-item goroutine allocations. The output slice still needs memory.

The plain loop still wins for these tiny discount calculations. That is a useful result: batching fixes a coordination problem, but it does not mean the job needs concurrency in the first place.

The exact speedup depends on available cores and chunk size. A very large chunk can leave some workers idle; a very small one brings back the coordination overhead.

## Key Takeaways

- Compare concurrent code with a sequential baseline.
- Batch small tasks so each channel operation carries useful work.
- Use a bounded number of workers for CPU-heavy batches.
- Keep output ranges separate and wait before reading the result.

## Going Further (optional)

1. Try chunk sizes of 16, 64, 256 and 4096. Which works best?
2. Add a buffer to the original single-item channel. How much does it help compared with batching?
3. Run with `CPU=1,4,8` and compare the scaling of the implementations.
