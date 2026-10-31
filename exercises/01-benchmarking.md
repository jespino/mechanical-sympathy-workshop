# Benchmarking 101

Every module in this workshop ends with the same sentence: *measure it*. So before we optimize anything, we need to know how to measure. Go ships a good tool for that in the `testing` package. In this module we see how to write and run a benchmark, how to use `benchstat` to find out whether a change to the code made a difference, and how the labs of this workshop work.

## Learning Objectives

By the end of this module, you will:

- Know how to write a benchmark with `b.Loop`, and what it does for you
- Know how to run a benchmark and read its output
- Use `benchstat` to compare two versions of your code with statistics instead of eyeballing
- Know how the labs in the rest of the workshop are organized

## How `go test -bench` Runs Your Code

### Writing a benchmark with `b.Loop`

A benchmark is a function whose name starts with `Benchmark`, that takes a `*testing.B`, and puts the code to measure inside a `for b.Loop()` loop:

```go
func BenchmarkFoo(b *testing.B) {
    // setup: runs once, not measured
    for b.Loop() {
        Foo()
    }
}
```

`b.Loop` (Go 1.24+) takes care of the hard parts for you:

- **It decides how many iterations to run.** It keeps the loop going until it has run long enough to give a stable number (1 second by default, `-benchtime` changes it), and reports the total time divided by the number of iterations: the `ns/op` column.
- **The setup is never measured.** Whatever you do before the loop runs once, and the timer starts at the first `b.Loop()` call.
- **The loop body is kept alive**, so the compiler can't throw away a call just because you don't use its result.

> **The old way.** You'll still see benchmarks written as `for i := 0; i < b.N; i++ { ... }`. They work, but you have to call `b.ResetTimer()` after the setup and make sure the compiler doesn't throw your code away. `b.Loop` does both for you, and it's the recommended way since Go 1.24.

### Running a benchmark

Run benchmarks with `go test -bench`. From the `labs/` directory:

```bash
go test -run '^$' -bench Stats -benchmem ./01-benchmarking/
```

`-bench Stats` picks which benchmarks to run (a regexp, `.` for all), `-run '^$'` skips the regular tests, and `-benchmem` adds memory stats. Each result is one line:

```
BenchmarkStats/impl=before-32    	  305234	      3953 ns/op	       0 B/op	       0 allocs/op
```

That's the name (`-32` is the number of CPUs it could use), the number of iterations, the time per iteration, and the bytes and allocations per iteration.

Two more flags you'll see: `-count 6` runs each benchmark 6 times, and `-benchtime 2s` makes each run longer. In the labs, `make bench LAB=NN` does all of this for you.

### Why `benchstat`

A single run tells you almost nothing: on a laptop, with a browser open (or, in this workshop, other people's benchmarks running), the same code can vary by 10% or more. [`benchstat`](https://pkg.go.dev/golang.org/x/perf/cmd/benchstat) runs the numbers for you. It takes several runs of each benchmark and shows the typical time (the **median**) and how much the runs varied (`± 4%`). When you compare two sets of runs, it also tells you whether the difference is real or could just be noise. If it could be noise, it prints `~` instead of a percentage. The `p=` value next to it is the chance of seeing a difference that big by luck alone. Below 0.05 counts as a real difference.

To compare two versions of your own code, save the results before and after the change and give both files to `benchstat`:

```bash
go test -run '^$' -bench . -count 6 > old.txt
# ...change the code...
go test -run '^$' -bench . -count 6 > new.txt
benchstat old.txt new.txt
```

### How the labs work

Almost every lab, starting with this one, has the same files in `labs/NN-name/`:

- `before.go`: the slow version. Don't edit it.
- `after.go`: your copy. It starts identical to `before.go` (its functions end in `After`), and it's the file you change.
- `solution.go`: a reference solution, if you get stuck.
- `lab_test.go`: a test that checks your version still gives the same results, plus the benchmarks.

Each benchmark measures both versions in the same run, as two sub-benchmarks named `impl=before` and `impl=after`. You run them with:

```bash
make bench LAB=03
```

This runs the lab's benchmarks 6 times, saves the output to `labs/03.bench.txt`, and then runs `benchstat -col /impl`, which puts `before` and `after` side by side and compares them:

```
              │    before     │                after                │
              │    sec/op     │    sec/op     vs base               │
Squares-32      28.684µ ± 30%   6.690µ ± 13%  -76.68% (p=0.002 n=6)
```

There's no baseline to save: edit `after.go` and run `make bench` again. `make solution LAB=03` does the same with the reference solution instead of your copy. To run or profile only one side, add it to the benchmark name, like `-bench 'Squares/impl=before'`.

## The Lab

A first lab, small on purpose, to get used to the loop every module follows. In `labs/01-benchmarking/`:

```
lab.go        Sum, which adds up a slice of ints
before.go     StatsBefore (don't edit)
after.go      StatsAfter: your copy, the one you change
solution.go   reference solution (built with -tags solution)
lab_test.go   the correctness test and BenchmarkStats
```

`StatsBefore` returns the sum and the average of a slice:

```go
func StatsBefore(nums []int) (sum, avg int) {
	return Sum(nums), Sum(nums) / len(nums)
}
```

It works, but it calls `Sum` twice with the same slice, so it adds up the 10,000 numbers twice.

### Step 1: Measure the baseline

From the repository root:

```bash
make bench LAB=01
```

`after.go` is still a copy of `before.go`, so the two columns should be the same (`~`):

```
         │   before    │            after             │
         │   sec/op    │   sec/op     vs base         │
Stats-32   3.966µ ± 1%   3.995µ ± 2%  ~ (p=0.180 n=6)
```

That's your noise floor: the `±` values tell you how noisy your machine is.

### Step 2: Fix it

Edit `labs/01-benchmarking/after.go` so that `StatsAfter` calls `Sum` only once and reuses the result for the average. Then check it still gives the same results:

```bash
cd labs
go test ./01-benchmarking/
```

### Step 3: Compare

```bash
make bench LAB=01
```

Here are the results with the reference solution (`make solution LAB=01`) on my machine:

```
         │   before    │               after                │
         │   sec/op    │   sec/op     vs base               │
Stats-32   4.006µ ± 2%   1.999µ ± 2%  -50.11% (p=0.002 n=6)
```

## Understanding the Results

Half the work, half the time: `before` adds up the slice twice and `after` only once, so it goes from about 4 µs to about 2 µs. `benchstat` reads it as `-50.11%`, and `p=0.002` says that a difference this big is very unlikely to be luck.

Your numbers will be different, but the ratio should be close to 2x. If `benchstat` prints `~` instead, the runs were too noisy to tell: close other programs and run it again.

## Key Takeaways

- A benchmark is a `BenchmarkXxx(b *testing.B)` function with the code to measure inside `for b.Loop()`
- `b.Loop` picks the number of iterations for you, never times the setup, and keeps call results alive
- One run tells you almost nothing: use `-count 6` (or more) and `benchstat`
- `~` means "can't tell from noise", and it's a perfectly good answer
- In every lab: edit `after.go`, run `make bench LAB=NN`, and compare the two columns

## Going Further (optional)

1. Write your own benchmark from scratch, outside the workshop's machinery: a new directory with a `go.mod`, a small function, and a `_test.go` file with a `BenchmarkXxx` that uses `b.Loop`.
2. Run it with `go test -bench .` and play with the flags: `-count`, `-benchtime`, `-benchmem`. How much do the numbers change from one run to the next?
3. Save a run to `old.txt`, change the function, save another run to `new.txt`, and compare them with `benchstat old.txt new.txt`.
