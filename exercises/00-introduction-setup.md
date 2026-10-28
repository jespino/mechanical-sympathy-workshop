# Introduction and Setup

> **Want to learn more?** The term comes from racing driver Jackie Stewart ("you don't have to be an engineer to be a racing driver, but you do have to have mechanical sympathy") and was brought to software by Martin Thompson. His [Mechanical Sympathy blog](https://mechanical-sympathy.blogspot.com/) is still a great read.

**Mechanical sympathy** means writing software with an understanding of the machine it runs on. You don't fight it, you cooperate with it. In Go, "the machine" is really three layers stacked on top of each other:

1. **The compiler**, which decides what lives on the stack, what gets inlined, and which checks can be removed
2. **The runtime**, which allocates memory, runs the garbage collector, and schedules goroutines onto threads
3. **The hardware**, with its caches, cache lines, branch predictors, and the cost of crossing into the kernel

![The three layers under your Go code: compiler, runtime and hardware](images/00-layers.webp)

Most of the time Go hides all of this from you, and that's a feature. But when a piece of code is hot, the difference between code that works *with* these layers and code that works *against* them is often 2x, 10x, or more, usually with no change to the algorithm.

## How This Workshop Works

The workshop is a **crescendo**. We start with the tools to measure, then the simplest optimizations, and we go deeper into the machine as we go:

| Part | Modules | What you'll learn |
|---|---|---|
| I · Measure first | 0–2 | Benchmarks that don't lie, `benchstat`, and `pprof` |
| II · Allocations and the compiler | 3–9 | Pre-allocation, strings, escape analysis, inlining, interfaces, bounds checks |
| III · The GC and the runtime | 10–14 | Struct layout and size classes, `sync.Pool`, GC pressure, the scheduler, syscalls |
| IV · Memory and the hardware | 15–17 | CPU caches, contention and false sharing, branch prediction |
| V · Putting it all together | 18–19 | PGO and a capstone optimization project |

Every lab follows the same loop, which is also the loop you should use in real life:

![The workshop loop: measure, understand why, change one thing, measure again](images/00-loop.webp)

The middle step is the heart of each module. We look at the mechanism that makes the slow version slow: usually in the Go source code (the compiler, the runtime, the standard library), and in Part IV in the hardware itself, with performance counters. Once you understand the *why*, the fix is usually obvious, and you'll recognize the same pattern in code you've never seen before.

> **A word of caution.** Everything in this workshop is about *hot* code. Most code isn't hot. Readability wins by default, and you trade it for speed only where a profile tells you to. Part I exists precisely so that you never optimize blind.

## Step 1: Install Go 1.27

You need **Go 1.27 or newer**. The internals we read are from the 1.27 release, and some modules rely on recent features (`b.Loop`, Swiss Table maps, the Green Tea GC, size-specialized allocation).

Download it from [go.dev/dl](https://go.dev/dl/) or use your package manager, then check:

```bash
go version
```

```
go version go1.27.3 linux/amd64
```

Any 1.27.x patch release is fine. You do **not** need to build Go from source: everything in this workshop uses the stock toolchain.

## Step 2: Get the Workshop Code

```bash
git clone https://github.com/jespino/mechanical-sympathy-workshop.git
cd mechanical-sympathy-workshop
```

## Step 3: Install the Tools

### benchstat (required)

`benchstat` turns repeated benchmark runs into statistics: medians, confidence intervals and p-values. We'll use it in every lab.

```bash
go install golang.org/x/perf/cmd/benchstat@latest
benchstat -h
```

Make sure `$(go env GOPATH)/bin` is in your `PATH`.

### Graphviz (optional)

`go tool pprof -http=:` works without it, but the graph view needs `dot`:

```bash
# Debian/Ubuntu
sudo apt install graphviz
# macOS
brew install graphviz
```

### perf (optional, Linux only)

In Part IV we use hardware performance counters (cache misses, branch misses) to *prove* what is happening inside the CPU. The tool for that is `perf`:

```bash
# Debian/Ubuntu
sudo apt install linux-tools-common linux-tools-$(uname -r)
# Fedora
sudo dnf install perf
```

Many distributions don't let regular users read hardware counters by default. If `perf stat true` fails with a permissions error, lower the restriction for the duration of the workshop:

```bash
cat /proc/sys/kernel/perf_event_paranoid    # 3 or 4 means "blocked"
sudo sysctl kernel.perf_event_paranoid=1
```

> ⚠️ **Not on Linux?** `perf` only exists on Linux. On macOS or Windows (and inside many VMs and containers, where hardware counters aren't exposed), skip the `perf` steps. Every module also works with benchmarks alone. You'll see the effect in `sec/op`, you just won't see the counter that explains it. On macOS, Instruments' "CPU Counters" template can show similar data if you want to dig deeper.

## Step 4: Verify Your Environment

From the repository root, check that every lab compiles and passes its tests, both in your version and in the reference solutions:

```bash
make test-labs
```

Then run your first benchmark comparison, using the reference solution of the pre-allocation lab:

```bash
make solution LAB=03
```

After a few seconds you should see a `benchstat` table similar to this one, with large negative percentages in the `vs base` column:

```
              │    before     │                after                │
              │    sec/op     │    sec/op     vs base               │
Squares-32      28.684µ ± 30%   6.690µ ± 13%  -76.68% (p=0.002 n=6)
Evens-32        13.415µ ±  6%   7.069µ ±  7%  -47.31% (p=0.002 n=6)
CountWords-32    252.8µ ± 17%   152.6µ ± 10%  -39.63% (p=0.002 n=6)
```

If that works, you're ready. Don't worry about understanding the table yet: that's the job of the next module.

## Step 5: Make Your Machine Boring

Benchmarks measure your code *plus everything else your machine is doing*. To get stable numbers:

- **Plug in your laptop.** On battery, CPUs throttle aggressively.
- **Close heavy applications**: browsers with many tabs, IDE indexers, video calls, Docker builds.
- **Don't touch the machine** while a long benchmark runs.
- **On Linux, consider the `performance` governor** while benchmarking: `sudo cpupower frequency-set -g performance`.
- **Prefer bare metal.** VMs and CI runners share CPUs with noisy neighbours.

You'll never get zero noise, which is why we'll always run benchmarks several times and let `benchstat` tell us whether a difference is real.
