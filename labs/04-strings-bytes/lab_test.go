package strbytes

import (
	"bytes"
	"fmt"
	"maps"
	"testing"
)

func testLabels() []Label {
	return []Label{
		{"__name__", "http_requests_total"},
		{"cluster", "eu-west-1"},
		{"env", "production"},
		{"host", "web-042.example.com"},
		{"instance", "10.0.12.42:8080"},
		{"job", "frontend"},
		{"method", "GET"},
		{"path", "/api/v1/query_range"},
		{"status", "200"},
		{"version", "v1.26.1"},
	}
}

func testSamples() []Sample {
	samples := make([]Sample, 1000)
	for i := range samples {
		samples[i] = Sample{
			Name:      fmt.Sprintf("node_cpu_seconds_total_%d", i%50),
			Value:     float64(i) * 1.25,
			Timestamp: 1_761_200_000_000 + int64(i)*15_000,
		}
	}
	return samples
}

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
	labels := testLabels()
	if b, a := RenderLabelsBefore(labels), RenderLabelsAfter(labels); b != a {
		t.Errorf("RenderLabels: after differs from before:\n%s\n%s", b, a)
	}
	samples := testSamples()
	if b, a := AppendSamplesBefore(nil, samples), AppendSamplesAfter(nil, samples); !bytes.Equal(b, a) {
		t.Errorf("AppendSamples: after differs from before")
	}
	tokens := testTokens()
	if !maps.Equal(CountTokensBefore(tokens), CountTokensAfter(tokens)) {
		t.Error("CountTokens: after differs from before")
	}
}

func BenchmarkRenderLabels(b *testing.B) {
	labels := testLabels()
	for _, impl := range []struct {
		name string
		fn   func([]Label) string
	}{{"before", RenderLabelsBefore}, {"after", RenderLabelsAfter}} {
		b.Run("impl="+impl.name, func(b *testing.B) {
			for b.Loop() {
				impl.fn(labels)
			}
		})
	}
}

func BenchmarkAppendSamples(b *testing.B) {
	samples := testSamples()
	for _, impl := range []struct {
		name string
		fn   func([]byte, []Sample) []byte
	}{{"before", AppendSamplesBefore}, {"after", AppendSamplesAfter}} {
		b.Run("impl="+impl.name, func(b *testing.B) {
			// Reuse one buffer across iterations, like a real exporter would:
			// we want to measure formatting, not the growth of dst.
			buf := impl.fn(nil, samples)
			for b.Loop() {
				buf = impl.fn(buf[:0], samples)
			}
		})
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
