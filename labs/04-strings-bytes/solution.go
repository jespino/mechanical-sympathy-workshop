//go:build solution

package strbytes

// CountTokensAfter stores a pointer to the counter in the map. Updating an
// existing token is then a lookup, m[string(t)], which doesn't allocate.
// A string key is only created the first time a token is seen.
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
