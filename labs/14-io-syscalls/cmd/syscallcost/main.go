//go:build linux

// syscallcost measures how long it takes to cross into the kernel and back,
// compared to a plain function call and to copying the same byte into a
// user-space buffer. It uses Linux syscall numbers, so it only builds there.
package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"syscall"
	"testing"
)

//go:noinline
func nop(b []byte) int { return len(b) }

func main() {
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		panic(err)
	}
	defer devnull.Close()
	one := []byte{'x'}
	buffered := bufio.NewWriter(io.Discard)

	cases := []struct {
		name string
		fn   func()
	}{
		{"function call (noinline)", func() { nop(one) }},
		{"bufio.Writer.Write 1 byte", func() { buffered.Write(one) }},
		{"getpid via RawSyscall", func() { syscall.RawSyscall(syscall.SYS_GETPID, 0, 0, 0) }},
		{"getpid via Syscall (+entersyscall)", func() { syscall.Syscall(syscall.SYS_GETPID, 0, 0, 0) }},
		{"os.File.Write 1 byte to /dev/null", func() { devnull.Write(one) }},
	}
	for _, c := range cases {
		r := testing.Benchmark(func(b *testing.B) {
			for b.Loop() {
				c.fn()
			}
		})
		fmt.Printf("%-36s %8.1f ns/op\n", c.name, float64(r.T.Nanoseconds())/float64(r.N))
	}
}
