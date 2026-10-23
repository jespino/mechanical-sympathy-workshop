//go:build solution

package escape

import (
	"strconv"
	"strings"
)

// parseSampleAfter returns the Sample by value: it is copied into the
// caller's frame, and nothing has to live on the heap.
func parseSampleAfter(line string) (Sample, bool) {
	name, rest, ok := strings.Cut(line, " ")
	if !ok {
		return Sample{}, false
	}
	value, ts, ok := strings.Cut(rest, " ")
	if !ok {
		return Sample{}, false
	}
	v, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return Sample{}, false
	}
	t, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return Sample{}, false
	}
	return Sample{Name: name, Value: v, Time: t}, true
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

// EncodeAfter uses the strconv Append functions, which take concrete types
// and write straight into dst: no interface boxing, nothing escapes.
func EncodeAfter(dst []byte, samples []Sample) []byte {
	for _, s := range samples {
		dst = append(dst, s.Name...)
		dst = append(dst, ' ')
		dst = strconv.AppendFloat(dst, s.Value, 'g', -1, 64)
		dst = append(dst, ' ')
		dst = strconv.AppendInt(dst, s.Time, 10)
		dst = append(dst, '\n')
	}
	return dst
}

// appendNormalized appends the normalized form of name to dst.
// It copies name with one append, then rewrites the copy in place: one
// capacity check per name instead of one per byte.
func appendNormalized(dst []byte, name string) []byte {
	n := len(dst)
	dst = append(dst, name...)
	buf := dst[n:]
	for i, c := range buf {
		switch {
		case 'A' <= c && c <= 'Z':
			c += 'a' - 'A'
		case c == '.' || c == '-':
			c = '_'
		}
		buf[i] = c
	}
	return dst
}

// CountKnownAfter normalizes into a fixed-size array that lives on the stack
// (append only moves to the heap if a name is longer than 128 bytes), and
// looks it up with m[string(b)], which the compiler does without allocating.
func CountKnownAfter(known map[string]bool, names []string) int {
	var scratch [128]byte
	n := 0
	for _, name := range names {
		key := appendNormalized(scratch[:0], name)
		if known[string(key)] {
			n++
		}
	}
	return n
}
