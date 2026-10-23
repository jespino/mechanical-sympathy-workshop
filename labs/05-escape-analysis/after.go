//go:build !solution

package escape

import (
	"fmt"
	"strconv"
	"strings"
)

// This is your file. It starts as a copy of before.go: change it so that it
// allocates less, then run the benchmarks to compare against before.go.
// Use `go build -gcflags=-m ./05-escape-analysis/` to see what escapes.
// The reference solution is in solution.go (run with -tags solution).

// parseSampleAfter parses a "name value timestamp" line.
// TODO: the caller only reads the value. Does it need a *Sample?
func parseSampleAfter(line string) (*Sample, bool) {
	name, rest, ok := strings.Cut(line, " ")
	if !ok {
		return nil, false
	}
	value, ts, ok := strings.Cut(rest, " ")
	if !ok {
		return nil, false
	}
	v, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return nil, false
	}
	t, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return nil, false
	}
	return &Sample{Name: name, Value: v, Time: t}, true
}

// SumAfter parses every line and adds up the values of the valid ones.
func SumAfter(lines []string) float64 {
	total := 0.0
	for _, line := range lines {
		s, ok := parseSampleAfter(line)
		if !ok {
			continue
		}
		total += s.Value
	}
	return total
}

// EncodeAfter appends every sample to dst in the "name value timestamp\n"
// format, and returns the extended buffer.
// TODO: fmt takes its arguments as ...any, and they escape. The strconv
// package has Append* functions that write straight into a []byte.
func EncodeAfter(dst []byte, samples []Sample) []byte {
	for _, s := range samples {
		dst = fmt.Appendf(dst, "%s %g %d\n", s.Name, s.Value, s.Time)
	}
	return dst
}

// normalizeAfter lowercases name and replaces '.' and '-' with '_'.
// TODO: two allocations per call: a make() whose size is unknown at compile
// time, and a string that is returned. Write into a caller-provided buffer
// instead (an AppendX-style helper), backed by a fixed-size array. Copy
// name with a single append and fix it up in place: a per-byte append loop
// costs more than the allocation it saves.
func normalizeAfter(name string) string {
	buf := make([]byte, len(name))
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case 'A' <= c && c <= 'Z':
			c += 'a' - 'A'
		case c == '.' || c == '-':
			c = '_'
		}
		buf[i] = c
	}
	return string(buf)
}

// CountKnownAfter counts how many names are in the known set once
// normalized.
// TODO: a map lookup with m[string(b)] does not allocate.
func CountKnownAfter(known map[string]bool, names []string) int {
	n := 0
	for _, name := range names {
		if known[normalizeAfter(name)] {
			n++
		}
	}
	return n
}
