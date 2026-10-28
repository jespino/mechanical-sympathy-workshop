# Capstone: Optimize a Log Analyzer

> **Want to learn more?** Keep the [`runtime/pprof`](https://pkg.go.dev/runtime/pprof) and [`benchstat`](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat) docs at hand, and the [Diagnostics](https://go.dev/doc/diagnostics) page of the Go documentation. Everything else you need is in the previous modules.

This is the module where you stop following steps. You get a realistic program, an access-log analyzer, written the way a lot of production code is written: correct, readable, and full of the anti-patterns we have studied. Your job is to make it fast without changing a single byte of its output. Nobody tells you what to fix. The profile does.

## Learning Objectives

By the end of this module, you will:

- Run a full optimization loop: **profile → hypothesize → change one thing → test → benchstat → repeat**
- Recognize the workshop's anti-patterns from their profile signatures
- Prioritize by measured cost, not by how ugly the code looks
- Keep a record of every step, so each change is backed by numbers
- Know when to stop

## The Program

`AnalyzeBefore(r io.Reader, w io.Writer)` reads lines like this one:

```
2026-10-23T12:00:00.123Z GET /api/users/1234 200 1534us "Mozilla/5.0 (X11; Linux x86_64) ... Chrome/129.0.0.0 Safari/537.36"
```

(timestamp, method, path, status, latency, user agent). It groups requests by route, with numeric path segments replaced by `:id` and query strings dropped, and writes a report:

```
lines: 100000  malformed: 187  routes: 64
window: 2026-10-23T12:00:00.015Z .. 2026-10-23T12:15:51.791Z

ROUTE                                   COUNT   P50(us)   P99(us)     2xx     3xx     4xx     5xx
GET /v2/reports/:id/export               3203      2747    418761    1705     372     766     360
GET /api/search                          3187      2733    451878    1699     375     758     355
GET /static/img/:id                      3183      2732    405572    1660     398     758     367
...
DELETE /api/products                      675      2866    584031     341      90     141     103

user agents:
  bot         12422  12.4%
  chrome      12706  12.7%
  ...
```

The input comes from `GenerateLog(n, seed)`, which is deterministic and includes about 0.2% malformed lines (a missing status, or a line cut short). Nothing is written to disk: tests and benchmarks generate their data in memory.

Read `labs/19-capstone/before.go` now, top to bottom. It's ~200 lines. Note what *you* think is slow, and then let the profile prove you right or wrong.

## The Lab

```
labs/19-capstone/
  gen.go               deterministic log generator (don't edit)
  before.go            the original analyzer (baseline, don't edit)
  after.go             your copy: make this one fast
  solution.go          reference solution (built with -tags solution)
  lab_test.go          output-equality tests + BenchmarkAnalyze
  cmd/loganalyzer/     run either version as a program, profile it, dump the data
```

The rules:

1. **The report must stay byte-for-byte identical.** `go test ./19-capstone/` compares `before` and `after` on three seeds and on a set of tricky hand-written lines (invalid dates, tabs, quotes inside the user agent, `+7` as a path segment, time zones...). Your parser must accept and reject exactly what the regexp and `time.Parse` do.
2. **One change per iteration**, so you know what each change bought you.
3. **Every change gets a benchstat.** No "it feels faster".

The benchmark analyzes 100,000 lines (~14 MB) per op and writes the report to `/dev/null` through an `*os.File`, like a CLI writing to stdout. The test uses 3,000 lines, so it stays fast even for `before.go`.

### The loop

From the repository root:

```bash
make bench LAB=19                      # before vs after, saved to labs/19.bench.txt
cp labs/19.bench.txt labs/19-step0.bench.txt
```

Then, for each iteration:

```bash
# 1. Profile your current version
cd labs
go test -run '^$' -bench 'Analyze/impl=after' -cpuprofile cpu.prof ./19-capstone/
go tool pprof -top cpu.prof | head -25        # where is the flat time?
go tool pprof -top -cum cpu.prof | head -40   # which of *your* functions own it?
go tool pprof -http=: cpu.prof                # flame graph, source view
cd ..

# 2. Hypothesize: "X costs ~N% because of Y. If I do Z, it should go away."
# 3. Change ONE thing in after.go
# 4. Check correctness
go test ./labs/19-capstone/        # or: make test-labs

# 5. Measure, and compare with the previous step
make bench LAB=19
cp labs/19.bench.txt labs/19-step1.bench.txt
benchstat -filter '/impl:after' labs/19-step0.bench.txt labs/19-step1.bench.txt
```

`make bench` always shows you the total distance from `before`. The `-filter '/impl:after'` comparison shows what *this* step bought. Also use `-benchmem` columns and `-memprofile` (`go tool pprof -sample_index=alloc_objects`) when allocations are the suspect. Those `*.bench.txt` files are gitignored.

### Reading the profile: symptoms → modules

| What you see in the profile | Likely cause | Module |
|---|---|---|
| `runtime.mallocgc`, `newobject`, `growslice` high in `-cum` | Allocations per item, slices growing from empty | [03](03-preallocation.md), [12](12-gc-pressure.md) |
| `gcBgMarkWorker`, `gcDrain`, `scanobject`, `markroot` | GC work caused by allocation rate, or pointers to scan | [12](12-gc-pressure.md) |
| `stackScanState`, `scanstack`, `lfstack` | The GC scanning **many goroutine stacks** | [13](13-goroutines-channels.md), [12](12-gc-pressure.md) |
| `runtime.newproc`, `schedule`, `findRunnable`, `gopark` | Goroutines that are too small for their cost | [13](13-goroutines-channels.md) |
| `sync.(*Mutex).Lock`, `lock2`, `futex`, `procyield` | Lock contention | [16](16-contention-false-sharing.md) |
| `regexp.(*Regexp).doOnePass`, `doExecute`, `backtrack` | Regexp on the hot path | [04](04-strings-bytes.md) |
| `strings.genSplit`, `Join`, `ToLower`, `slicebytetostring`, `concatstrings` | String copies and per-item temporary strings | [04](04-strings-bytes.md) |
| `fmt.Sprintf`, `fmt.(*pp).doPrintf`, `convT64`, `convTstring` | fmt and interface boxing | [04](04-strings-bytes.md), [08](08-interfaces-boxing.md) |
| `runtime.assertE2I`, `typeAssert`, `map[string]any` | Dynamic typing on the hot path | [08](08-interfaces-boxing.md) |
| `sort.Slice`, `reflectlite.Swapper`, `sort.(*lessSwap)` | Reflection-based sorting with closures | [08](08-interfaces-boxing.md) |
| `mapaccess*`, `mapassign*` | Too many map operations, or maps where an array would do | [03](03-preallocation.md), [10](10-struct-layout.md) |
| `time.Parse`, `strconv` | Parsing work that isn't needed for the result | this module |
| `syscall.Syscall`, `internal/poll.(*FD).Write` | Unbuffered I/O | [14](14-io-syscalls.md) |
| Small functions just over the inlining budget in the hot loop | Call overhead, escapes that inlining would remove | [06](06-inlining.md), [07](07-inlining-escape.md), [18](18-pgo.md) |

Also look at the header line of `go tool pprof -top`: `Duration: 1.05s, Total samples = 7.65s` means the program kept ~7.3 cores busy. Wall time is what `sec/op` measures, but CPU time is what your cloud bill measures.

### Step 1: Baseline

Run `make bench LAB=19` and profile `impl=before`. Write down the top five entries of `-top` and `-top -cum`. On an AMD Ryzen AI MAX+ 395, `before` takes ~320ms per 100k lines (~42 MB/s), allocates ~170 MB and makes 2.3 million allocations per run.

### Step 2 and onward: your iterations

Work through the loop until you're happy. A reasonable target is **10x** faster with **under 1% of the allocations**. The reference solution reaches ~17x.

If you get stuck, the **Hints** section at the end goes from vague to specific. Try to use them one at a time.

## Understanding the Results

> **Spoilers.** Read this after your own attempt.

Here is how the reference solution was built, one step at a time. Each row is the `impl=after` benchmark at that step (6 runs each, AMD Ryzen AI MAX+ 395, other jobs running on the machine). The "vs prev" column is `benchstat` against the previous step, all with p=0.002.

| Step | Change | sec/op | vs prev | B/op | allocs/op |
|---|---|---|---|---|---|
| 0 | `before.go` as is | 321.6ms | | 176.3 MiB | 2,355k |
| 1 | No goroutine per line, no mutex: a plain loop | 240.9ms | -25% | 149.7 MiB | 2,069k |
| 2 | Hand-written parser returning a struct, instead of regexp + `map[string]any` | 74.3ms | -69% | 89.7 MiB | 1,225k |
| 3 | Route key built in a reused `[]byte`, looked up with `m[string(key)]`. User agent family cached per distinct UA | 26.7ms | -64% | 49.9 MiB | 1.9k |
| 4 | `slices.Sort` instead of `sort.Slice` | 24.0ms | -10% | 49.9 MiB | 1.7k |
| 5 | `[10]int` status classes instead of a `map[int]int` per route. Timestamps compared as strings, no `time.Parse` per line | 22.0ms | -8% | 49.8 MiB | 1.4k |
| 6 | Stream the input through a `bufio.Reader` and parse `[]byte` in place (no `ReadAll`, no `Split`). Buffered output | 17.9ms | -19% | 2.5 MiB | 1.3k |

And the final comparison (`make solution LAB=19`):

```
           │    before     │               after                │
           │    sec/op     │   sec/op     vs base               │
Analyze-32   322.24m ± 14%   18.48m ± 2%  -94.27% (p=0.002 n=6)

           │    before     │                 after                  │
           │      B/s      │      B/s       vs base                 │
Analyze-32   42.43Mi ± 12%   739.85Mi ± 2%  +1643.74% (p=0.002 n=6)

           │     before     │                after                │
           │      B/op      │     B/op      vs base               │
Analyze-32   167.722Mi ± 7%   2.489Mi ± 0%  -98.52% (p=0.002 n=6)

           │     before     │               after                │
           │   allocs/op    │  allocs/op   vs base               │
Analyze-32   2339.365k ± 1%   1.319k ± 0%  -99.94% (p=0.002 n=6)
```

**17.4x faster, 1,800x fewer allocations, single-threaded.** On a million lines (143 MB), `go run ./19-capstone/cmd/loganalyzer -impl before -lines 1000000` takes 3.1s, and the solution takes 200ms.

What each step teaches:

**Step 1: the "parallel" version was the slowest part.** The baseline profile showed 7.65s of CPU in 1.05s of wall time, and the biggest entry wasn't the regexp. It was `runtime.(*lfstack).pop` under `runtime.getempty`, called from `stackScanState.putPtr` and `stackScanState.addObject`: **the GC scanning the stacks of up to 100,000 live goroutines**, all fighting over a global list of work buffers. The per-line work is a few microseconds at most, far too little to pay for a goroutine plus a contended mutex. Going sequential was only 25% faster in wall time, but it used **one core instead of ~7.3** (the step 1 profile: `Total samples = 1.08s` in 1.02s). If you need parallelism, split the input into a few big chunks with one goroutine and one private result per chunk, and merge at the end (see Going Further).

**Step 2: regexp and `map[string]any`.** `doOnePass` and `mallocgc` dominated. A regexp match with submatches allocates, and a `map[string]any` per line boxes every value and allocates a map. A ~40-line parser that checks the same grammar removed two thirds of the time. The edge-case test is what makes this safe: `\S` means "not one of `\t\n\f\r `", and `\d{3}` means exactly three ASCII digits.

**Step 3: the biggest allocation win.** `strings.Split` + `Join` in `normalizeRoute`, `fmt.Sprintf` for the key, and `strings.ToLower` on a 100-byte user agent each allocated per line. Building the key into a reused buffer and looking it up with `m[string(buf)]` (the compiler doesn't allocate for that conversion in a map index) took allocations **from 1.2 million to 1,857**. There are only 8 distinct user agents, so classifying each one once and caching the result is free.

**Steps 4 and 5: the diminishing returns.** `sort.Slice` goes through a reflection-based swapper and a closure for every comparison, while `slices.Sort` is generic and specialized for `[]int`. Fixed-layout timestamps sort as strings, so `time.Parse` only runs twice per report. A cheap range check keeps the "invalid date is malformed" behaviour. These gave ~10% and ~8%. They're real, but they're the kind of change you'd skip if you were short on time.

**Step 6: the last copy.** `io.ReadAll` grows a buffer up to 14 MB (copying as it goes), then `string(data)` copies all of it again, then `strings.Split` allocates a 100k-entry slice. Streaming through a 64 KiB `bufio.Reader` and parsing `[]byte` in place cut B/op from 50 MB to 2.5 MB. Most of what's left is the latency slices themselves.

**And the one that didn't matter: unbuffered output.** `strace -c` shows that `before` makes 76 `write` syscalls (one per report line), against 3 for the solution. At ~100ns each, that's ~7µs out of 300ms. The solution buffers the output because it's free to do, but it **still uses `fmt.Fprintf` for the report**. It runs ~75 times per report, so it's cold code, and optimizing it would only make it harder to read. The profile tells you where to stop as well as where to start.

## Hints

Use them one at a time, from the top.

1. Before changing anything, look at the *header* of the baseline profile (`Total samples`), and at the top of `-top` (flat), not only `-cum`. Is the program spending its time in your code or in the runtime?
2. Who calls the hottest runtime functions? Use `go tool pprof -peek 'runtime.getempty$' cpu.prof`, or the flame graph.
3. How long does processing one line take, compared to starting a goroutine and taking a contended lock? Would 32 workers with private maps, merged at the end, be different from 100,000 goroutines sharing one map?
4. Once the loop is sequential, the parse is the next big block. A regexp with capture groups allocates per match. What does the line grammar look like if you write it as a few `for` loops?
5. `map[string]any` boxes every value (see `convT64`, `convTstring`) and allocates a map per line. A small struct returned by value doesn't allocate at all.
6. Run `-memprofile` with `-sample_index=alloc_objects`. Which three functions allocate per line? (`normalizeRoute`, `fmt.Sprintf`, `agentFamily`.)
7. `m[string(b)]` with `b []byte` does not allocate. Build `"METHOD /route/:id"` into a `[]byte` you reuse across lines.
8. How many distinct user agents are there in the log? Do you need to lowercase each one 100,000 times?
9. `sort.Slice` vs `slices.Sort`: look at `sort.(*lessSwap)` and `reflectlite.Swapper` in the profile.
10. A `map[int]int` per route for status codes, when you only need `status/100`: what's the smallest data structure that works?
11. `"2026-10-23T12:00:00.015Z"` < `"2026-10-23T12:00:01.000Z"` as strings. When is that true for all timestamps? What else must you check so that invalid dates are still rejected?
12. `io.ReadAll` + `string(data)` + `strings.Split` copies the whole input twice and allocates a slice of 100k strings. `bufio.Reader.ReadSlice('\n')` hands you each line in place, as a `[]byte`. Watch out for lines longer than the buffer (`bufio.ErrBufferFull`).
13. The output: `w` is an `*os.File` in the benchmark. Is it buffered? Does it matter? Measure before you decide.

## Key Takeaways

- The loop is **profile → hypothesize → change one thing → test → benchstat**. Keep every step's numbers
- Check the profile header: CPU time can be many times the wall time, and burning cores is a cost too
- Fan-out at the wrong granularity is worse than no fan-out: goroutines cost scheduling, memory and **GC stack scanning**
- The biggest wins came from removing *per-item allocations* (regexp, boxing, string building), not from micro-tuning
- An output-equality test, with edge cases, is what lets you rewrite a parser fearlessly
- Stop when the profile says the rest is cold code. Readable `fmt` in a report that runs once is fine

## Going Further (optional)

1. **Parallelism done right.** Split the input into `runtime.GOMAXPROCS(0)` chunks at line boundaries, give each goroutine its own `analyzer`, and merge the results (concatenate latency slices, add counters). How close to linear is the speedup? What does the merge cost? Keep the output identical.
2. **Percentiles without sorting.** The latencies take most of the remaining memory. Could a fixed histogram (like HDR histograms) replace the slices? Why can't you do that here without changing the report?
3. **PGO on top.** Collect a profile of your final version and rebuild with `-pgo` (see [Profile-Guided Optimization](18-pgo.md)). Is there anything left for the compiler to win?
4. **GC tuning.** Run your step 3 version with `GOGC=400` and `GOGC=off`. How much of the remaining time was GC? Then compare with the final version. Why does GOGC barely matter for it?
