package pgo

import "testing"

const numLines = 100_000

// observer is the configured implementation. It's a package variable, like
// something read from a config file at startup, so the compiler can't see
// which concrete type Ingest will call.
var observer Observer = &Histogram{}

func TestIngest(t *testing.T) {
	data := GenerateLog(10_000)
	h := &Histogram{}
	n, err := NewAggregator(h).Ingest(data)
	if err != nil {
		t.Fatal(err)
	}
	if n != 10_000 {
		t.Fatalf("parsed %d lines, want 10000", n)
	}
	total, errs := 0, 0
	for e := range h.Counts {
		for _, c := range h.Counts[e] {
			total += int(c)
		}
		errs += int(h.Errors[e])
	}
	if total != n || errs == 0 || errs > n/50 {
		t.Fatalf("histogram total=%d errors=%d", total, errs)
	}

	c := &Counter{}
	if _, err := NewAggregator(c).Ingest(data); err != nil || c.N != n {
		t.Fatalf("Counter saw %d records (err %v), want %d", c.N, err, n)
	}

	if _, err := NewAggregator(c).Ingest([]byte("1,200,x\n")); err == nil {
		t.Fatal("expected a syntax error")
	}
}

func BenchmarkIngest(b *testing.B) {
	data := GenerateLog(numLines)
	a := NewAggregator(observer)
	b.SetBytes(int64(len(data)))
	for b.Loop() {
		if _, err := a.Ingest(data); err != nil {
			b.Fatal(err)
		}
	}
}
