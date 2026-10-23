// Package gcpressure is the lab for module 14: helping the garbage collector.
//
// before.go holds the naive implementation. Don't edit this file: it is the
// baseline that after.go is benchmarked against.
package gcpressure

import "strconv"

// Record is one user profile in an in-memory store.
type Record struct {
	ID    int64
	Name  string
	Email string
	Score float64
	Age   int32
}

// StoreBefore keeps every record in its own heap object, and indexes them by
// ID with a map of pointers. Simple and idiomatic, but every *Record, every
// string and every map slot is a pointer the GC has to follow, on every cycle,
// for as long as the store is alive.
type StoreBefore struct {
	records []*Record
	byID    map[int64]*Record
}

// NewStoreBefore builds a store with n synthetic records.
func NewStoreBefore(n int) *StoreBefore {
	s := &StoreBefore{
		records: make([]*Record, 0, n),
		byID:    make(map[int64]*Record, n),
	}
	for i := range n {
		r := &Record{
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
func (s *StoreBefore) Len() int { return len(s.records) }

// Lookup returns the name and score of the record with the given ID.
func (s *StoreBefore) Lookup(id int64) (name string, score float64, ok bool) {
	r, ok := s.byID[id]
	if !ok {
		return "", 0, false
	}
	return r.Name, r.Score, true
}

// AverageScore returns the mean score of the records older than minAge.
func (s *StoreBefore) AverageScore(minAge int32) float64 {
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

// recordID spreads the IDs out a bit so they are not just 0..n-1.
func recordID(i int) int64 { return int64(i)*7 + 1_000_000 }
