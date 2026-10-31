package inlining

import "testing"

func add(a, b int) int { return a + b }

//go:noinline
func addNoInline(a, b int) int { return a + b }

var addSink int

// BenchmarkAdd compares the same addition with and without a function call.
// Each result feeds the next addition, and the final result is kept so the
// compiler cannot discard the work. Each helper is called directly: using a
// function variable here would introduce another indirect call.
func BenchmarkAdd(b *testing.B) {
	b.Run("impl=before", func(b *testing.B) {
		total := 0
		for i := 0; i < b.N; i++ {
			total = addNoInline(total, i)
		}
		addSink = total
	})
	b.Run("impl=after", func(b *testing.B) {
		total := 0
		for i := 0; i < b.N; i++ {
			total = add(total, i)
		}
		addSink = total
	})
}
