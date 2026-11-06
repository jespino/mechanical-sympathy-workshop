# Interfaces and Boxing

Interfaces make code flexible, but they can add allocations and hide optimization opportunities from the compiler. We will explore both with two small exercises using product prices.

## Content You May Want to Check First

- [Go Empty Interface Explained & The Hidden Heap Allocation](https://www.youtube.com/watch?v=_BxdvkLLnDc) (video by Phuong Le): what an `eface` holds, and when storing a value in one sends it to the heap through `convT`.
- [The Reflect Package](https://internals-for-interns.com/posts/go-runtime-reflect/#inside-an-interface) (article, Internals for Interns): the layout of an interface value and the type data behind it.
- [The IR](https://internals-for-interns.com/posts/the-go-ir/#devirtualization-making-the-indirect-direct) (article, Internals for Interns): how the compiler turns an interface call into a direct call.

## Learning Objectives

By the end of this module, you will:

- Understand boxing and when it can allocate
- Recognize the cost of interface calls in a hot loop
- Compare interface and concrete method calls
- Avoid unnecessary boxing when storing a list of prices

## The Concept: What an Interface Holds

### What an interface holds

`any` can hold values of different types. For example:

```go
price := 1000
var value any = price
```

Go remembers two things: **the type** (`int`) and **where to find the value** (`1000`).

For a value such as this number, Go can store a separate copy and make the interface point to it. This is called **boxing**. The copy may need extra memory on the heap, but the compiler can avoid that allocation in some cases. Using `any` does not always allocate.

![An any value remembers the type int and points to a stored copy of the price 1000; boxing may need extra heap memory](images/08-iface.webp)

### Calling a method through an interface

`any` has no required methods. Other interfaces describe what a value can **do**. Here, `PriceFilter` accepts any type with a `Keep` method, and `*Budget` is one such type:

```go
type PriceFilter interface {
    Keep(price int) bool
}

type Budget struct { MaxCents int }

func (b *Budget) Keep(price int) bool {
    return price <= b.MaxCents
}
```

We can put a budget into the interface and call its method:

```go
var filter PriceFilter = &Budget{MaxCents: 1500}
affordable := filter.Keep(1000) // true
```

Go uses the concrete type stored in `filter` to find the right `Keep` method. In this case it calls `Budget`'s method with the stored budget.

### Why method calls matter

When a function accepts `PriceFilter`, it could receive different filter types. The compiler may not know which `Keep` method will run, so it cannot inline that call.

If the function accepts `*Budget` instead, the compiler knows the method and can inline its comparison. Recognizing a concrete implementation behind an interface is called **devirtualization**.

## The Lab

Both exercises use product prices in cents. First we will look at **allocations**, then **execution time**. Edit `labs/08-interfaces-boxing/after.go` and run commands from the repository root.

### Exercise 1: Store prices without boxing

`CopyPricesAfter` copies 1,000 prices into `[]any`. But every price is an `int`, so the extra flexibility is unnecessary.

**Measure:**

```bash
make bench LAB=08 BENCH=CopyPrices
```

**Change:** replace `[]any` with `[]int` in the return type and in `make`. Keep the loop unchanged:

```go
func CopyPricesAfter(prices []int) []int {
    out := make([]int, len(prices))
    for i, price := range prices {
        out[i] = price
    }
    return out
}
```

**Check and compare:**

```bash
(cd labs && go test ./08-interfaces-boxing/)
make bench LAB=08 BENCH=CopyPrices
```

With the reference solution, on my machine:

| Allocations per copy | Before | After |
|---|---|---|
| 1,000 prices | 1,001 | 1 |

The remaining allocation is the output slice. We removed the separate boxed copy of each price. The benchmark uses prices above 255; Go can use shared storage for smaller boxed integers.

### Exercise 2: Make the budget check direct

`CountAffordableAfter` counts prices that fit a budget. Its loop calls `budget.Keep(price)` for each of 10,000 prices.

**Measure:**

```bash
make bench LAB=08 BENCH=CountAffordable
```

Both versions allocate nothing. Watch **`sec/op`** this time.

**Change:** replace the parameter type `PriceFilter` with `*Budget`. Keep the body unchanged:

```go
func CountAffordableAfter(prices []int, budget *Budget) int
```

Keep the supplied `//go:noinline` on this outer function. The comparison we want to inline is `Budget.Keep` **inside** its loop.

**Check and compare:**

```bash
(cd labs && go test ./08-interfaces-boxing/)
make bench LAB=08 BENCH=CountAffordable
```

On my machine, the reference solution takes about **2.8 µs**, compared with **8–12 µs** for the interface version. Both still report zero allocations.

## Understanding the Results

The first change avoids boxing because the slice now stores integers directly. The second lets the compiler inline a tiny method because its type is known. Neither changes which prices are copied or counted.

Interfaces remain useful when you need different implementations. Use a concrete type where the extra flexibility is unnecessary and measurements show a benefit.

## Key Takeaways

- If all values are integers, `[]int` can avoid boxing into `[]any`.
- A known method implementation can be inlined.
- Measure allocations and execution time separately.

## Going Further (optional)

1. Change the test prices to values below 256. What happens to boxing allocations?
2. Change the budget limit. Does the number of affordable products stay the same in both versions?
