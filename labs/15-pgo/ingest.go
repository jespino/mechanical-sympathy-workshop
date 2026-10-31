// Package pgo is the lab for module 15: profile-guided optimization.
//
// Unlike the other labs, there is no after.go to edit. The code stays the
// same and only the build changes: you benchmark this package compiled with
// -pgo=off, then compiled with a CPU profile of itself, and compare.
package pgo

import (
	"errors"
	"sync"
)

// Record is one parsed access-log line.
type Record struct {
	Endpoint uint32
	Status   uint32
	Micros   uint32
}

var errSyntax = errors.New("syntax error")

// parseRecord parses one "endpoint,status,micros\n" line at the start of b
// and returns the record and the number of bytes consumed.
//
// It is slightly too big for the inliner's default budget of 80, so without
// PGO every call is a real call, and the &Record{} it returns escapes to the
// heap: one allocation per line.
func parseRecord(b []byte) (*Record, int, error) {
	r := &Record{}
	field := 0
	var v uint32
	for i, c := range b {
		switch {
		case c >= '0' && c <= '9':
			v = v*10 + uint32(c-'0')
		case c == ',' && field == 0:
			r.Endpoint, v, field = v, 0, 1
		case c == ',' && field == 1:
			r.Status, v, field = v, 0, 2
		case c == '\n' && field == 2:
			r.Micros = v
			return r, i + 1, nil
		default:
			return nil, 0, errSyntax
		}
	}
	return nil, 0, errSyntax
}

// Observer receives every parsed record. The implementation is chosen at run
// time (from configuration, say), so the compiler can't know which Observe
// method a call will reach: it's an indirect call through the itab.
type Observer interface {
	Observe(r Record)
}

// NumEndpoints is the number of distinct endpoints in the log.
const NumEndpoints = 64

// NumBuckets is the number of latency buckets per endpoint.
const NumBuckets = 8

// Histogram is the Observer used in production: it counts requests per
// endpoint and latency bucket, and 5xx errors per endpoint.
type Histogram struct {
	Counts [NumEndpoints][NumBuckets]uint32
	Errors [NumEndpoints]uint32
}

func (h *Histogram) Observe(r Record) {
	e := r.Endpoint % NumEndpoints
	b := 0
	switch {
	case r.Micros < 500:
		b = 0
	case r.Micros < 1_000:
		b = 1
	case r.Micros < 2_500:
		b = 2
	case r.Micros < 5_000:
		b = 3
	case r.Micros < 10_000:
		b = 4
	case r.Micros < 100_000:
		b = 5
	case r.Micros < 1_000_000:
		b = 6
	default:
		b = 7
	}
	h.Counts[e][b]++
	if r.Status >= 500 {
		h.Errors[e]++
	}
}

// Counter is another Observer, used in tests and for debugging.
type Counter struct{ N int }

func (c *Counter) Observe(Record) { c.N++ }

// Aggregator feeds log data to an Observer. It may be shared by several
// goroutines, so it takes a lock, once per batch rather than once per line.
type Aggregator struct {
	mu  sync.Mutex
	obs Observer
}

// NewAggregator returns an Aggregator that reports to obs.
func NewAggregator(obs Observer) *Aggregator {
	return &Aggregator{obs: obs}
}

// Ingest parses a batch of log lines and returns how many it parsed.
func (a *Aggregator) Ingest(data []byte) (int, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := 0
	for len(data) > 0 {
		r, k, err := parseRecord(data)
		if err != nil {
			return n, err
		}
		data = data[k:]
		a.obs.Observe(*r)
		n++
	}
	return n, nil
}

// GenerateLog returns n deterministic log lines.
func GenerateLog(n int) []byte {
	out := make([]byte, 0, n*16)
	x := uint64(0x9E3779B97F4A7C15)
	for range n {
		// xorshift64: cheap, deterministic, good enough for test data.
		x ^= x << 13
		x ^= x >> 7
		x ^= x << 17
		status := uint64(200)
		if x%100 == 0 {
			status = 503
		}
		micros := 300 + (x>>8)%4_000 // most requests take 0.3-4.3ms
		if x%50 == 1 {
			micros <<= (x >> 32) % 10 // and a few are much slower
		}
		out = appendUint(out, (x>>40)%NumEndpoints, ',')
		out = appendUint(out, status, ',')
		out = appendUint(out, micros, '\n')
	}
	return out
}

func appendUint(b []byte, v uint64, sep byte) []byte {
	var tmp [20]byte
	i := len(tmp)
	for {
		i--
		tmp[i] = byte('0' + v%10)
		v /= 10
		if v == 0 {
			break
		}
	}
	b = append(b, tmp[i:]...)
	return append(b, sep)
}
