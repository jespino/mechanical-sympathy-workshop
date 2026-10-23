package preallocation

import (
	"fmt"
	"maps"
	"slices"
	"testing"
)

const size = 10_000

func testNums() []int {
	nums := make([]int, size)
	for i := range nums {
		nums[i] = i * 7
	}
	return nums
}

func testWords() []string {
	words := make([]string, size)
	for i := range words {
		words[i] = fmt.Sprintf("word-%d", i%(size/2))
	}
	return words
}

func TestSameResults(t *testing.T) {
	if !slices.Equal(SquaresBefore(size), SquaresAfter(size)) {
		t.Error("Squares: after differs from before")
	}
	nums := testNums()
	if !slices.Equal(EvensBefore(nums), EvensAfter(nums)) {
		t.Error("Evens: after differs from before")
	}
	words := testWords()
	if !maps.Equal(CountWordsBefore(words), CountWordsAfter(words)) {
		t.Error("CountWords: after differs from before")
	}
}

func BenchmarkSquares(b *testing.B) {
	for _, impl := range []struct {
		name string
		fn   func(int) []int
	}{{"before", SquaresBefore}, {"after", SquaresAfter}} {
		b.Run("impl="+impl.name, func(b *testing.B) {
			for b.Loop() {
				impl.fn(size)
			}
		})
	}
}

func BenchmarkEvens(b *testing.B) {
	nums := testNums()
	for _, impl := range []struct {
		name string
		fn   func([]int) []int
	}{{"before", EvensBefore}, {"after", EvensAfter}} {
		b.Run("impl="+impl.name, func(b *testing.B) {
			for b.Loop() {
				impl.fn(nums)
			}
		})
	}
}

func BenchmarkCountWords(b *testing.B) {
	words := testWords()
	for _, impl := range []struct {
		name string
		fn   func([]string) map[string]int
	}{{"before", CountWordsBefore}, {"after", CountWordsAfter}} {
		b.Run("impl="+impl.name, func(b *testing.B) {
			for b.Loop() {
				impl.fn(words)
			}
		})
	}
}
