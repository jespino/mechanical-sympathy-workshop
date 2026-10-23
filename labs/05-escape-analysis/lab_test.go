package escape

import (
	"bytes"
	"fmt"
	"testing"
)

const size = 1_000

func testLines() []string {
	lines := make([]string, size)
	for i := range lines {
		lines[i] = fmt.Sprintf("http_requests_total.host-%d %d.%d %d", i%10, i, i%100, 1700000000+i)
	}
	lines[size/2] = "broken line"
	return lines
}

func testSamples() []Sample {
	samples := make([]Sample, size)
	for i := range samples {
		samples[i] = Sample{Name: fmt.Sprintf("cpu.usage-%d", i%10), Value: float64(i) / 8, Time: 1700000000 + int64(i)}
	}
	return samples
}

func testNames() (map[string]bool, []string) {
	known := map[string]bool{}
	names := make([]string, size)
	for i := range names {
		names[i] = fmt.Sprintf("Service.HTTP-Requests.Duration-Seconds.Bucket-%d", i%50)
		if i%2 == 0 {
			known[fmt.Sprintf("service_http_requests_duration_seconds_bucket_%d", i%50)] = true
		}
	}
	return known, names
}

func TestSameResults(t *testing.T) {
	lines := testLines()
	if b, a := SumBefore(lines), SumAfter(lines); b != a {
		t.Errorf("Sum: before=%v after=%v", b, a)
	}
	samples := testSamples()
	if b, a := EncodeBefore(nil, samples), EncodeAfter(nil, samples); !bytes.Equal(b, a) {
		t.Errorf("Encode: after differs from before")
	}
	known, names := testNames()
	if b, a := CountKnownBefore(known, names), CountKnownAfter(known, names); b != a {
		t.Errorf("CountKnown: before=%d after=%d", b, a)
	}
}

func BenchmarkSum(b *testing.B) {
	lines := testLines()
	for _, impl := range []struct {
		name string
		fn   func([]string) float64
	}{{"before", SumBefore}, {"after", SumAfter}} {
		b.Run("impl="+impl.name, func(b *testing.B) {
			for b.Loop() {
				impl.fn(lines)
			}
		})
	}
}

func BenchmarkEncode(b *testing.B) {
	samples := testSamples()
	for _, impl := range []struct {
		name string
		fn   func([]byte, []Sample) []byte
	}{{"before", EncodeBefore}, {"after", EncodeAfter}} {
		b.Run("impl="+impl.name, func(b *testing.B) {
			buf := impl.fn(nil, samples) // reuse one buffer across iterations
			for b.Loop() {
				buf = impl.fn(buf[:0], samples)
			}
		})
	}
}

func BenchmarkCountKnown(b *testing.B) {
	known, names := testNames()
	for _, impl := range []struct {
		name string
		fn   func(map[string]bool, []string) int
	}{{"before", CountKnownBefore}, {"after", CountKnownAfter}} {
		b.Run("impl="+impl.name, func(b *testing.B) {
			for b.Loop() {
				impl.fn(known, names)
			}
		})
	}
}
