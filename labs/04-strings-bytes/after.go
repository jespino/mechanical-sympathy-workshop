//go:build !solution

package strbytes

// This is your file. It starts as a copy of before.go: change it so that it
// allocates less, then run the benchmarks to compare against before.go.
// The reference solution is in solution.go (run with -tags solution).

// CountTokensAfter counts how many times each token appears.
// TODO: counts[string(t)]++ stores into the map, so Go must create a new
// string key every time, even when the token is already in the map. Only
// the lookup m[string(t)] is free. Find a way to update an existing entry
// with just a lookup (hint: what if the map's values were pointers?).
func CountTokensAfter(tokens [][]byte) map[string]int {
	counts := map[string]int{}
	for _, t := range tokens {
		counts[string(t)]++
	}
	return counts
}
