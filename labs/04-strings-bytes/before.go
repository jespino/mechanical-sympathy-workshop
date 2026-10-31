// Package strbytes is the lab for module 04: strings and bytes.
//
// before.go holds the naive implementation. Don't edit this file: it is the
// baseline that after.go is benchmarked against.
package strbytes

// CountTokensBefore counts how many times each token appears. Tokens come
// from a []byte buffer (for example, a file or a network read).
func CountTokensBefore(tokens [][]byte) map[string]int {
	counts := map[string]int{}
	for _, t := range tokens {
		counts[string(t)]++
	}
	return counts
}
