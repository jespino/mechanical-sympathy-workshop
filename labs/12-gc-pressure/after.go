//go:build !solution

package gcpressure

import "strconv"

// This is your file. It starts as a copy of before.go: change the layout so
// the GC has (almost) nothing to scan, then run the benchmarks to compare.
// The reference solution is in solution.go (run with -tags solution).
//
// TODO:
//  1. Make the record type pointer-free. Strings are pointers too: store the
//     text of every record in one big []byte and keep offsets and lengths.
//  2. Store the records by value in one []record, not as []*record.
//  3. Make the map pointer-free too: map[int64]int32 (an index into the
//     slice) instead of map[int64]*Record.

type recordAfter struct {
	ID    int64
	Name  string
	Email string
	Score float64
	Age   int32
}

// StoreAfter is your version of the store.
type StoreAfter struct {
	records []*recordAfter
	byID    map[int64]*recordAfter
}

// NewStoreAfter builds a store with n synthetic records.
func NewStoreAfter(n int) *StoreAfter {
	s := &StoreAfter{
		records: make([]*recordAfter, 0, n),
		byID:    make(map[int64]*recordAfter, n),
	}
	for i := range n {
		r := &recordAfter{
			ID:    recordID(i),
			Name:  "user-" + strconv.Itoa(i),
			Email: "user" + strconv.Itoa(i) + "@example.com",
			Score: float64(i%1000) / 10,
			Age:   int32(18 + i%60),
		}
		s.records = append(s.records, r)
		s.byID[r.ID] = r
	}
	return s
}

// Len returns the number of records.
func (s *StoreAfter) Len() int { return len(s.records) }

// Lookup returns the name and score of the record with the given ID.
func (s *StoreAfter) Lookup(id int64) (name string, score float64, ok bool) {
	r, ok := s.byID[id]
	if !ok {
		return "", 0, false
	}
	return r.Name, r.Score, true
}

// AverageScore returns the mean score of the records older than minAge.
func (s *StoreAfter) AverageScore(minAge int32) float64 {
	var sum float64
	var n int
	for _, r := range s.records {
		if r.Age > minAge {
			sum += r.Score
			n++
		}
	}
	return sum / float64(max(n, 1))
}
