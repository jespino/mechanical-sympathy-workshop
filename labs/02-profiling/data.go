package profiling

import (
	"fmt"
	"math/rand/v2"
	"strings"
)

// GenerateLog returns n fake access-log lines like
//
//	2026-10-23T10:00:00Z GET /api/users/4821/orders/17 200 12ms
//
// The output is always the same for a given n.
func GenerateLog(n int) []string {
	r := rand.New(rand.NewPCG(42, 1024))
	routes := []string{
		"/api/users/%d",
		"/api/users/%d/orders",
		"/api/users/%d/orders/%d",
		"/api/products/%d",
		"/api/products/%d/reviews",
		"/api/cart",
		"/api/checkout",
		"/healthz",
	}
	methods := []string{"GET", "GET", "GET", "POST", "PUT", "DELETE"}
	statuses := []string{"200", "200", "200", "200", "201", "404", "500", "503"}

	lines := make([]string, n)
	for i := range lines {
		route := routes[r.IntN(len(routes))]
		var ids []any
		for range strings.Count(route, "%d") {
			ids = append(ids, r.IntN(100_000))
		}
		lines[i] = fmt.Sprintf("2026-10-23T10:%02d:%02dZ %s %s %s %dms",
			i/60%60, i%60,
			methods[r.IntN(len(methods))],
			fmt.Sprintf(route, ids...),
			statuses[r.IntN(len(statuses))],
			1+r.IntN(250),
		)
	}
	return lines
}
