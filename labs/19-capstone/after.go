//go:build !solution

package capstone

// This is your file. It starts as a copy of before.go (with every name
// suffixed with After): make it fast, one measured step at a time, and keep
// the report byte-for-byte identical (go test checks it).
// The reference solution is in solution.go (run with -tags solution).
//
// Start with a profile, not with this list. But here is the workshop's
// checklist of things that look suspicious in this file:
//
// TODO: one goroutine per line, all fighting over one mutex (modules 15, 16)
// TODO: a regexp and a map[string]any per line (modules 04, 08, 14)
// TODO: time.Parse on every line, just to find the first and last timestamp
// TODO: io.ReadAll + strings.Split(string(data)): two full copies (04)
// TODO: strings.Split/Join/ToLower and fmt.Sprintf to build keys (04)
// TODO: growing slices and maps from empty (03)
// TODO: sort.Slice vs slices.Sort (08)
// TODO: w may be an *os.File: is the output buffered? (17)

import (
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var lineREAfter = regexp.MustCompile(`^(\S+) (\S+) (\S+) (\d{3}) (\d+)us "([^"]*)"$`)

type routeStatsAfter struct {
	count     int
	latencies []int
	statuses  map[int]int
}

// parseLineAfter turns a log line into a generic record, or nil if it is malformed.
func parseLineAfter(line string) map[string]any {
	m := lineREAfter.FindStringSubmatch(line)
	if m == nil {
		return nil
	}
	ts, err := time.Parse(time.RFC3339Nano, m[1])
	if err != nil {
		return nil
	}
	status, _ := strconv.Atoi(m[4])
	latency, _ := strconv.Atoi(m[5])
	return map[string]any{
		"time":    ts,
		"method":  m[2],
		"path":    m[3],
		"status":  status,
		"latency": latency,
		"agent":   m[6],
	}
}

// normalizeRouteAfter drops the query string and replaces numeric path segments
// with ":id", so /api/users/42?x=1 becomes /api/users/:id.
func normalizeRouteAfter(path string) string {
	path = strings.SplitN(path, "?", 2)[0]
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if _, err := strconv.Atoi(p); err == nil {
			parts[i] = ":id"
		}
	}
	return strings.Join(parts, "/")
}

// agentFamilyAfter classifies a user agent.
func agentFamilyAfter(ua string) string {
	ua = strings.ToLower(ua)
	switch {
	case strings.Contains(ua, "bot"):
		return "bot"
	case strings.Contains(ua, "firefox"):
		return "firefox"
	case strings.Contains(ua, "edg/"):
		return "edge"
	case strings.Contains(ua, "chrome"):
		return "chrome"
	case strings.Contains(ua, "safari"):
		return "safari"
	case strings.Contains(ua, "curl"):
		return "curl"
	}
	return "other"
}

// AnalyzeAfter reads an access log from r and writes a report to w.
func AnalyzeAfter(r io.Reader, w io.Writer) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	var (
		mu          sync.Mutex
		wg          sync.WaitGroup
		routes      = map[string]*routeStatsAfter{}
		agents      = map[string]int{}
		total, bad  int
		first, last time.Time
	)
	// Every line is processed in its own goroutine, to use all the cores.
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		wg.Add(1)
		go func(line string) {
			defer wg.Done()
			rec := parseLineAfter(line)
			mu.Lock()
			defer mu.Unlock()
			total++
			if rec == nil {
				bad++
				return
			}
			ts := rec["time"].(time.Time)
			if first.IsZero() || ts.Before(first) {
				first = ts
			}
			if ts.After(last) {
				last = ts
			}
			key := fmt.Sprintf("%s %s", rec["method"], normalizeRouteAfter(rec["path"].(string)))
			rs, ok := routes[key]
			if !ok {
				rs = &routeStatsAfter{statuses: map[int]int{}}
				routes[key] = rs
			}
			rs.count++
			rs.latencies = append(rs.latencies, rec["latency"].(int))
			rs.statuses[rec["status"].(int)]++
			agents[agentFamilyAfter(rec["agent"].(string))]++
		}(line)
	}
	wg.Wait()

	return writeReportAfter(w, routes, agents, total, bad, first, last)
}

func writeReportAfter(w io.Writer, routes map[string]*routeStatsAfter, agents map[string]int, total, bad int, first, last time.Time) error {
	fmt.Fprintf(w, "lines: %d  malformed: %d  routes: %d\n", total, bad, len(routes))
	fmt.Fprintf(w, "window: %s .. %s\n\n", first.Format(time.RFC3339Nano), last.Format(time.RFC3339Nano))

	var keys []string
	for k := range routes {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if routes[keys[i]].count != routes[keys[j]].count {
			return routes[keys[i]].count > routes[keys[j]].count
		}
		return keys[i] < keys[j]
	})

	fmt.Fprintf(w, "%-36s %8s %9s %9s %7s %7s %7s %7s\n", "ROUTE", "COUNT", "P50(us)", "P99(us)", "2xx", "3xx", "4xx", "5xx")
	for _, k := range keys {
		rs := routes[k]
		sort.Slice(rs.latencies, func(i, j int) bool { return rs.latencies[i] < rs.latencies[j] })
		n := len(rs.latencies)
		var classes [10]int
		for status, c := range rs.statuses {
			classes[status/100] += c
		}
		fmt.Fprintf(w, "%-36s %8d %9d %9d %7d %7d %7d %7d\n", k, rs.count,
			rs.latencies[(n-1)*50/100], rs.latencies[(n-1)*99/100],
			classes[2], classes[3], classes[4], classes[5])
	}

	fmt.Fprintf(w, "\nuser agents:\n")
	var families []string
	for f := range agents {
		families = append(families, f)
	}
	sort.Strings(families)
	for _, f := range families {
		fmt.Fprintf(w, "  %-8s %8d %5.1f%%\n", f, agents[f], 100*float64(agents[f])/float64(total-bad))
	}
	return nil
}
