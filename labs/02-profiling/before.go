// Package profiling is the lab for module 02: profiling.
//
// before.go holds the slow implementation. Don't edit this file: it is the
// baseline that after.go is benchmarked against.
package profiling

import (
	"regexp"
	"slices"
	"strings"
)

// EndpointStats is one row of the report.
type EndpointStats struct {
	Endpoint string // method and normalized path, e.g. "GET /api/users/:id"
	Requests int
	Errors   int // responses with a 5xx status
}

// ReportBefore parses access-log lines and returns per-endpoint statistics,
// busiest endpoints first. Numeric IDs in paths are replaced by ":id" so that
// /api/users/1 and /api/users/2 count as the same endpoint.
func ReportBefore(lines []string) []EndpointStats {
	stats := map[string]*EndpointStats{}
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) != 5 {
			continue
		}
		method, path, status := fields[1], fields[2], fields[3]

		endpoint := method + " " + normalizePathBefore(path)
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

// normalizePathBefore replaces every numeric path segment with ":id".
func normalizePathBefore(path string) string {
	re := regexp.MustCompile(`/[0-9]+`)
	return re.ReplaceAllString(path, "/:id")
}

// sortStats flattens the map into a slice, busiest endpoints first.
func sortStats(stats map[string]*EndpointStats) []EndpointStats {
	out := make([]EndpointStats, 0, len(stats))
	for _, s := range stats {
		out = append(out, *s)
	}
	slices.SortFunc(out, func(a, b EndpointStats) int {
		if a.Requests != b.Requests {
			return b.Requests - a.Requests
		}
		return strings.Compare(a.Endpoint, b.Endpoint)
	})
	return out
}
