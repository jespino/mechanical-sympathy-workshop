// conversions measures which string <-> []byte conversions copy and
// allocate, and which ones the compiler turns into free reinterpretations.
//
//	go run ./04-strings-bytes/cmd/conversions
//
// Each case runs as a benchmark through testing.Benchmark. The code under
// test lives in small //go:noinline functions, so what we measure is the
// conversion itself, not side effects of the benchmark loop.
package main

import (
	"fmt"
	"testing"
)

var (
	b      = []byte("hello, gopher, this is longer than 32 bytes")
	s      = string(b)
	index  = map[string]int{s: 1}
	sinkS  string
	sinkN  int
	counts = map[string]int{}
)

//go:noinline
func kept(b []byte) { sinkS = string(b) }

//go:noinline
func mapLookup(b []byte) int { return index[string(b)] }

//go:noinline
func mapIncrement(b []byte) { counts[string(b)]++ }

//go:noinline
func compare(b []byte) bool { return string(b) == "hello, gopher, this is longer than 32 bytes" }

//go:noinline
func switchOn(b []byte) int {
	switch string(b) {
	case "hello":
		return 1
	case "hello, gopher, this is longer than 32 bytes":
		return 2
	}
	return 0
}

//go:noinline
func rangeBytes(s string) (n int) {
	for _, c := range []byte(s) {
		n += int(c)
	}
	return n
}

//go:noinline
func readOnlyBytes(s string) int { bs := []byte(s); return int(bs[5]) + len(bs) }

//go:noinline
func modifiedBytes(s string) int { bs := []byte(s); bs[0] = 'H'; return int(bs[0]) }

func main() {
	cases := []struct {
		name string
		fn   func()
	}{
		{"string(b), result kept", func() { kept(b) }},
		{"m[string(b)] lookup", func() { sinkN += mapLookup(b) }},
		{"m[string(b)]++", func() { mapIncrement(b) }},
		{`string(b) == "..."`, func() {
			if compare(b) {
				sinkN++
			}
		}},
		{"switch string(b)", func() { sinkN += switchOn(b) }},
		{"for range []byte(s)", func() { sinkN += rangeBytes(s) }},
		{"[]byte(s), read only", func() { sinkN += readOnlyBytes(s) }},
		{"[]byte(s), modified", func() { sinkN += modifiedBytes(s) }},
	}
	fmt.Printf("%-24s %10s %8s %10s\n", "case", "ns/op", "B/op", "allocs/op")
	for _, c := range cases {
		r := testing.Benchmark(func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				c.fn()
			}
		})
		fmt.Printf("%-24s %10.2f %8d %10d\n", c.name,
			float64(r.T.Nanoseconds())/float64(r.N), r.AllocedBytesPerOp(), r.AllocsPerOp())
	}
}
