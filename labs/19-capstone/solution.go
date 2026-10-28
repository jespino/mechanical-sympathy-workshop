//go:build solution

package capstone

import (
	"bufio"
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The reference solution. Same report, byte for byte, but:
//
//   - one goroutine, no locks: the per-line work is ~100ns, far too small to
//     pay for a goroutine and a contended mutex (modules 13, 16)
//   - the input is streamed through a bufio.Reader and parsed in place as
//     []byte: no ReadAll, no strings.Split, no per-line string (04, 17)
//   - a hand-written parser replaces the regexp and the map[string]any, so
//     nothing is boxed and nothing is allocated per line (04, 05, 08, 14)
//   - timestamps have a fixed layout, so they compare correctly as bytes:
//     time.Parse runs twice per report instead of once per line
//   - the route key is built in a reusable buffer and looked up with
//     m[string(buf)], which doesn't allocate; we only allocate a key the
//     first time we see a route (04)
//   - routes live in a slice of values indexed by the map, and status codes
//     are counted in a fixed array instead of a map per route (03, 09)
//   - user agents are classified once per distinct string, then cached
//   - slices.Sort instead of sort.Slice (08)
//   - the output goes through a bufio.Writer. The report itself still uses
//     fmt: it runs ~70 times per report, so it's not worth optimizing (17)

// tsLayout is the timestamp layout the log uses. Timestamps in this layout
// sort lexicographically in time order.
const tsLayout = "2006-01-02T15:04:05.000Z"

type route struct {
	key       string
	latencies []int
	classes   [10]int // status / 100
}

// Agent families, in the (alphabetical) order the report prints them.
var families = [...]string{"bot", "chrome", "curl", "edge", "firefox", "other", "safari"}

type analyzer struct {
	index  map[string]int32 // route key -> position in routes
	routes []route
	key    []byte // scratch buffer for route keys

	agentCache map[string]uint8 // user agent -> family
	agents     [len(families)]int

	total, bad int

	// Earliest and latest timestamp seen, as raw bytes in tsLayout.
	first, last []byte
	// Timestamps in any other layout that time.Parse accepts (the log
	// never has them, but before.go accepts them, so we do too).
	firstT, lastT time.Time
}

// AnalyzeAfter reads an access log from r and writes a report to w.
func AnalyzeAfter(r io.Reader, w io.Writer) error {
	a := &analyzer{
		index:      make(map[string]int32, 128),
		agentCache: make(map[string]uint8, 16),
	}
	br := bufio.NewReaderSize(r, 64*1024)
	var long []byte // for the rare line longer than the buffer
	for {
		line, err := br.ReadSlice('\n')
		if errors.Is(err, bufio.ErrBufferFull) {
			long = append(long[:0], line...)
			for errors.Is(err, bufio.ErrBufferFull) {
				line, err = br.ReadSlice('\n')
				long = append(long, line...)
			}
			line = long
		}
		line = bytes.TrimSuffix(line, []byte{'\n'})
		if len(line) > 0 {
			a.add(line)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
	}
	return a.writeReport(w)
}

// isSpace reports whether c is in RE2's \s class.
func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\f' || c == '\r'
}

// field returns the bytes before the next ' ', which must be a non-empty
// run of non-space bytes (the regexp's `(\S+) `), and the rest after it.
func field(b []byte) (f, rest []byte, ok bool) {
	for i, c := range b {
		if c == ' ' {
			return b[:i], b[i+1:], i > 0
		}
		if isSpace(c) {
			return nil, nil, false
		}
	}
	return nil, nil, false
}

// parse splits a line exactly like
// `^(\S+) (\S+) (\S+) (\d{3}) (\d+)us "([^"]*)"$`.
func parse(line []byte) (ts, method, path []byte, status int, latency []byte, agent []byte, ok bool) {
	var rest []byte
	if ts, rest, ok = field(line); !ok {
		return
	}
	if method, rest, ok = field(rest); !ok {
		return
	}
	if path, rest, ok = field(rest); !ok {
		return
	}
	ok = false
	if len(rest) < 4 || rest[3] != ' ' {
		return
	}
	for _, c := range rest[:3] {
		if c < '0' || c > '9' {
			return
		}
		status = status*10 + int(c-'0')
	}
	rest = rest[4:]
	i := 0
	for i < len(rest) && rest[i] >= '0' && rest[i] <= '9' {
		i++
	}
	if i == 0 || !bytes.HasPrefix(rest[i:], []byte(`us "`)) {
		return
	}
	latency, rest = rest[:i], rest[i+4:]
	if len(rest) == 0 || rest[len(rest)-1] != '"' {
		return
	}
	agent = rest[:len(rest)-1]
	if bytes.IndexByte(agent, '"') >= 0 {
		return
	}
	return ts, method, path, status, latency, agent, true
}

// canonicalTS reports whether ts is in tsLayout, with plausible digits.
func canonicalTS(ts []byte) bool {
	if len(ts) != len(tsLayout) {
		return false
	}
	for i, c := range ts {
		switch l := tsLayout[i]; {
		case l >= '0' && l <= '9':
			if c < '0' || c > '9' {
				return false
			}
		case c != l:
			return false
		}
	}
	return true
}

func num(b []byte) int {
	n := 0
	for _, c := range b {
		n = n*10 + int(c-'0')
	}
	return n
}

// validDate does the range checks time.Parse would do on a canonical
// timestamp: "2006-01-02T15:04:05.000Z".
func validDate(ts []byte) bool {
	year, month, day := num(ts[0:4]), num(ts[5:7]), num(ts[8:10])
	if month < 1 || month > 12 || day < 1 || num(ts[11:13]) > 23 || num(ts[14:16]) > 59 || num(ts[17:19]) > 59 {
		return false
	}
	days := [...]int{31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}[month-1]
	if month == 2 && year%4 == 0 && (year%100 != 0 || year%400 == 0) {
		days = 29
	}
	return day <= days
}

func (a *analyzer) add(line []byte) {
	a.total++
	ts, method, path, status, latency, agent, ok := parse(line)
	if !ok {
		a.bad++
		return
	}

	// Timestamps in the log's layout compare correctly as bytes, so we only
	// check that the date is valid, and keep the smallest and largest.
	if canonicalTS(ts) {
		if !validDate(ts) {
			a.bad++
			return
		}
		if a.first == nil || bytes.Compare(ts, a.first) < 0 {
			a.first = append(a.first[:0], ts...)
		}
		if a.last == nil || bytes.Compare(ts, a.last) > 0 {
			a.last = append(a.last[:0], ts...)
		}
	} else {
		t, err := time.Parse(time.RFC3339Nano, string(ts))
		if err != nil {
			a.bad++
			return
		}
		if a.firstT.IsZero() || t.Before(a.firstT) {
			a.firstT = t
		}
		if t.After(a.lastT) {
			a.lastT = t
		}
	}

	// Route key: "METHOD /normalized/path", built in a reused buffer.
	k := append(a.key[:0], method...)
	k = append(k, ' ')
	k = appendRoute(k, path)
	a.key = k
	idx, found := a.index[string(k)] // no allocation for the lookup
	if !found {
		idx = int32(len(a.routes))
		key := string(k)
		a.index[key] = idx
		a.routes = append(a.routes, route{key: key})
	}
	rt := &a.routes[idx]
	var lat int
	if len(latency) <= 18 {
		lat = num(latency)
	} else {
		lat, _ = strconv.Atoi(string(latency)) // same result as before.go
	}
	rt.latencies = append(rt.latencies, lat)
	rt.classes[status/100]++

	fam, found := a.agentCache[string(agent)]
	if !found {
		fam = uint8(slices.Index(families[:], agentFamilyOf(string(agent))))
		a.agentCache[string(agent)] = fam
	}
	a.agents[fam]++
}

// appendRoute appends path without its query string, with every segment
// that strconv.Atoi accepts replaced by ":id".
func appendRoute(dst, path []byte) []byte {
	if i := bytes.IndexByte(path, '?'); i >= 0 {
		path = path[:i]
	}
	for first := true; ; first = false {
		if !first {
			dst = append(dst, '/')
		}
		seg := path
		i := bytes.IndexByte(path, '/')
		if i >= 0 {
			seg = path[:i]
		}
		if isNumber(seg) {
			dst = append(dst, ":id"...)
		} else {
			dst = append(dst, seg...)
		}
		if i < 0 {
			return dst
		}
		path = path[i+1:]
	}
}

// isNumber reports whether strconv.Atoi(string(seg)) would succeed.
func isNumber(seg []byte) bool {
	if len(seg) == 0 {
		return false
	}
	if len(seg) > 18 {
		_, err := strconv.Atoi(string(seg)) // rare: let strconv decide
		return err == nil
	}
	digits := seg
	if seg[0] == '+' || seg[0] == '-' {
		digits = seg[1:]
	}
	if len(digits) == 0 {
		return false
	}
	for _, c := range digits {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// agentFamilyOf is before.go's classification. It allocates, but it only
// runs once per distinct user agent.
func agentFamilyOf(ua string) string {
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

func (a *analyzer) window() (first, last time.Time) {
	first, last = a.firstT, a.lastT
	if a.first != nil {
		f, _ := time.Parse(time.RFC3339Nano, string(a.first))
		l, _ := time.Parse(time.RFC3339Nano, string(a.last))
		if first.IsZero() || f.Before(first) {
			first = f
		}
		if l.After(last) {
			last = l
		}
	}
	return first, last
}

func (a *analyzer) writeReport(w io.Writer) error {
	bw := bufio.NewWriter(w)
	first, last := a.window()
	fmt.Fprintf(bw, "lines: %d  malformed: %d  routes: %d\n", a.total, a.bad, len(a.routes))
	fmt.Fprintf(bw, "window: %s .. %s\n\n", first.Format(time.RFC3339Nano), last.Format(time.RFC3339Nano))

	slices.SortFunc(a.routes, func(x, y route) int {
		if c := cmp.Compare(len(y.latencies), len(x.latencies)); c != 0 {
			return c
		}
		return strings.Compare(x.key, y.key)
	})

	fmt.Fprintf(bw, "%-36s %8s %9s %9s %7s %7s %7s %7s\n", "ROUTE", "COUNT", "P50(us)", "P99(us)", "2xx", "3xx", "4xx", "5xx")
	for i := range a.routes {
		rt := &a.routes[i]
		slices.Sort(rt.latencies)
		n := len(rt.latencies)
		fmt.Fprintf(bw, "%-36s %8d %9d %9d %7d %7d %7d %7d\n", rt.key, n,
			rt.latencies[(n-1)*50/100], rt.latencies[(n-1)*99/100],
			rt.classes[2], rt.classes[3], rt.classes[4], rt.classes[5])
	}

	fmt.Fprintf(bw, "\nuser agents:\n")
	for i, f := range families {
		if c := a.agents[i]; c > 0 {
			fmt.Fprintf(bw, "  %-8s %8d %5.1f%%\n", f, c, 100*float64(c)/float64(a.total-a.bad))
		}
	}
	return bw.Flush()
}
