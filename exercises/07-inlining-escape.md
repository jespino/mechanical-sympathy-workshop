# Inlining × Escape Analysis

Inlining can do more than remove a function call. It can also help the compiler keep an object on the stack, even when a constructor returns a pointer. This module combines the ideas from the previous two modules.

## Content You May Want to Check First

- [The IR](https://internals-for-interns.com/posts/the-go-ir/#the-optimization-pipeline) (article, Internals for Interns): the order of the optimization passes, which is why inlining can change what escapes.

## Learning Objectives

By the end of this module, you will:

- Explain why inlining can avoid a heap allocation
- Split a constructor into a small wrapper and an initialization method
- Check the result with compiler output and `allocs/op`

## The Concept: Allocations Move With Inlining

### Why the order matters

The compiler does **inlining before escape analysis**. If it copies a constructor into its caller, it can then see how that caller uses the new object.

If the caller only uses the object locally, it may stay on the caller's stack. If the caller stores it somewhere that lives longer, it still needs the heap. Returning a pointer doesn't tell the whole story: the caller matters too.

### Keep the constructor small

Move the validation and setup into an `init` method, leaving a small constructor:

```go
func NewCustomer(input Signup) (*Customer, error) {
    c := &Customer{}
    if err := c.init(input); err != nil {
        return nil, err
    }
    return c, nil
}
```

The constructor can be inlined even if `init` is too large. What matters is that `init` doesn't store `c` somewhere that outlives the call. Adding more work to the constructor can push it over the inlining budget.

## The Lab

You are processing customer signup forms. Each form has a name and email. `NewCustomerBefore` trims spaces, checks that the name is present and that the email has one `@` with text on both sides, then returns a `*CustomerBefore`.

`CountValidBefore` counts valid forms. It only needs each customer during the loop, but the constructor is too large to inline, so 1,000 valid forms cause 1,000 heap allocations.

Run the commands from the repository root.

### Step 1: Measure the baseline

```bash
make bench LAB=07
```

### Step 2: Find the cost

```bash
(cd labs && go build -gcflags=-m=2 ./07-inlining-escape/ 2>&1 | grep -E 'inline NewCustomerBefore|CustomerBefore.*escapes')
```

Look for `cannot inline NewCustomerBefore` and `escapes to heap`. The constructor's cost is 261, above the usual budget of 80.

### Step 3: Fix it

Edit `labs/07-inlining-escape/after.go`. Keep `before.go` as the baseline.

1. Move the work in `NewCustomerAfter` into `func (c *CustomerAfter) init(input Signup) error`.
2. In `init`, return errors directly. On success, fill `*c` instead of returning a new customer, then return `nil`.
3. Use the small constructor above, with `CustomerAfter` as the type. Leave `CountValidAfter` unchanged.

### Step 4: Check and compare

```bash
(cd labs && go test ./07-inlining-escape/)
make bench LAB=07
```

Reference solution (`make solution LAB=07`), on my machine:

| Benchmark | Before | After |
|---|---|---|
| `CountValid` time | about 17 µs | about 7.8 µs |
| Allocations per run | 1,000 | 0 |

## Understanding the Results

All 1,000 customer allocations disappear, and this benchmark is about twice as fast. The name and email checks stay the same; the temporary customer now lives on the caller's stack.

Use `-gcflags=-m=2` again and look for `can inline NewCustomerAfter` and `does not escape` at the call in `CountValidAfter`. The standalone constructor may still report `escapes to heap`: the inlined copy and the standalone function are analyzed separately.

This only works while the wrapper fits the budget and the caller keeps the customer local. A pointer stored for later use can still escape.

## Key Takeaways

- Inlining lets escape analysis see how the caller uses an object.
- Keep constructors small; move setup into a method that does not retain the receiver.
- Returning a pointer can still avoid the heap when the call is inlined.
- Confirm with compiler output and `allocs/op`.

## Going Further (optional)

1. Add a validation check to the small constructor. Does it still inline?
2. Store the customers in a slice that is kept after `CountValidAfter` returns. Do allocations come back?
