package branchprediction

import (
	"math/rand/v2"
	"slices"
	"testing"
)

const (
	size      = 1 << 20 // 4 MB of int32: too long a pattern for the predictor to memorize
	threshold = 128
)

// testData returns product prices uniformly distributed in [0, 256): with
// threshold 128, each comparison is a coin flip.
func testData(n int) []int32 {
	r := rand.New(rand.NewPCG(1, 2))
	d := make([]int32, n)
	for i := range d {
		d[i] = int32(r.IntN(256))
	}
	return d
}

func TestSameResults(t *testing.T) {
	d := testData(10_000)
	s1, c1 := SumAboveBefore(d, threshold)
	s2, c2 := SumAboveAfter(d, threshold)
	if s1 != s2 || c1 != c2 {
		t.Errorf("SumAbove: before=(%d,%d) after=(%d,%d)", s1, c1, s2, c2)
	}
	if s3 := SumAboveSimple(d, threshold); s3 != s1 {
		t.Errorf("SumAboveSimple = %d, want %d", s3, s1)
	}
	o1, o2 := make([]int32, len(d)), make([]int32, len(d))
	n1, n2 := FilterBefore(d, o1, threshold), FilterAfter(d, o2, threshold)
	if !slices.Equal(o1[:n1], o2[:n2]) {
		t.Error("Filter: after differs from before")
	}
}

// datasets returns the same values twice: in random order, and sorted.
func datasets() []struct {
	name string
	d    []int32
} {
	random := testData(size)
	sorted := slices.Clone(random)
	slices.Sort(sorted)
	return []struct {
		name string
		d    []int32
	}{{"random", random}, {"sorted", sorted}}
}

var sink int64

func BenchmarkSumAbove(b *testing.B) {
	for _, ds := range datasets() {
		for _, impl := range []struct {
			name string
			fn   func([]int32, int32) (int64, int)
		}{{"before", SumAboveBefore}, {"after", SumAboveAfter}} {
			b.Run("data="+ds.name+"/impl="+impl.name, func(b *testing.B) {
				for b.Loop() {
					s, c := impl.fn(ds.d, threshold)
					sink += s + int64(c)
				}
			})
		}
	}
}

func BenchmarkFilter(b *testing.B) {
	out := make([]int32, size)
	for _, ds := range datasets() {
		for _, impl := range []struct {
			name string
			fn   func([]int32, []int32, int32) int
		}{{"before", FilterBefore}, {"after", FilterAfter}} {
			b.Run("data="+ds.name+"/impl="+impl.name, func(b *testing.B) {
				for b.Loop() {
					sink += int64(impl.fn(ds.d, out, threshold))
				}
			})
		}
	}
}
