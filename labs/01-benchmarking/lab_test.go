package benchmarking

import (
	"math/bits"
	"math/rand/v2"
	"testing"
)

// randomInts returns n pseudo-random ints, always the same ones.
func randomInts(n int) []int {
	r := rand.New(rand.NewPCG(1, 2))
	out := make([]int, n)
	for i := range out {
		out[i] = r.Int()
	}
	return out
}

func TestPopCount(t *testing.T) {
	for _, x := range []uint64{0, 1, 0xdeadbeef, 1<<64 - 1, 0x8000000000000001} {
		if got, want := PopCount(x), bits.OnesCount64(x); got != want {
			t.Errorf("PopCount(%#x) = %d, want %d", x, got, want)
		}
	}
}

func TestMix(t *testing.T) {
	// Known SplitMix64 outputs: the generator returns Mix(seed + k*golden).
	const golden = 0x9e3779b97f4a7c15
	for i, want := range []uint64{0xe220a8397b1dcdaf, 0x6e789e6aa1b965f4, 0x06c45d188009454f} {
		if got := Mix(uint64(i+1) * golden); got != want {
			t.Errorf("Mix(%d*golden) = %#x, want %#x", i+1, got, want)
		}
	}
}

// The benchmarks below run the broken version (before_test.go) and your
// version (after_test.go) as sub-benchmarks, so benchstat can put them side
// by side with -col /impl.

func BenchmarkPopCount(b *testing.B) {
	b.Run("impl=before", benchPopCountBefore)
	b.Run("impl=after", benchPopCountAfter)
}

func BenchmarkMix(b *testing.B) {
	b.Run("impl=before", benchMixBefore)
	b.Run("impl=after", benchMixAfter)
}

func BenchmarkSort(b *testing.B) {
	b.Run("impl=before", benchSortBefore)
	b.Run("impl=after", benchSortAfter)
}
