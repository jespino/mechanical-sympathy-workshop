package strbytes

import (
	"bytes"
	"maps"
	"testing"
)

func testTokens() [][]byte {
	words := bytes.Fields([]byte("the quick brown fox jumps over the lazy dog while " +
		"the gopher reads the source code of the runtime and the compiler"))
	tokens := make([][]byte, 10_000)
	for i := range tokens {
		tokens[i] = words[i*7%len(words)]
	}
	return tokens
}

func TestSameResults(t *testing.T) {
	tokens := testTokens()
	if !maps.Equal(CountTokensBefore(tokens), CountTokensAfter(tokens)) {
		t.Error("CountTokens: after differs from before")
	}
}

func BenchmarkCountTokens(b *testing.B) {
	tokens := testTokens()
	for _, impl := range []struct {
		name string
		fn   func([][]byte) map[string]int
	}{{"before", CountTokensBefore}, {"after", CountTokensAfter}} {
		b.Run("impl="+impl.name, func(b *testing.B) {
			for b.Loop() {
				impl.fn(tokens)
			}
		})
	}
}
