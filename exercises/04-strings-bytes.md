# Strings and Bytes

Strings are everywhere: log lines, HTTP headers, map keys, JSON, metric names. In the previous module we saw how growing a slice or a map hides allocations. Strings have one more trick up their sleeve: **they can never change**. That makes them safe and cheap to share, but it also means that every conversion between `string` and `[]byte` may have to copy. In this module we look at when that copy happens, when the compiler removes it for you, and how to avoid the one case that catches almost everybody.

## Learning Objectives

By the end of this module, you will:

- Know why strings can't change, and why that makes `string` ↔ `[]byte` conversions copy
- Know which conversions are free and which ones allocate, measured, not guessed
- Avoid the allocation hidden in `m[string(b)]++`

## The Concept: Immutable Bytes

### Two headers

A string is a small header with two fields: a pointer to the bytes and a length. A slice has the same two plus a third one, the capacity:

```go
type stringStruct struct {   type slice struct {
    str unsafe.Pointer           array unsafe.Pointer
    len int                      len   int
}                                cap   int
                             }
```

![A string header has a pointer and a length; a slice header adds a capacity. Converting between them copies the bytes](images/04-headers.webp)

The important difference is not the extra field. It's that **the bytes behind a string can never change**. That's what makes strings safe to share: taking a substring (`s[3:10]`) or assigning a string to another variable only copies the header, never the bytes, and a string can be a map key or be passed between goroutines without locks.

The flip side: a `[]byte` *can* change. So converting between them has to copy the bytes. Otherwise, someone could modify the bytes of a "string" that others are holding.

### When the conversion is free

The copy is only needed if someone could see the bytes change. When the compiler can prove nobody can, it skips the copy and just reuses the bytes that are already there. That happens when the converted value is only used for a moment and then thrown away:

- **Map lookups**: `m[string(b)]`
- **Comparisons**: `string(b) == "..."`
- **`switch string(b)`**
- **`for i, c := range []byte(s)`**
- **`[]byte(s)` that is only read, never modified**

The copy is still needed when the result is kept (stored in a variable that outlives the function, returned, or put in a map as a new key) or when the bytes are modified.

Let's check it. The lab includes a small program that benchmarks each case:

```bash
cd labs
go run ./04-strings-bytes/cmd/conversions
```

```
case                          ns/op     B/op  allocs/op
string(b), result kept        14.16       48          1
m[string(b)] lookup            8.11        0          0
m[string(b)]++                31.39       48          1
string(b) == "..."             4.18        0          0
switch string(b)               4.52        0          0
for range []byte(s)            8.46        0          0
[]byte(s), read only           1.89        0          0
[]byte(s), modified           13.11       48          1
```

(On my machine.) Look at the two map rows: **`m[string(b)]` is free, but `m[string(b)]++` is not.** `++` writes to the map, and if the key is new, the map has to store it. So the compiler must create a real string every time, even when the key is already there. That's the trap we fix in the lab.

### Building strings

`s += x` in a loop has the same problem as `append` on a slice that starts empty, only worse: since a string can't grow in place, every `+=` creates a new string and copies everything built so far. The fix is the same idea as in [Pre-allocation](03-preallocation.md): use a `strings.Builder` (or a `bytes.Buffer`) and, if you know the final size, call `Grow(n)` first.

## The Lab

```
labs/04-strings-bytes/
  before.go           CountTokensBefore (baseline, don't edit)
  after.go            your copy: make this one fast
  solution.go         reference solution (built with -tags solution)
  lab_test.go         correctness test + benchmark
  cmd/conversions/    the conversion table above
```

`CountTokensBefore` counts how many times each token appears. The tokens are `[]byte` slices, as they would be if they came from a file or a network read:

```go
func CountTokensBefore(tokens [][]byte) map[string]int {
	counts := map[string]int{}
	for _, t := range tokens {
		counts[string(t)]++
	}
	return counts
}
```

The benchmark runs it on 10,000 tokens that repeat only 17 different words.

### Step 1: Measure the baseline

```bash
make bench LAB=04
```

Look at `allocs/op`: about 10,000, one per token, even though there are only 17 different words.

### Step 2: Find the evidence

Ask the memory profiler where the allocations come from. `-memprofilerate=1` records every allocation instead of sampling, and `-list` shows them line by line:

```bash
cd labs
go test -run '^$' -bench 'CountTokens/impl=before' -memprofile mem.prof -memprofilerate 1 ./04-strings-bytes/
go tool pprof -sample_index=alloc_objects -list 'CountTokensBefore' mem.prof
```

```
         .          .      9:func CountTokensBefore(tokens [][]byte) map[string]int {
       681        681     10:	counts := map[string]int{}
         .          .     11:	for _, t := range tokens {
   2171031    2171031     12:		counts[string(t)]++
         .          .     13:	}
```

Almost every allocation comes from `counts[string(t)]++`: a new string for every token, used once to update the map, and then thrown away.

### Step 3: Fix it

Edit `labs/04-strings-bytes/after.go` so that updating an existing token is a pure lookup, which is free. Use a `map[string]*int`: if `counters[string(t)]` returns a pointer, increment through it. Only insert a new key (and allocate its string) the first time a token is seen. Convert to a `map[string]int` at the end.

`go test ./04-strings-bytes/` checks that the counts are the same.

### Step 4: Compare

```bash
make bench LAB=04
```

Results with the reference solution (`make solution LAB=04`) on my machine:

```
               │    before    │               after                │
               │    sec/op    │   sec/op     vs base               │
CountTokens-32   233.52µ ± 6%   70.70µ ± 5%  -69.73% (p=0.002 n=6)

               │    before     │                after                │
               │     B/op      │     B/op      vs base               │
CountTokens-32   51.320Ki ± 0%   2.578Ki ± 0%  -94.98% (p=0.002 n=6)

               │    before     │               after               │
               │   allocs/op   │ allocs/op   vs base               │
CountTokens-32   10007.00 ± 0%   43.00 ± 0%  -99.57% (p=0.002 n=6)
```

## Understanding the Results

10,000 tokens, but only 17 different words. The before version created a string key for every token. The after version creates 17 keys and 17 counters, and the remaining 9 allocations are the internal storage of the two maps. That's 3.3x faster with 99.6% fewer allocations.

The extra pass that converts `map[string]*int` to `map[string]int` costs less than what we saved. If you control the API, return the `map[string]*int` (or a slice of counts) and skip that pass too.

> **Rule of thumb:** a conversion only costs something when its result is *kept* or *modified*. Lookups, comparisons and reads are free. Storing and modifying are not.

## Key Takeaways

- Strings are immutable; conversions to and from `[]byte` may copy.
- The compiler can skip copies for temporary, read-only uses.
- `m[string(b)]++` allocates. Look up first; update through a pointer.
- Build strings with `strings.Builder` and `Grow` instead of repeated `+=`.

## Going Further (optional)

1. Add your own cases to `cmd/conversions`. Is `string(b)` free when you pass it to `strings.HasPrefix`? And `[]byte(s)` when you pass it to `bytes.IndexByte`?
2. Change `testTokens` in `lab_test.go` so that every token is different. How much does the after version save now, and why?
