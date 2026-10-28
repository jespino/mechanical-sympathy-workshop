# Profile-Guided Optimization

> **Want to learn more?** Read the official [PGO guide](https://go.dev/doc/pgo), the hot-callsite logic in [`cmd/compile/internal/inline/inl.go`](https://github.com/golang/go/blob/go1.27.0/src/cmd/compile/internal/inline/inl.go) (look for `inlineHotMaxBudget` and `inlineCostOK`), and PGO devirtualization in [`cmd/compile/internal/devirtualize/pgo.go`](https://github.com/golang/go/blob/go1.27.0/src/cmd/compile/internal/devirtualize/pgo.go).

The compiler makes its decisions without knowing which code is hot. It has to inline a little everywhere, and it can't know which concrete type sits behind an interface. In every module so far, *you* used a profile to fix the code. With profile-guided optimization (PGO), available since Go 1.21, you give the profile to the compiler instead, and it uses it to make better decisions. No code changes, only a different build.

## Learning Objectives

By the end of this module, you will:

- Know what the Go compiler does with a profile in Go 1.27: hot-callsite inlining, devirtualization and hot loop alignment
- Know the `-pgo` flag (`auto`, `off`, a file) and where `default.pgo` must live
- Collect a representative CPU profile and build with it
- Read the compiler's PGO decisions with `-gcflags=-m` and `-d=pgodebug=1`
- Explain why PGO can remove *allocations* and not only call overhead
- Know PGO's limits and pitfalls

## The Concept: Feeding the Profile to the Compiler

### Hot-callsite inlining

![PGO: build, run under real load, collect a CPU profile, and rebuild with it so the compiler optimizes the hot paths](images/18-pgo-loop.webp)

You saw in [Inlining](06-inlining.md) that the inliner gives every function a cost and only inlines it if the cost is at most `inlineMaxBudget = 80`. With a profile, `cmd/compile/internal/inline/inl.go` adds a second, much bigger budget:

```go
// Threshold in CDF percentage for hot callsite inlining,
// that is, for a threshold of X the hottest callsites that
// make up the top X% of total edge weight will be
// considered hot for inlining candidates.
inlineCDFHotCallSiteThresholdPercent = float64(99)

// Budget increased due to hotness.
inlineHotMaxBudget int32 = 2000
```

The compiler sorts the call edges in the profile by weight and marks the hottest ones, those that add up to 99% of the total weight, as **hot**. Then `inlineCostOK` decides each call:

```go
if metric <= maxCost {
	// Simple case. Function is already cheap enough.
	return true, 0, metric, hot
}

// We'll also allow inlining of hot functions below inlineHotMaxBudget,
// but only in small functions.
if !hot {
	// Cold
	return false, maxCost, metric, false
}
...
if bigCaller { ... return false, ... }   // caller has > 5000 IR nodes
if metric > inlineHotMaxBudget {
	return false, inlineHotMaxBudget, metric, false
}
```

So a function with cost 101 is never inlined in a normal build. At a **hot call site** in a not-too-big caller, it can be inlined up to cost 2000. The rest of the program keeps the normal budget, so the binary barely grows.

### Devirtualization

An interface method call is an indirect call through the itab, so the compiler can't inline it (see [Interfaces and Boxing](08-interfaces-boxing.md)). The profile records which concrete method each call site actually reached. `devirtualize/pgo.go` uses that to rewrite the hottest target into a guarded direct call:

```go
// to:
//
//	func foo(i Iface) {
//		if c, ok := i.(Concrete); ok {
//			c.Foo()
//		} else {
//			i.Foo()
//		}
//	}
//
// The primary benefit of this transformation is enabling inlining of the
// direct call.
```

The direct call `c.Foo()` can then be inlined (with the hot budget). The fallback keeps the program correct when another type shows up. The same is done for calls through **function values** (`if FuncPC(fn) == FuncPC(Concrete) { Concrete() }`).

### And a bit of layout

When a function is hot, the SSA backend also tags its loop headers, and on amd64 `ssagen` aligns them to a 64-byte cache line if 31 bytes or fewer remain in the current line (`hotAlign = 64`, `hotRequire = 31` in `ssagen/ssa.go`, controlled by `-d=alignhot`). That's a small effect, and it applies on amd64 and 386 only.

### How the go command finds the profile

The `-pgo` build flag (`go help build`):

- `-pgo=auto` (the default): for each **main package**, use `default.pgo` from that package's directory, and apply it to the main package and all its dependencies
- `-pgo=off`: no PGO
- `-pgo=path/to/file.pprof`: use this profile for every package in the build

Two consequences: a `default.pgo` in a library directory is ignored, and `go test` on a non-main package never picks one up automatically. For benchmarks you pass `-pgo=file` explicitly. The profile used is recorded in the binary: `go version -m ./binary` shows a `-pgo=` build setting.

## The Lab

This lab is different: **there is no `after.go`**. The code doesn't change, only the build does, so `make bench LAB=18` doesn't apply. You compare two builds with plain `go test` and `benchstat old.txt new.txt`.

> **Shortcut:** `make pgo` (from the repository root) runs the whole cycle below in one go: benchmark with `-pgo=off`, collect a CPU profile, rebuild with it, and print the `benchstat` comparison. Do it by hand the first time, though: the point of this module is seeing each step.

```
labs/18-pgo/
  ingest.go         an access-log ingester (don't edit)
  lab_test.go       correctness test + BenchmarkIngest
  cmd/ingest/       the same workload as a main program, for default.pgo
```

`(*Aggregator).Ingest` parses lines like `17,200,1534\n` with `parseRecord`, which returns a `*Record`, and passes each record to an `Observer` interface. In production the observer is always a `*Histogram`, but the compiler can't know that. Run the inliner's diagnostics to see the starting point:

```bash
cd labs
go build -gcflags='labs/18-pgo=-m=2' ./18-pgo/ 2>&1 | grep -E 'parseRecord|Observe|Ingest|Record\{\}'
```

```
18-pgo/ingest.go:28:6: cannot inline parseRecord: function too complex: cost 101 exceeds budget 80
18-pgo/ingest.go:70:6: cannot inline (*Histogram).Observe: function too complex: cost 93 exceeds budget 80
18-pgo/ingest.go:115:6: cannot inline (*Aggregator).Ingest: unhandled op DEFER
18-pgo/ingest.go:29:7: &Record{} escapes to heap
```

Both functions are just over the budget, and since `parseRecord` is a real call, its `&Record{}` escapes: **one heap allocation per line**.

### Step 1: Measure without PGO

From `labs/`:

```bash
go test -pgo=off -run '^$' -bench Ingest -benchmem -count 10 ./18-pgo/ | tee old.txt
```

### Step 2: Collect a profile

The profile must come from a representative workload. Here, that's the benchmark itself:

```bash
go test -run '^$' -bench Ingest -cpuprofile cpu.prof ./18-pgo/
go tool pprof -top cpu.prof | head -12
```

```
      flat  flat%   sum%        cum   cum%
     0.45s 32.14% 32.14%      0.93s 66.43%  labs/18-pgo.parseRecord
     0.28s 20.00% 52.14%      0.48s 34.29%  runtime.mallocgcTinySC2
     0.21s 15.00% 67.14%      0.21s 15.00%  labs/18-pgo.(*Histogram).Observe
     0.14s 10.00% 77.14%      1.28s 91.43%  labs/18-pgo.(*Aggregator).Ingest
     0.06s  4.29% 81.43%      0.06s  4.29%  runtime.getMCache (inline)
```

About a third of the time goes to `runtime.mallocgcTinySC2`, allocating the 12-byte `Record`s (16 bytes each in the tiny allocator). Note that there is no `newobject` in between: since Go 1.27 the compiler calls a size-specialized allocation routine directly for small objects, which makes each allocation cheaper than in Go 1.26 but doesn't remove it.

### Step 3: See what the compiler does with it

```bash
go build -pgo=cpu.prof -gcflags='labs/18-pgo=-m -d=pgodebug=1' ./18-pgo/ 2>&1 \
  | grep -E 'hot|PGO|parseRecord|Observe|Record\{\}'
```

```
18-pgo/ingest.go:125:16: PGO devirtualizing interface call a.obs.Observe to (*Histogram).Observe
hot-callsite-thres-from-CDF=0.6369426751592357
hot-node enabled increased budget=2000 for func=labs/18-pgo.parseRecord
18-pgo/ingest.go:28:6: can inline parseRecord
hot-node enabled increased budget=2000 for func=labs/18-pgo.(*Histogram).Observe
18-pgo/ingest.go:70:6: can inline (*Histogram).Observe
18-pgo/ingest.go:100:6: can inline (*Counter).Observe
hot-node enabled increased budget=2000 for func=labs/18-pgo.(*Aggregator).Ingest
hot-budget check allows inlining for call labs/18-pgo.parseRecord (cost 101) at 18-pgo/ingest.go:120:27 in function labs/18-pgo.(*Aggregator).Ingest
18-pgo/ingest.go:120:27: inlining call to parseRecord
hot-budget check allows inlining for call labs/18-pgo.(*Histogram).Observe (cost 93) at 18-pgo/ingest.go:125:16 in function labs/18-pgo.(*Aggregator).Ingest
18-pgo/ingest.go:125:16: inlining call to (*Histogram).Observe
18-pgo/ingest.go:29:7: &Record{} escapes to heap
18-pgo/ingest.go:120:27: &Record{} does not escape
```

Read it line by line:

1. The interface call was **devirtualized** to `(*Histogram).Observe`.
2. Both callees were **inlined with the hot budget** (101 and 93 > 80).
3. The last two lines are the real payoff. The standalone `parseRecord` still allocates (`ingest.go:29`), but **the copy inlined into `Ingest` doesn't** (`ingest.go:120`). Once the body is inlined, escape analysis sees that the record is only copied into `Observe` and never leaves `Ingest`, so it lives on the stack. That's the inlining and escape analysis interaction from [Inlining and Escape Analysis](07-inlining-escape.md), now unlocked by the profile.

### Step 4: Measure with PGO

```bash
go test -pgo=cpu.prof -run '^$' -bench Ingest -benchmem -count 10 ./18-pgo/ | tee new.txt
benchstat old.txt new.txt
```

On an AMD Ryzen AI MAX+ 395:

```
          │   old.txt   │               new.txt               │
          │   sec/op    │   sec/op     vs base                │
Ingest-32   1.979m ± 3%   1.004m ± 1%  -49.28% (p=0.000 n=10)

          │   old.txt    │                new.txt                │
          │     B/s      │      B/s       vs base                │
Ingest-32   563.5Mi ± 3%   1111.1Mi ± 1%  +97.17% (p=0.000 n=10)

          │   old.txt   │             new.txt                │
          │  allocs/op  │ allocs/op  vs base                 │
Ingest-32   100.0k ± 0%   0.0k ± 0%  -100.00% (p=0.000 n=10)
```

**Twice as fast, and 100,000 allocations per run became zero**, without touching the source.

### Step 5: The real workflow, with `default.pgo`

In production you profile the real binary (for example via `net/http/pprof`, for 30 seconds or more under typical load), commit the profile as `default.pgo` next to `main.go`, and a plain `go build` uses it. Try it:

```bash
go run ./18-pgo/cmd/ingest -cpuprofile /tmp/ingest.prof
cp /tmp/ingest.prof 18-pgo/cmd/ingest/default.pgo
go build -o /tmp/ingest-pgo ./18-pgo/cmd/ingest
go build -pgo=off -o /tmp/ingest ./18-pgo/cmd/ingest
go version -m /tmp/ingest-pgo | grep pgo
/tmp/ingest; /tmp/ingest-pgo
```

```
	build	-pgo=/home/you/.../labs/18-pgo/cmd/ingest/default.pgo
ingested 50000000 lines in 987ms (19.7 ns/line)
ingested 50000000 lines in 509ms (10.2 ns/line)
```

The PGO binary was 8.4 KB bigger (2.996 MB → 3.004 MB, +0.3%). Delete `18-pgo/cmd/ingest/default.pgo` when you're done, so the other steps start from a clean state.

## Understanding the Results

**Where did the time go?** Not mainly to the removed calls. Let's isolate devirtualization by turning it off while keeping the profile:

```bash
go test -pgo=cpu.prof -gcflags='labs/18-pgo=-d=pgodevirtualize=0' -run '^$' -bench Ingest -benchmem -count 10 ./18-pgo/
```

```
          │   old.txt   │            nodevirt.txt             │               new.txt               │
          │   sec/op    │   sec/op     vs base                │   sec/op     vs base                │
Ingest-32   1.979m ± 3%   1.043m ± 4%  -47.31% (p=0.000 n=10)   1.004m ± 1%  -49.28% (p=0.000 n=10)
```

Almost all of the gain comes from inlining `parseRecord`, and specifically from the **allocation it removed**: `mallocgcTinySC2` was ~34% of the profile. Devirtualizing `Observe` added only ~4% on top (`-3.74%, p=0.000` against `nodevirt` on a quiet machine, and within the noise on a busy one). The indirect call always goes to the same target, so the CPU's branch predictor already handles it well (see [Branch Prediction](17-branch-prediction.md)). Devirtualization pays off most when the inlined body enables further optimizations, which wasn't the case for this `Observe`.

**Why does `Ingest` have a `defer`?** It's not just for the lock. Hot-callsite decisions are keyed on *(caller, line offset within the caller)*. If `Ingest` were small enough, it would itself be hot-inlined into `BenchmarkIngest`. The `parseRecord` call inside that inlined copy then belongs to a different caller, doesn't match the profile, and is compiled as a normal call. That's PGO working at the granularity of call sites: the effect depends on the *shape* of your call graph, and `-d=pgodebug=1` is how you find out what happened.

**Line offsets, not line numbers.** The profile stores call sites as offsets from the start of the function (`NodeLineOffset` in `pgoir/irgraph.go`). Editing other functions doesn't invalidate a profile, but editing *inside* a hot function shifts its call sites until you collect a fresh one.

## PGO in Practice

- **Typical gains are 2–14%** (from the Go team's own measurements in the [PGO guide](https://go.dev/doc/pgo)). The 2x here comes from a lab designed to hit the sweet spot: a hot allocation that only disappears once a function is inlined.
- **Representative profiles matter.** A profile from a load test that exercises the wrong path makes the wrong things hot. You can merge profiles from several instances with `go tool pprof -proto a.prof b.prof > merged.prof`.
- **Iterate.** It's fine to profile a PGO-built binary and use that profile for the next build. The Go team designed it to be stable over iterations.
- **Build cost.** PGO builds take longer and recompile all dependencies with the profile. The go command caches the preprocessed profile (`go tool preprofile`) to help.
- **It doesn't replace fixing the code.** Moving the allocation out by hand (returning `Record` by value) would get the same speedup in every build. PGO is great for code you can't or don't want to restructure, like dependencies and the standard library.

## Key Takeaways

- PGO gives the compiler a CPU profile. In Go 1.27 it uses it for **hot-callsite inlining** (budget 80 → 2000), **devirtualization** of interface and function-value calls, and **hot loop alignment** on amd64
- `-pgo=auto` uses `default.pgo` next to a **main** package. Tests and libraries need `-pgo=file`
- `-gcflags=-m -d=pgodebug=1` shows every PGO decision
- Inlining is an enabler: the biggest wins come from what inlining unlocks, like **escape analysis keeping values on the stack**
- Decisions are made per call site, so the call graph's shape matters, and the profile must be representative

## Going Further (optional)

1. Change `parseRecord` to return `Record` by value, and benchmark with `-pgo=off`. How close do you get to the PGO build?
2. Change `observer` in `lab_test.go` to `&Counter{}`, collect a new profile, and check with `-d=pgodebug=1` which type gets devirtualized now. What happens if you build with the *old* profile and run with the new observer? (The `INGEST_COUNT_ONLY=1` environment variable does the same for `cmd/ingest`.)
3. Try `-gcflags='labs/18-pgo=-d=pgoinlinebudget=100'` and `-d=pgoinlinecdfthreshold=50`. Which decisions change?
4. Remove the `defer` from `Ingest` (unlock manually on every return), rebuild with `-pgo=cpu.prof -gcflags=-m`, and check whether `&Record{}` still stays on the stack.
