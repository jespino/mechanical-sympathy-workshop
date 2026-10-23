//go:build solution

package profiling

import (
	"regexp"
	"strings"
)

// ReportAfter is the same as ReportBefore. The only change is in
// normalizePathAfter.
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

// idSegment is compiled once, when the package is initialized. A compiled
// *regexp.Regexp is safe for concurrent use.
var idSegment = regexp.MustCompile(`/[0-9]+`)

// normalizePathAfter replaces every numeric path segment with ":id".
func normalizePathAfter(path string) string {
	return idSegment.ReplaceAllString(path, "/:id")
}
