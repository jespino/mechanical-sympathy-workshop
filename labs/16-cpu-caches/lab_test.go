package cpucaches

import (
	"slices"
	"testing"
)

const (
	matrixN  = 4096    // 4096×4096 float64 = 128 MB, far bigger than L3
	accountN = 1 << 20 // 1M accounts × 64 B = 64 MB
)

func testMatrix(n int) []float64 {
	m := make([]float64, n*n)
	for i := range m {
		m[i] = float64(i%1000) * 0.5
	}
	return m
}

func TestSameResults(t *testing.T) {
	m := testMatrix(300)
	if !slices.Equal(DailySalesBefore(m, 300), DailySalesAfter(m, 300)) {
		t.Error("DailySales: after differs from before")
	}
	if NewPtrLedgerBefore(10_000).Total() != NewPtrLedgerAfter(10_000).Total() {
		t.Error("PtrLedger: after differs from before")
	}
	if NewLedgerBefore(10_000).Total() != NewLedgerAfter(10_000).Total() {
		t.Error("Ledger: after differs from before")
	}
}

func BenchmarkDailySales(b *testing.B) {
	m := testMatrix(matrixN)
	for _, impl := range []struct {
		name string
		fn   func([]float64, int) []float64
	}{{"before", DailySalesBefore}, {"after", DailySalesAfter}} {
		b.Run("impl="+impl.name, func(b *testing.B) {
			for b.Loop() {
				impl.fn(m, matrixN)
			}
		})
	}
}

func BenchmarkPtrLedger(b *testing.B) {
	b.Run("impl=before", func(b *testing.B) {
		l := NewPtrLedgerBefore(accountN)
		for b.Loop() {
			l.Total()
		}
	})
	b.Run("impl=after", func(b *testing.B) {
		l := NewPtrLedgerAfter(accountN)
		for b.Loop() {
			l.Total()
		}
	})
}

func BenchmarkLedger(b *testing.B) {
	b.Run("impl=before", func(b *testing.B) {
		l := NewLedgerBefore(accountN)
		for b.Loop() {
			l.Total()
		}
	})
	b.Run("impl=after", func(b *testing.B) {
		l := NewLedgerAfter(accountN)
		for b.Loop() {
			l.Total()
		}
	})
}
