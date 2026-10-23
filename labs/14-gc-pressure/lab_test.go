package gcpressure

import (
	"runtime"
	"runtime/metrics"
	"testing"
)

// store is what both implementations have in common.
type store interface {
	Len() int
	Lookup(id int64) (name string, score float64, ok bool)
	AverageScore(minAge int32) float64
}

var impls = []struct {
	name string
	new  func(n int) store
}{
	{"before", func(n int) store { return NewStoreBefore(n) }},
	{"after", func(n int) store { return NewStoreAfter(n) }},
}

func TestSameResults(t *testing.T) {
	const n = 2000
	before, after := NewStoreBefore(n), NewStoreAfter(n)
	if before.Len() != after.Len() {
		t.Fatalf("Len: before=%d after=%d", before.Len(), after.Len())
	}
	for i := range n + 10 { // a few IDs past the end must be missing in both
		id := recordID(i)
		n1, s1, ok1 := before.Lookup(id)
		n2, s2, ok2 := after.Lookup(id)
		if n1 != n2 || s1 != s2 || ok1 != ok2 {
			t.Fatalf("Lookup(%d): before=(%q,%v,%v) after=(%q,%v,%v)", id, n1, s1, ok1, n2, s2, ok2)
		}
	}
	for _, age := range []int32{0, 30, 60, 100} {
		if a, b := before.AverageScore(age), after.AverageScore(age); a != b {
			t.Fatalf("AverageScore(%d): before=%v after=%v", age, a, b)
		}
	}
}

// benchRecords is the size of the store in the benchmarks: big enough that
// the GC's work is dominated by it.
const benchRecords = 2_000_000

// BenchmarkGC measures one full, forced garbage collection while the store
// is alive. That is the cost the GC pays on *every* cycle, no matter who
// triggered it.
func BenchmarkGC(b *testing.B) {
	for _, impl := range impls {
		b.Run("impl="+impl.name, func(b *testing.B) {
			s := impl.new(benchRecords)
			runtime.GC()
			for b.Loop() {
				runtime.GC()
			}
			runtime.KeepAlive(s)
		})
	}
}

var sink []byte

// BenchmarkChurn simulates request handling next to the store: every op
// allocates some short-lived garbage, which keeps triggering GC cycles,
// and each cycle has to mark the (unchanged) store again.
// gc-cpu-ns/op is the CPU time the GC spent, divided by the number of ops.
func BenchmarkChurn(b *testing.B) {
	for _, impl := range impls {
		b.Run("impl="+impl.name, func(b *testing.B) {
			s := impl.new(benchRecords)
			runtime.GC()
			startGC := gcCPUSeconds()
			for b.Loop() {
				sink = make([]byte, 16<<10)
			}
			b.ReportMetric((gcCPUSeconds()-startGC)*1e9/float64(b.N), "gc-cpu-ns/op")
			runtime.KeepAlive(s)
		})
	}
}

// BenchmarkAverageScore walks every record once.
func BenchmarkAverageScore(b *testing.B) {
	for _, impl := range impls {
		b.Run("impl="+impl.name, func(b *testing.B) {
			s := impl.new(benchRecords)
			runtime.GC()
			for b.Loop() {
				s.AverageScore(40)
			}
		})
	}
}

func gcCPUSeconds() float64 {
	sample := []metrics.Sample{{Name: "/cpu/classes/gc/total:cpu-seconds"}}
	metrics.Read(sample)
	return sample[0].Value.Float64()
}
