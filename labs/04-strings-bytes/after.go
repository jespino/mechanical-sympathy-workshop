//go:build !solution

package strbytes

// This is your file. It starts as a copy of before.go: change it so that it
// copies and allocates less, then run the benchmarks to compare against
// before.go. The reference solution is in solution.go (run with -tags
// solution).

import "fmt"

// RenderLabelsAfter renders labels as {name1="value1",name2="value2"}.
// TODO: every += allocates a new string and copies everything so far. Use a
// strings.Builder, and Grow it to the final size first.
func RenderLabelsAfter(labels []Label) string {
	s := "{"
	for i, l := range labels {
		if i > 0 {
			s += ","
		}
		s += l.Name + `="` + l.Value + `"`
	}
	s += "}"
	return s
}

// AppendSamplesAfter appends one "name value timestamp\n" line per sample
// to dst.
// TODO: fmt.Sprintf parses the format, boxes every argument in an interface
// and allocates the result string. Append the parts to dst directly, using
// strconv.AppendFloat (format 'g', precision -1) and strconv.AppendInt.
func AppendSamplesAfter(dst []byte, samples []Sample) []byte {
	for _, s := range samples {
		line := fmt.Sprintf("%s %g %d\n", s.Name, s.Value, s.Timestamp)
		dst = append(dst, line...)
	}
	return dst
}

// CountTokensAfter counts how many times each token appears.
// TODO: counts[string(t)]++ is an assignment, so the compiler must allocate
// a string key every time, even when the token is already in the map. Only
// the lookup m[string(t)] is free. Find a way to update an existing entry
// with just a lookup (hint: what if the map's values were pointers?).
func CountTokensAfter(tokens [][]byte) map[string]int {
	counts := map[string]int{}
	for _, t := range tokens {
		counts[string(t)]++
	}
	return counts
}
