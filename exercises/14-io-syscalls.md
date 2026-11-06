# I/O and Syscalls

> **Want to learn more?** On Internals for Interns, [The Scheduler](https://internals-for-interns.com/posts/go-runtime-scheduler/#system-calls) explains what the runtime does when a goroutine enters a system call, and [The Network Poller](https://internals-for-interns.com/posts/go-netpoller/) explains how Go keeps a blocking API on top of non-blocking network I/O.

Reading or writing a file asks the operating system to do work through a syscall. Thousands of tiny reads or writes can spend more time on those requests than on useful processing. We will batch them with buffers.

## Learning Objectives

By the end of this module, you will:

- Understand why many small I/O calls are expensive
- Use buffered writes and larger reads
- Count write syscalls with `strace` on Linux
- Handle write errors and flush the final buffer

## The Concept: Crossing Into the Kernel

### Crossing into the operating system

A syscall transfers control to the kernel, which performs an operation and returns. Go also does bookkeeping so other goroutines can continue when a syscall blocks. This is more work than copying a few bytes into memory.

Sockets often let Go wait for readiness without holding an operating-system thread. Regular-file I/O can block a thread. Either way, reducing unnecessary calls can help.

### Batch small writes

`*os.File` does not collect small writes into a Go buffer. A loop that calls `fmt.Fprintf(f, ...)` once per record normally makes a write syscall for every record.

`bufio.Writer` collects those small writes in memory, then sends a larger chunk to the underlying writer. Its default buffer is 4 KiB. Large reads provide the same benefit in the other direction.

![Without a buffer every small write crosses into the kernel; bufio collects them and crosses once per 4 KB](images/14-syscalls.webp)

### Finish the write

Always call `Flush` and check its error. The last part of the output may still be in memory when the loop ends. Closing the underlying file does not flush a separate `bufio.Writer` for you.

Handle errors from writes as well. Flushing sends buffered bytes to the underlying writer; it does not guarantee that file data is durable on disk.

## The Lab

`WriteRecords` writes 5,000 CSV lines with `fmt.Fprintf`. `CountLines` reads a file one byte at a time. The write benchmark uses `/dev/null`; the read benchmark uses a temporary file. We are mainly measuring call overhead, not disk speed.

Run the commands from the repository root.

### Step 1: Measure the baseline

```bash
make bench LAB=14
```

### Step 2: Find the cost

On Linux, count writes with:

```bash
(cd labs && go build -o /tmp/records ./14-io-syscalls/cmd/records)
strace -c -f -e trace=write /tmp/records -mode=before
```

Expect about 5,001 writes: one per record plus the program's final message. If `strace` is unavailable, continue with the benchmarks. The optional `cmd/syscallcost` demo compares individual call costs on Linux.

### Step 3: Fix it

Edit `labs/14-io-syscalls/after.go`. Keep `before.go` as the baseline.

1. In `WriteRecordsAfter`, create `bufio.NewWriter(f)` and write to it instead of directly to `f`. Check write errors and return the final `Flush` error. Benchmark this change first.
2. Then replace `fmt.Fprintf` with `strconv.AppendInt` and `append` into a reused `line []byte`. Reset it with `line[:0]` for each record, keeping the exact CSV format.
3. In `CountLinesAfter`, read blocks into a 32 KiB buffer and count newlines with `bytes.Count(buf[:n], []byte{'\n'})`. Process any bytes returned before handling `io.EOF` or another error.

### Step 4: Check and compare

```bash
(cd labs && go test ./14-io-syscalls/)
make bench LAB=14
```

Reference solution (`make solution LAB=14`), on my machine:

| Benchmark | Before | After |
|---|---|---|
| `WriteRecords` | 855.89 µs | 86.35 µs |
| `WriteRecords` allocations | 14,730 | 4 |
| `CountLines` | 13.431 ms | 4.830 µs |

## Understanding the Results

**WriteRecords** improves in two stages. Buffering removes most small syscalls; direct formatting into a reused slice removes the extra `fmt` work and boxing allocations. Together they make it about 10x faster here.

**CountLines** makes a few large reads instead of tens of thousands of one-byte reads. It uses a larger temporary buffer but does much less coordination with the operating system.

Rebuild `/tmp/records` and run `-mode=after` under `strace`. The reference solution reduces writes from about 5,001 to 24. Correctness tests also catch missing output caused by forgetting `Flush`.

## Key Takeaways

- Batch small file writes with `bufio.Writer`.
- Read blocks instead of one byte at a time.
- Always handle errors and check the final `Flush`.
- Buffering and avoiding formatting allocations are separate improvements.

## Going Further (optional)

1. Try a 64 KiB writer buffer. Does it help beyond the default size?
2. Run `/tmp/records -mode=noflush` and compare the output size.
3. Reuse the read buffer across calls. How do `B/op` and `allocs/op` change?
