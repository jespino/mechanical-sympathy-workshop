package profiling

import (
	"slices"
	"testing"
)

const size = 10_000

func TestSameResults(t *testing.T) {
	lines := GenerateLog(size)
	before, after := ReportBefore(lines), ReportAfter(lines)
	if !slices.Equal(before, after) {
		t.Errorf("after differs from before:\nbefore: %v\nafter:  %v", before, after)
	}
}

func TestReport(t *testing.T) {
	lines := []string{
		"2026-10-23T10:00:00Z GET /api/users/12 200 3ms",
		"2026-10-23T10:00:01Z GET /api/users/345 500 9ms",
		"2026-10-23T10:00:02Z POST /api/users/7/orders/88 201 4ms",
		"not a log line",
		"2026-10-23T10:00:03Z GET /healthz 200 1ms",
	}
	want := []EndpointStats{
		{"GET /api/users/:id", 2, 1},
		{"GET /healthz", 1, 0},
		{"POST /api/users/:id/orders/:id", 1, 0},
	}
	for name, report := range map[string]func([]string) []EndpointStats{
		"before": ReportBefore, "after": ReportAfter,
	} {
		if got := report(lines); !slices.Equal(got, want) {
			t.Errorf("%s: got %v, want %v", name, got, want)
		}
	}
}

func BenchmarkReport(b *testing.B) {
	lines := GenerateLog(size)
	for _, impl := range []struct {
		name string
		fn   func([]string) []EndpointStats
	}{{"before", ReportBefore}, {"after", ReportAfter}} {
		b.Run("impl="+impl.name, func(b *testing.B) {
			for b.Loop() {
				impl.fn(lines)
			}
		})
	}
}
