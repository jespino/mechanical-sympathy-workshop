// Package escape is the lab for module 05: escape analysis.
//
// before.go holds the naive implementations. Don't edit this file: it is the
// baseline that after.go is benchmarked against.
package escape

import (
	"fmt"
	"strconv"
	"strings"
)

// Sample is one line of a tiny text metrics format: "name value timestamp".
type Sample struct {
	Name  string
	Value float64
	Time  int64
}

// parseSampleBefore parses a "name value timestamp" line. It returns a
// pointer, so the Sample has to outlive this function's stack frame.
func parseSampleBefore(line string) (*Sample, bool) {
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

// SumBefore parses every line and adds up the values of the valid ones.
func SumBefore(lines []string) float64 {
	total := 0.0
	for _, line := range lines {
		s, ok := parseSampleBefore(line)
		if !ok {
			continue
		}
		total += s.Value
	}
	return total
}

// EncodeBefore appends every sample to dst in the "name value timestamp\n"
// format, and returns the extended buffer.
func EncodeBefore(dst []byte, samples []Sample) []byte {
	for _, s := range samples {
		dst = fmt.Appendf(dst, "%s %g %d\n", s.Name, s.Value, s.Time)
	}
	return dst
}

// normalizeBefore lowercases name and replaces '.' and '-' with '_'.
func normalizeBefore(name string) string {
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

// CountKnownBefore counts how many names are in the known set once
// normalized.
func CountKnownBefore(known map[string]bool, names []string) int {
	n := 0
	for _, name := range names {
		if known[normalizeBefore(name)] {
			n++
		}
	}
	return n
}
