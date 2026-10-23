# I/O and Syscalls

> **Want to learn more?** Read `entersyscall`, `exitsyscall` and `retake` in [`runtime/proc.go`](https://github.com/golang/go/blob/go1.27.0/src/runtime/proc.go), `(*FD).Write` in [`internal/poll/fd_unix.go`](https://github.com/golang/go/blob/go1.27.0/src/internal/poll/fd_unix.go), `newFile` in [`os/file_unix.go`](https://github.com/golang/go/blob/go1.27.0/src/os/file_unix.go), and the [`bufio`](https://pkg.go.dev/bufio) package docs.

So far every cost we've looked at lived inside your process: allocations, cache misses, branches, locks. This module is about the cost of leaving it. Every time your program reads or writes a file, it asks the kernel to do it, and crossing that boundary is one of the most expensive "single operations" a Go program performs. We'll see what a syscall costs, what the Go scheduler does while one is in flight, and why buffering your I/O is often the biggest one-line speedup there is.

## Learning Objectives

By the end of this module, you will:

- Know what a syscall costs compared to a function call, and why
- Understand what `entersyscall` / `exitsyscall` do and how `sysmon` hands off a P stuck in a syscall
- Know the difference between network I/O (netpoller) and file I/O (blocking syscalls) in Go
- Know that `*os.File` is **unbuffered**, and use `bufio.Writer` / `bufio.Reader` to batch syscalls
- Count syscalls with `strace -c` and recognize them in a CPU profile
- Avoid the classic `bufio` bug: forgetting to `Flush`

## The Concept: Crossing Into the Kernel

### What a syscall costs

A syscall is not a function call. On x86-64, the `SYSCALL` instruction switches the CPU to kernel mode, the kernel saves the user registers, checks the syscall number, runs the handler, copies data between user and kernel memory, and then returns with `SYSRET`. On top of that, mitigations for CPU vulnerabilities (Spectre and friends) can add work at the boundary. You can see which ones your machine applies in `/sys/devices/system/cpu/vulnerabilities/`.

The lab includes a small program that measures the cost directly:

```bash
cd labs
go run ./17-io-syscalls/cmd/syscallcost
```

```
function call (noinline)                  1.8 ns/op
bufio.Writer.Write 1 byte                 3.3 ns/op
getpid via RawSyscall                    39.5 ns/op
getpid via Syscall (+entersyscall)       52.6 ns/op
os.File.Write 1 byte to /dev/null       100.6 ns/op
```

On an AMD Ryzen AI MAX+ 395, the cheapest possible syscall (`getpid`, which does nothing) costs about **20x a function call**. A real one-byte `write` to `/dev/null`, which the kernel throws away, costs ~100 ns, about **30x more than copying that byte into a `bufio` buffer**. The difference between the two `getpid` lines is Go's own bookkeeping, which we look at next.

### What Go does around a syscall

When your goroutine calls `syscall.Write`, it ends up in `syscall.Syscall` (`syscall/syscall_linux.go`):

```go
func Syscall(trap, a1, a2, a3 uintptr) (r1, r2 uintptr, err Errno) {
	runtime_entersyscall()
	...
	r1, r2, err = RawSyscall6(trap, a1, a2, a3, 0, 0, 0)
	runtime_exitsyscall()
	return
}
```

`runtime.entersyscall` (`reentersyscall` in `runtime/proc.go`) records the goroutine's stack pointer and PC and switches it from `_Grunning` to `_Gsyscall`. The M (OS thread) keeps its P, but from now on **the P can be taken away**:

```go
// As soon as we switch to _Gsyscall, we are in danger of losing our P.
// We must not touch it after this point.
```

Who takes it? `sysmon`, the runtime's monitor thread, which runs without a P and wakes up every 20µs to 10ms. In `retake` it looks at every P whose goroutine is in a syscall:

```go
// Retake the P if it's there for more than 1 sysmon tick (at least 20us).
...
// On the one hand we don't want to retake Ps if there is no other work to do,
// but on the other hand we want to retake them eventually
// because they can prevent the sysmon thread from deep sleep.
if runqempty(pp) && sched.nmspinning.Load()+sched.npidle.Load() > 0 && pd.syscallwhen+10*1000*1000 > now {
	thread.resume()
	goto done
}
...
handoffp(pp)
```

So a syscall that has been running for at least one sysmon tick loses its P to another thread (`handoffp`) **if there is other work waiting**, or after 10ms anyway. This is how a goroutine stuck in a slow `read` doesn't freeze a whole P. When the syscall returns, `exitsyscall` takes the fast path if the P is still there. Otherwise it has to find an idle P, or park the goroutine on the global run queue.

Most syscalls are short, so they pay only the fast path: ~10 ns of bookkeeping on top of the kernel crossing. Note that since Go 1.26 there is no `_Psyscall` state any more (it is still `_Psyscall_unused` in Go 1.27's `runtime2.go`). Whether a P is "in a syscall" is decided by looking at its goroutine's status.

### Network vs files: the netpoller

Not all I/O blocks a thread. Sockets and pipes are put in **non-blocking mode** and registered with the **netpoller** (epoll on Linux, kqueue on BSD/macOS). Look at `(*FD).Write` in `internal/poll/fd_unix.go`:

```go
n, err := ignoringEINTRIO(syscall.Write, fd.Sysfd, p[nn:max])
...
if err == syscall.EAGAIN && fd.pd.pollable() {
	if err = fd.pd.waitWrite(fd.isFile); err == nil {
		continue
	}
}
```

If a socket isn't ready, the syscall returns `EAGAIN` right away and the goroutine **parks** (no thread blocked), to be woken by the netpoller later. Regular files are different: epoll refuses them, and `newFile` in `os/file_unix.go` notes that registering "disk files on Linux systems" fails. So a file read or write is a plain **blocking syscall** that holds an M for its whole duration. That's the case where the `sysmon` handoff above matters.

Either way, **every** `Read` or `Write` on an `*os.File` or `net.Conn` is at least one syscall. `*os.File` has no buffer. `fmt.Fprintf(f, ...)` formats into a temporary buffer and then calls `f.Write` once, so it costs **one syscall per call**. The same goes for `fmt.Println`, which writes to `os.Stdout`, an `*os.File`.

### Batching with bufio

`bufio.Writer` keeps a buffer (`defaultBufSize = 4096` in `bufio/bufio.go`) and only calls the underlying `Write` when it's full:

```go
func (b *Writer) Write(p []byte) (nn int, err error) {
	for len(p) > b.Available() && b.err == nil {
		if b.Buffered() == 0 {
			// Large write, empty buffer.
			// Write directly from p to avoid copy.
			n, b.err = b.wr.Write(p)
		} else {
			n = copy(b.buf[b.n:], p)
			b.n += n
			b.Flush()
		}
		...
	}
	...
	n := copy(b.buf[b.n:], p)
```

![Without a buffer every small write crosses into the kernel; bufio collects them and crosses once per 4 KB](images/17-syscalls.webp)

A 20-byte record becomes a `copy` into memory. One syscall now carries ~200 records. `bufio.Reader` and `bufio.Scanner` do the same for reads. Two things to remember:

- **You must call `Flush`.** Whatever is still in the buffer when you stop is lost, with no error and no warning.
- **Errors are sticky.** After a failed write, `b.err` is set and every later `Write` and `Flush` returns it. So checking the error of the final `Flush` is enough, but a `defer w.Flush()` that ignores the error is not.

## The Lab

```
labs/17-io-syscalls/
  before.go          naive implementations (baseline, don't edit)
  after.go           your copy: make this one fast
  solution.go        reference solution (built with -tags solution)
  lab_test.go        correctness test + benchmarks
  cmd/syscallcost/   the syscall cost demo above (Linux only)
  cmd/records/       writes records to a file, for strace
```

| Function | What it does |
|---|---|
| `WriteRecords(f, recs)` | Writes 5,000 CSV lines (`id,name,latency`) to an `*os.File` with `fmt.Fprintf` |
| `CountLines(r)` | Counts the lines of a ~93 KB file, reading **one byte per `Read`** |

The write benchmark writes to `/dev/null`, which still costs a full `write` syscall but no disk I/O. The read benchmark reads a real temp file that stays in the page cache. Both measure syscall overhead, not your SSD.

### Step 1: Measure the baseline

```bash
make bench LAB=17
```

Both columns should be the same (`~`). Note the absolute numbers: almost 1ms to write 93 KB is very slow.

### Step 2: Count the syscalls

Build the `records` program and run it under `strace -c`, which counts syscalls by type (`-f` follows all of the runtime's threads):

```bash
cd labs
go build -o /tmp/records ./17-io-syscalls/cmd/records
strace -c -f -e trace=write /tmp/records -mode=before
```

```
mode=before: wrote 5000 records, file is 93331 bytes (/tmp/records.csv)
% time     seconds  usecs/call     calls    errors syscall
------ ----------- ----------- --------- --------- ----------------
100,00    0,002706           0      5001           write
```

**5,001 `write` calls**: one per record, plus the final message to stderr.

> **Not on Linux?** On macOS, `dtruss` needs System Integrity Protection relaxed, so skip this step and rely on the profile and the benchmark. On Windows, use the profile too.

### Step 3: Find the syscalls in a profile

```bash
go test -run '^$' -bench 'WriteRecords/impl=before' -cpuprofile cpu.prof ./17-io-syscalls/
go tool pprof -top -cum cpu.prof | head -25
```

```
      flat  flat%   sum%        cum   cum%
     0.02s  1.65%  1.65%      1.15s 95.04%  labs/17-io-syscalls.WriteRecordsBefore
     0.03s  2.48%  4.13%      1.01s 83.47%  fmt.Fprintf
     0.01s  0.83%  4.96%      0.66s 54.55%  os.(*File).Write
     0.01s  0.83%  5.79%      0.65s 53.72%  internal/poll.(*FD).Write
     0.01s  0.83%  6.61%      0.56s 46.28%  syscall.write
     0.01s  0.83%  7.44%      0.55s 45.45%  syscall.Syscall
     0.03s  2.48%  9.92%      0.44s 36.36%  syscall.RawSyscall6
     0.41s 33.88% 43.80%      0.41s 33.88%  internal/runtime/syscall/linux.Syscall6
     0.02s  1.65% 48.76%      0.21s 17.36%  fmt.(*pp).printArg
     0.03s  2.48% 57.85%      0.10s  8.26%  runtime.mallocgc
     0.01s  0.83% 67.77%      0.08s  6.61%  runtime.convT64
     0.02s  1.65% 83.47%      0.05s  4.13%  runtime.mallocgcTinySC2
     0.01s  0.83% 85.12%      0.04s  3.31%  runtime.convTstring
```

The chain `os.(*File).Write → internal/poll.(*FD).Write → syscall.Syscall → syscall.RawSyscall6 → internal/runtime/syscall/linux.Syscall6` is the signature of an unbuffered write. The big **flat** time on `Syscall6` is time spent inside the kernel: the CPU profiler measures the thread's CPU time, kernel time included. Also notice `convTstring` and `convT64`: `fmt.Fprintf` boxes every argument into an `any`, and that allocates (see [Interfaces and Boxing](08-interfaces-boxing.md)). `mallocgcTinySC2` is new in Go 1.27: `mallocgc` now dispatches small allocations to size-specialized routines, so expect names like `mallocgcTinySC2` or `mallocgcSmallScanNoHeaderSC2` under it.

### Step 4: Fix it

Edit `labs/17-io-syscalls/after.go`:

1. **`WriteRecordsAfter`**: wrap `f` in `bufio.NewWriter(f)`, write to the buffer, and `return w.Flush()` at the end. Run the benchmark: this alone removes the syscall cost.
2. **Bonus**: replace `fmt.Fprintf` with `strconv.AppendInt` into a `line []byte` that you reuse (`line[:0]`) for every record. This removes the boxing allocations as well.
3. **`CountLinesAfter`**: read into a big buffer (`make([]byte, 32*1024)`) and count with `bytes.Count(buf[:n], []byte{'\n'})`. Wrapping `r` in a `bufio.Reader` and calling `ReadByte` also works, but is slower.

Run `go test ./17-io-syscalls/`: the test compares the files byte by byte, so a missing `Flush` fails it.

### Step 5: Compare

```bash
make bench LAB=17
```

Here is the intermediate step, with only `bufio.NewWriter` added and `fmt.Fprintf` still in place, on an AMD Ryzen AI MAX+ 395:

```
                │   before    │               after                │
                │   sec/op    │   sec/op     vs base               │
WriteRecords-32   885.0µ ± 6%   392.2µ ± 2%  -55.68% (p=0.000 n=8)
```

And the reference solution (`make solution LAB=17`):

```
                │     before      │               after                │
                │     sec/op      │   sec/op     vs base               │
WriteRecords-32      855.89µ ± 2%   86.35µ ± 1%  -89.91% (p=0.000 n=8)
CountLines-32     13431.148µ ± 4%   4.830µ ± 3%  -99.96% (p=0.000 n=8)

                │     before     │                   after                    │
                │      B/op      │      B/op       vs base                    │
WriteRecords-32   154.393Ki ± 0%     4.055Ki ± 0%       -97.37% (p=0.000 n=8)
CountLines-32         1.000 ± 0%   32768.000 ± 0%  +3276700.00% (p=0.000 n=8)

                │     before     │                after                │
                │   allocs/op    │ allocs/op   vs base                 │
WriteRecords-32   14730.000 ± 0%   4.000 ± 0%  -99.97% (p=0.000 n=8)
CountLines-32         1.000 ± 0%   1.000 ± 0%        ~ (p=1.000 n=8) ¹
```

Run `strace` again with your new version:

```bash
go build -o /tmp/records ./17-io-syscalls/cmd/records
strace -c -f -e trace=write /tmp/records -mode=after
```

```
100,00    0,000051           2        24           write
```

**24 writes instead of 5,001**: 22 full 4 KiB buffers, one final `Flush` of the remaining 3,219 bytes, and the stderr message.

## Understanding the Results

**WriteRecords** splits cleanly into two costs. Adding `bufio` cut the time roughly in half (~885µs → ~392µs). That's ~5,000 syscalls × ~100ns. The remaining ~400µs was `fmt`: parsing the format string, boxing three arguments per record (~14,700 allocations), and reflection-based printing. `strconv.AppendInt` into a reused slice removed that, for a total of **~10x**. Only 4 allocations are left: the `bufio.Writer`, its 4 KiB buffer, and the scratch `line` slice growing to fit the longest record.

**CountLines** is the extreme case: 93,331 `read` syscalls of one byte each, ~150ns apiece, against 4 reads of 32 KiB. That's a **~3,000x** difference for the same result. B/op went up from 1 byte to 32 KiB because we allocate the buffer on every call. That's a fine trade here, and you could reuse it across calls if it mattered. Why does the "before" version allocate 1 byte at all? `one` escapes because it is passed to `r.Read` through an interface, and the compiler can't see what the implementation does with it (see [Escape Analysis](05-escape-analysis.md)).

Check the noflush mode of the demo program to see the classic bug:

```bash
/tmp/records -mode=noflush
```

```
mode=noflush: wrote 5000 records, file is 90112 bytes (/tmp/records.csv)
```

90,112 = 22 × 4,096. The last partial buffer, 3,219 bytes or about 170 records, silently vanished. No error, no panic.

> **Rule of thumb:** never do small I/O directly on an `*os.File` or a `net.Conn` in a loop. Put a `bufio.Writer`/`bufio.Reader` in front, size the buffer to your access pattern (`bufio.NewWriterSize`), and always check the error from `Flush`.

## Beyond bufio

- `os.ReadFile` / `io.ReadAll` / `io.Copy` already use big buffers, so prefer them for whole-file work.
- `bufio.Scanner` reads lines with a 64 KiB max token by default (`MaxScanTokenSize`). Longer lines fail with `ErrTooLong` unless you call `Scanner.Buffer`.
- `net/http` already buffers responses (a 2 KiB buffer per response on top of a 4 KiB `bufio.Writer` per connection, in `net/http/server.go`), so wrapping `http.ResponseWriter` again rarely helps.
- `log.Logger` and `slog` handlers write one line per call to their `io.Writer`. If that's `os.Stderr`, each log line is a syscall.
- `writev`-style batching: `net.Buffers` sends several slices with one `writev` on sockets.
- Buffering trades latency and durability for throughput: data in a `bufio.Writer` (or in the kernel page cache!) is lost if the process (or the machine) dies. `f.Sync()` (`fsync`) is the way to make it durable, and it is far more expensive than `write`.

## Key Takeaways

- A syscall costs tens to hundreds of nanoseconds, **20–50x a function call**, before doing any real work
- `entersyscall` marks the goroutine `_Gsyscall`, and `sysmon` hands its P to another thread if the call takes more than ~20µs and there is other work
- Sockets use the netpoller (park, don't block). Regular files are blocking syscalls that hold an M
- `*os.File` is unbuffered: each `Write`, `Fprintf` or `Println` is a syscall
- `bufio` turns thousands of syscalls into a handful. `strace -c` and `syscall.Syscall` in pprof show you when you need it
- Always `Flush`, and check its error

## Going Further (optional)

1. Run `make bench LAB=17` with `bufio.NewWriterSize(f, 64*1024)`. Does a bigger buffer help? When does it stop mattering?
2. Use `go test -bench 'WriteRecords/impl=before' -trace trace.out` and open `go tool trace trace.out`. Find the syscall events in the goroutine view.
3. Run the benchmark under `/usr/bin/time -v go test ...` (or `perf stat -e task-clock,context-switches`). Compare "System time" before and after.
4. Change the write benchmark to a real file in `b.TempDir()` instead of `/dev/null`. How much does the page cache add per `write`?
