# Struct Layout and Size Classes

> **Want to learn more?** [The Memory Allocator](https://internals-for-interns.com/posts/go-memory-allocator/) on Internals for Interns explains arenas, spans and size classes in detail.

The order of fields in a struct can change how much memory it uses. Go may insert unused bytes between fields to keep them aligned. We will reorder a struct and measure how that affects both allocations and loops over its data.

## Learning Objectives

By the end of this module, you will:

- Recognize padding between struct fields
- Measure struct size and field offsets
- Understand why heap allocations round up to size classes
- Reduce memory use by reordering fields

## The Concept: Padding, Alignment and Size Classes

### Alignment and padding

Some values must start at an address that is a multiple of their alignment. For example, an `int64` usually needs 8-byte alignment. If a `bool` comes just before it, Go may insert seven unused bytes to reach the next suitable address. These unused bytes are **padding**.

Go keeps fields in the order you declare them. Grouping fields with larger alignment first often reduces padding.

The lab's order has 32 bytes of actual fields but takes 56 bytes in its original order. Reordering those fields reduces it to 32 bytes without removing any data.

### Size classes

The heap allocator uses fixed-size slots called **size classes**. A single 56-byte order takes a 64-byte slot; a 32-byte order fits in a 32-byte slot. Reducing the struct size can therefore save even more heap space.

![Reordering the same order fields removes 24 bytes of padding: the struct shrinks from 56 to 32 bytes and its individual heap slot shrinks from 64 to 32 bytes](images/10-padding.webp)

The shaded areas are padding. The fields themselves still hold exactly the same data.

### Slots inside a span

Go manages memory for small objects in larger blocks called **spans**. Each span is divided into equal-size slots and serves one size class.

In this example, both spans are 8 KiB: one holds **256 slots of 32 bytes**, and the other holds **128 slots of 64 bytes**. Smaller slots let the same amount of memory hold more objects.

![Two 8 KiB spans hold 256 slots of 32 bytes or 128 slots of 64 bytes; a 56-byte order uses one 64-byte slot with 8 bytes left unused](images/10-spans.webp)

A 56-byte order uses one 64-byte slot, leaving 8 bytes unused. This rounding is separate from padding inside the struct, which is already included in those 56 bytes.

A slice of structs is different: its elements are packed using the struct size. A million 56-byte orders need about 56 MB, while 32-byte orders need about 32 MB.

### A small layout surprise

Even a zero-size field such as `struct{}` can add padding if it is the last field of a non-empty struct. Put marker fields first and check the resulting size.

`unsafe.Sizeof`, `unsafe.Alignof` and `unsafe.Offsetof` report sizes and positions. The lab's `cmd/sizes` program prints them for you.

## The Lab

`OrderBefore` stores an order ID, item count, payment and delivery flags, and three amounts in cents. `CreateOrders` allocates 10,000 orders. `TotalPaid` adds subtotal plus shipping minus discount for paid orders in a million-element slice. We will change only the field order of `OrderAfter`.

Run the commands from the repository root.

### Step 1: Measure the baseline

```bash
make bench LAB=10
```

### Step 2: Find the cost

```bash
(cd labs && go run ./10-struct-layout/cmd/sizes)
```

Look for `OrderBefore: size=56` and the gaps between its fields. Compare the total field sizes with the struct size.

### Step 3: Fix it

Edit `labs/10-struct-layout/after.go`. Keep `before.go` as the baseline.

1. Put the three `int64` fields first, followed by `uint32`, `uint16` and the two `bool` fields, as shown below.
2. Keep the functions unchanged; their struct literals use field names.
3. Run `cmd/sizes` again and check that `OrderAfter` takes 32 bytes.

```go
type OrderAfter struct {
    SubtotalCents int64
    ShippingCents int64
    DiscountCents int64
    ID            uint32
    Items         uint16
    Paid          bool
    Delivered     bool
}
```

### Step 4: Check and compare

```bash
(cd labs && go test ./10-struct-layout/)
make bench LAB=10
```

Reference solution (`make solution LAB=10`), on my machine:

| Benchmark | Before | After |
|---|---|---|
| `CreateOrders` time | about 202 µs | about 131 µs |
| `CreateOrders` bytes per run | 705.0 KiB | 392.5 KiB |
| `TotalPaid` time | about 1.27 ms | about 0.68 ms |

## Understanding the Results

**CreateOrders** still makes 10,000 order allocations, but each is smaller. Watch `B/op`: fewer bytes matter even when `allocs/op` stays the same.

**TotalPaid** takes about half the time without changing the loop. The CPU reads less memory for the same number of orders. We will explore that in [CPU Caches](16-cpu-caches.md).

A smaller struct is not always faster. Fields used together should usually stay close, while fields written by different goroutines can interfere with each other. Measure the access pattern as well as the size.

## Key Takeaways

- Field order affects padding and struct size.
- Group fields with larger alignment first, then check the layout.
- Heap size classes can amplify the memory saving.
- Smaller records can make large scans faster.

## Going Further (optional)

1. Add a `CustomerName string` field. Where can you place it to minimize padding?
2. Put `_ struct{}` at the end, then at the beginning. Compare the sizes.
3. Reduce `sumN` to `10_000`. Does the smaller data set change the speedup?
