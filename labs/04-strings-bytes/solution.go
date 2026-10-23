//go:build solution

package strbytes

import (
	"strconv"
	"strings"
)

// RenderLabelsAfter computes the final length, grows a strings.Builder to
// exactly that size, and writes every piece into it: one allocation, and
// String() hands over the buffer without copying it.
func RenderLabelsAfter(labels []Label) string {
	n := 2 // "{" and "}"
	for _, l := range labels {
		n += len(l.Name) + len(l.Value) + 4 // =, two quotes and a comma
	}
	var sb strings.Builder
	sb.Grow(n)
	sb.WriteByte('{')
	for i, l := range labels {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString(l.Name)
		sb.WriteString(`="`)
		sb.WriteString(l.Value)
		sb.WriteByte('"')
	}
	sb.WriteByte('}')
	return sb.String()
}

// AppendSamplesAfter appends every piece straight into dst. No format string
// to parse, no interfaces, no temporary strings: when dst has enough
// capacity, it doesn't allocate at all.
func AppendSamplesAfter(dst []byte, samples []Sample) []byte {
	for _, s := range samples {
		dst = append(dst, s.Name...)
		dst = append(dst, ' ')
		dst = strconv.AppendFloat(dst, s.Value, 'g', -1, 64)
		dst = append(dst, ' ')
		dst = strconv.AppendInt(dst, s.Timestamp, 10)
		dst = append(dst, '\n')
	}
	return dst
}

// CountTokensAfter stores a pointer to the counter in the map. Updating an
// existing token is then a lookup, m[string(t)], which the compiler does
// without allocating. A string key is only allocated the first time a
// token is seen.
func CountTokensAfter(tokens [][]byte) map[string]int {
	counters := map[string]*int{}
	for _, t := range tokens {
		if c := counters[string(t)]; c != nil {
			*c++
			continue
		}
		c := 1
		counters[string(t)] = &c
	}
	counts := make(map[string]int, len(counters))
	for k, c := range counters {
		counts[k] = *c
	}
	return counts
}
