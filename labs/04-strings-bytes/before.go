// Package strbytes is the lab for module 04: strings and bytes.
//
// before.go holds the naive implementations. Don't edit this file: it is the
// baseline that after.go is benchmarked against.
package strbytes

import "fmt"

// Label is a name="value" pair attached to a metric.
type Label struct {
	Name, Value string
}

// Sample is one data point of a time series.
type Sample struct {
	Name      string
	Value     float64
	Timestamp int64 // milliseconds since the Unix epoch
}

// RenderLabelsBefore renders labels as {name1="value1",name2="value2"}.
func RenderLabelsBefore(labels []Label) string {
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

// AppendSamplesBefore appends one "name value timestamp\n" line per sample
// to dst, the text format used by many metrics systems.
func AppendSamplesBefore(dst []byte, samples []Sample) []byte {
	for _, s := range samples {
		line := fmt.Sprintf("%s %g %d\n", s.Name, s.Value, s.Timestamp)
		dst = append(dst, line...)
	}
	return dst
}

// CountTokensBefore counts how many times each token appears. Tokens come
// from a []byte buffer (for example, a file or a network read).
func CountTokensBefore(tokens [][]byte) map[string]int {
	counts := map[string]int{}
	for _, t := range tokens {
		counts[string(t)]++
	}
	return counts
}
