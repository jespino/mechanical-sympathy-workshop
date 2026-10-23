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
	"bytes"
	"fmt"
	"testing"
)

var (
	short  = []byte("hello, gopher")                               // 13 bytes
	long   = []byte("hello, gopher, this is longer than 32 bytes") // 44 bytes
	longS  = string(long)
	index  = map[string]int{longS: 1}
	sinkS  string
	sinkB  []byte
	sinkN  int
	counts = map[string]int{}
)

//go:noinline
func escapes(b []byte) { sinkS = string(b) }

//go:noinline
func localShort(b []byte) int { s := string(b); return len(s) + int(s[3]) }

//go:noinline
func localLong(b []byte) int { s := string(b); return len(s) + int(s[3]) }

//go:noinline
func oneByte(b []byte) { sinkS = string(b[:1]) }

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
func concatWithLiteral(b []byte) { sinkS = "<" + string(b) + ">" }

//go:noinline
func concatNoLiteral(b []byte) { sinkS = longS + string(b) }

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
func mutatedBytes(s string) int { bs := []byte(s); bs[0] = 'H'; return int(bs[0]) }

//go:noinline
func toAssembly(s string) int { return bytes.IndexByte([]byte(s), 'z') }

//go:noinline
func bytesEscape(s string) { sinkB = []byte(s) }

func main() {
	cases := []struct {
		name string
		fn   func()
	}{
		{"string(b), result escapes", func() { escapes(long) }},
		{"string(b), local, 13 bytes", func() { sinkN += localShort(short) }},
		{"string(b), local, 44 bytes", func() { sinkN += localLong(long) }},
		{"string(b), 1 byte", func() { oneByte(long) }},
		{"m[string(b)] lookup", func() { sinkN += mapLookup(long) }},
		{"m[string(b)]++", func() { mapIncrement(long) }},
		{`string(b) == "..."`, func() {
			if compare(long) {
				sinkN++
			}
		}},
		{"switch string(b)", func() { sinkN += switchOn(long) }},
		{`"<" + string(b) + ">"`, func() { concatWithLiteral(long) }},
		{"s + string(b)", func() { concatNoLiteral(long) }},
		{"for range []byte(s)", func() { sinkN += rangeBytes(longS) }},
		{"[]byte(s), read only", func() { sinkN += readOnlyBytes(longS) }},
		{"[]byte(s), mutated", func() { sinkN += mutatedBytes(longS) }},
		{"bytes.IndexByte([]byte(s))", func() { sinkN += toAssembly(longS) }},
		{"[]byte(s), result escapes", func() { bytesEscape(longS) }},
	}
	fmt.Printf("%-30s %10s %8s %10s\n", "case", "ns/op", "B/op", "allocs/op")
	for _, c := range cases {
		r := testing.Benchmark(func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				c.fn()
			}
		})
		fmt.Printf("%-30s %10.2f %8d %10d\n", c.name,
			float64(r.T.Nanoseconds())/float64(r.N), r.AllocedBytesPerOp(), r.AllocsPerOp())
	}
}
