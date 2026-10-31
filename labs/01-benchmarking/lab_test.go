package benchmarking

import "testing"

const size = 10_000

func testNums() []int {
	nums := make([]int, size)
	for i := range nums {
		nums[i] = i * 7
	}
	return nums
}

func TestSameResults(t *testing.T) {
	nums := testNums()
	sumB, avgB := StatsBefore(nums)
	sumA, avgA := StatsAfter(nums)
	if sumA != sumB || avgA != avgB {
		t.Errorf("Stats: after = (%d, %d), before = (%d, %d)", sumA, avgA, sumB, avgB)
	}
}

func BenchmarkStats(b *testing.B) {
	nums := testNums()
	for _, impl := range []struct {
		name string
		fn   func([]int) (int, int)
	}{{"before", StatsBefore}, {"after", StatsAfter}} {
		b.Run("impl="+impl.name, func(b *testing.B) {
			for b.Loop() {
				impl.fn(nums)
			}
		})
	}
}
