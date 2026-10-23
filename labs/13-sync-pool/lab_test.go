package syncpool

import (
	"bytes"
	"io"
	"runtime"
	"testing"
)

func testEvent() *Event {
	return &Event{
		Time:    1767225600123456789,
		Level:   "info",
		Service: "checkout-api",
		Message: "payment authorized for order, forwarding to fulfillment queue",
		UserID:  4815162342,
		Latency: 12.875,
		Tags:    []string{"region=eu-west-1", "canary=false", "retry=0", "payment=card", "currency=EUR"},
	}
}

func TestSameOutput(t *testing.T) {
	e := testEvent()
	for range 3 { // several times, so the pooled path reuses a buffer
		var before, after bytes.Buffer
		if err := WriteEventBefore(&before, e); err != nil {
			t.Fatal(err)
		}
		if err := WriteEventAfter(&after, e); err != nil {
			t.Fatal(err)
		}
		if before.String() != after.String() {
			t.Fatalf("output differs:\nbefore: %s\nafter:  %s", before.String(), after.String())
		}
	}
}

var impls = []struct {
	name string
	fn   func(io.Writer, *Event) error
}{{"before", WriteEventBefore}, {"after", WriteEventAfter}}

// reportGCs adds a "GCs/Mop" column: garbage collections per million calls.
func reportGCs(b *testing.B, start uint32) {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	b.ReportMetric(float64(ms.NumGC-start)*1e6/float64(b.N), "GCs/Mop")
}

func numGC() uint32 {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return ms.NumGC
}

// BenchmarkWriteEvent calls the function from a single goroutine.
func BenchmarkWriteEvent(b *testing.B) {
	e := testEvent()
	for _, impl := range impls {
		b.Run("impl="+impl.name, func(b *testing.B) {
			b.ReportAllocs()
			start := numGC()
			for b.Loop() {
				impl.fn(io.Discard, e)
			}
			reportGCs(b, start)
		})
	}
}

// BenchmarkWriteEventParallel calls the function from GOMAXPROCS goroutines
// at once, like a busy server would.
func BenchmarkWriteEventParallel(b *testing.B) {
	e := testEvent()
	for _, impl := range impls {
		b.Run("impl="+impl.name, func(b *testing.B) {
			b.ReportAllocs()
			start := numGC()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					impl.fn(io.Discard, e)
				}
			})
			reportGCs(b, start)
		})
	}
}
