//go:build !solution

package profiling

// This is your file. It starts as a copy of before.go: profile it, find the
// hot spot, and make it fast. The reference solution is in solution.go (run
// with -tags solution).

import (
	"regexp"
	"strings"
)

// ReportAfter parses access-log lines and returns per-endpoint statistics,
// busiest endpoints first. Numeric IDs in paths are replaced by ":id" so that
// /api/users/1 and /api/users/2 count as the same endpoint.
//
// TODO: don't guess. Profile the benchmark (see the module page), find where
// the time goes, and fix that.
func ReportAfter(lines []string) []EndpointStats {
	stats := map[string]*EndpointStats{}
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 5 {
			continue
		}
		method, path, status := fields[1], fields[2], fields[3]

		endpoint := method + " " + normalizePathAfter(path)
		s := stats[endpoint]
		if s == nil {
			s = &EndpointStats{Endpoint: endpoint}
			stats[endpoint] = s
		}
		s.Requests++
		if strings.HasPrefix(status, "5") {
			s.Errors++
		}
	}
	return sortStats(stats)
}

// normalizePathAfter replaces every numeric path segment with ":id".
func normalizePathAfter(path string) string {
	re := regexp.MustCompile(`/[0-9]+`)
	return re.ReplaceAllString(path, "/:id")
}
