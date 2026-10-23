package capstone

import (
	"strconv"
	"time"
)

// Log line format (one per line, fields separated by single spaces):
//
//	2026-10-23T12:00:00.123Z GET /api/users/1234 200 1534us "Mozilla/5.0 ..."
//
// timestamp (RFC 3339, UTC, millisecond precision), method, path (maybe
// with a query string), 3-digit status, latency in microseconds, and the
// quoted user agent. About 0.2% of the lines are malformed.

var (
	genMethods = []string{"GET", "GET", "GET", "GET", "POST", "POST", "PUT", "DELETE"}
	genPaths   = []string{
		"/api/users/{id}",
		"/api/users/{id}/orders",
		"/api/orders/{id}",
		"/api/orders/{id}/items/{id}",
		"/api/products",
		"/api/products/{id}",
		"/api/search?q=shoes&page={id}",
		"/api/cart",
		"/api/cart/items/{id}",
		"/api/login",
		"/api/logout",
		"/healthz",
		"/static/app.js",
		"/static/app.css",
		"/static/img/{id}",
		"/v2/reports/{id}/export",
	}
	genAgents = []string{
		"Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.0.0 Safari/537.36",
		"Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/129.0.0.0 Safari/537.36 Edg/129.0.0.0",
		"Mozilla/5.0 (Macintosh; Intel Mac OS X 14_6) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15",
		"Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:131.0) Gecko/20100101 Firefox/131.0",
		"Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1",
		"Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)",
		"curl/8.9.1",
		"Go-http-client/1.1",
	}
	genStatuses = []int{200, 200, 200, 200, 200, 200, 200, 201, 204, 301, 304, 400, 401, 404, 404, 500, 503}
)

// GenerateLog returns n deterministic log lines generated from seed.
func GenerateLog(n int, seed uint64) []byte {
	x := seed | 1
	next := func() uint64 { // xorshift64
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		return x
	}
	start := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)
	out := make([]byte, 0, n*170)
	ms := int64(0)
	for range n {
		ms += int64(next() % 20)
		line := len(out)
		out = start.Add(time.Duration(ms)*time.Millisecond).AppendFormat(out, "2006-01-02T15:04:05.000Z")
		out = append(out, ' ')
		out = append(out, genMethods[next()%uint64(len(genMethods))]...)
		out = append(out, ' ')
		path := genPaths[next()%uint64(len(genPaths))]
		for i := 0; i < len(path); i++ {
			if path[i] == '{' { // "{id}"
				out = strconv.AppendUint(out, next()%100_000, 10)
				i += 3
				continue
			}
			out = append(out, path[i])
		}
		out = append(out, ' ')
		r := next()
		if r%1000 == 0 {
			out = append(out, '-') // malformed: no status
		} else {
			out = strconv.AppendInt(out, int64(genStatuses[r%uint64(len(genStatuses))]), 10)
		}
		out = append(out, ' ')
		// Log-normal-ish latency: mostly 0.2-5ms, with a long tail.
		lat := 200 + next()%4_800
		if r%20 == 1 {
			lat *= 1 + next()%200
		}
		out = strconv.AppendUint(out, lat, 10)
		out = append(out, "us \""...)
		out = append(out, genAgents[next()%uint64(len(genAgents))]...)
		out = append(out, '"')
		if r%1000 == 7 {
			out = out[:line+int(next()%uint64(len(out)-line))] // malformed: truncated
		}
		out = append(out, '\n')
	}
	return out
}
