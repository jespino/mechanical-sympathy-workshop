package ifaces

import (
	"testing"
	"time"
)

const size = 1_000

func testRequests() []Request {
	reqs := make([]Request, size)
	paths := []string{"/api/v1/query", "/api/v1/write", "/metrics", "/health"}
	for i := range reqs {
		reqs[i] = Request{
			Method:  "GET",
			Path:    paths[i%len(paths)],
			Status:  200,
			Bytes:   1000 + i,
			Latency: time.Duration(500+i) * time.Microsecond,
		}
	}
	return reqs
}

func testValues() []float64 {
	vals := make([]float64, 10_000)
	for i := range vals {
		vals[i] = float64((i*7919)%1000) / 1000
	}
	return vals
}

func TestSameResults(t *testing.T) {
	reqs := testRequests()
	hb, ha := &TextHandler{}, &TextHandlerAfter{}
	LogRequestsBefore(NewLoggerBefore(hb), reqs)
	LogRequestsAfter(NewLoggerAfter(ha), reqs)
	if string(hb.Buf) != string(ha.Buf) {
		t.Errorf("Log: after differs from before:\n%.200s\n%.200s", hb.Buf, ha.Buf)
	}

	vals := testValues()
	n1, s1, m1 := StatsBefore(vals, Threshold{Min: 0.5})
	n2, s2, m2 := StatsAfter(vals, ThresholdAfter{Min: 0.5})
	if n1 != n2 || s1 != s2 || m1 != m2 {
		t.Errorf("Stats: before=(%d %v %v) after=(%d %v %v)", n1, s1, m1, n2, s2, m2)
	}
}

func BenchmarkLog(b *testing.B) {
	reqs := testRequests()
	b.Run("impl=before", func(b *testing.B) {
		h := &TextHandler{}
		l := NewLoggerBefore(h)
		for b.Loop() {
			h.Buf = h.Buf[:0]
			LogRequestsBefore(l, reqs)
		}
	})
	b.Run("impl=after", func(b *testing.B) {
		h := &TextHandlerAfter{}
		l := NewLoggerAfter(h)
		for b.Loop() {
			h.Buf = h.Buf[:0]
			LogRequestsAfter(l, reqs)
		}
	})
}

// minFlag is a variable so that the compiler can't treat the filter as a
// constant.
var minFlag = 0.5

func BenchmarkStats(b *testing.B) {
	vals := testValues()
	b.Run("impl=before", func(b *testing.B) {
		var f Filter = Threshold{Min: minFlag}
		for b.Loop() {
			StatsBefore(vals, f)
		}
	})
	b.Run("impl=after", func(b *testing.B) {
		for b.Loop() {
			StatsAfter(vals, ThresholdAfter{Min: minFlag})
		}
	})
}
