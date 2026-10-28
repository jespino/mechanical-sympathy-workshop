//go:build solution

package gcpressure

import "strconv"

// recordAfter has no pointers at all: the strings live in one shared byte
// slice and the record only stores where they start and end.
type recordAfter struct {
	ID       int64
	Score    float64
	nameOff  uint32
	emailOff uint32
	nameLen  uint16
	emailLen uint16
	Age      int32
}

// StoreAfter is pointer-free where it matters. records and text are each a
// single noscan allocation, and the map stores int32 indices, so its groups
// contain no pointers either. The GC marks these three objects and moves on:
// it never has to look inside them.
type StoreAfter struct {
	records []recordAfter
	text    []byte
	byID    map[int64]int32
}

// NewStoreAfter builds a store with n synthetic records.
func NewStoreAfter(n int) *StoreAfter {
	s := &StoreAfter{
		records: make([]recordAfter, 0, n),
		text:    make([]byte, 0, n*32),
		byID:    make(map[int64]int32, n),
	}
	for i := range n {
		r := recordAfter{
			ID:    recordID(i),
			Score: float64(i%1000) / 10,
			Age:   int32(18 + i%60),
		}
		r.nameOff = uint32(len(s.text))
		s.text = append(s.text, "user-"...)
		s.text = strconv.AppendInt(s.text, int64(i), 10)
		r.nameLen = uint16(uint32(len(s.text)) - r.nameOff)

		r.emailOff = uint32(len(s.text))
		s.text = append(s.text, "user"...)
		s.text = strconv.AppendInt(s.text, int64(i), 10)
		s.text = append(s.text, "@example.com"...)
		r.emailLen = uint16(uint32(len(s.text)) - r.emailOff)

		s.byID[r.ID] = int32(len(s.records))
		s.records = append(s.records, r)
	}
	return s
}

// Len returns the number of records.
func (s *StoreAfter) Len() int { return len(s.records) }

// Lookup returns the name and score of the record with the given ID.
// Converting the name to a string allocates; callers on a hot path could
// work with the []byte directly.
func (s *StoreAfter) Lookup(id int64) (name string, score float64, ok bool) {
	i, ok := s.byID[id]
	if !ok {
		return "", 0, false
	}
	r := &s.records[i]
	return string(s.text[r.nameOff : r.nameOff+uint32(r.nameLen)]), r.Score, true
}

// AverageScore returns the mean score of the records older than minAge.
func (s *StoreAfter) AverageScore(minAge int32) float64 {
	var sum float64
	var n int
	for i := range s.records {
		r := &s.records[i]
		if r.Age > minAge {
			sum += r.Score
			n++
		}
	}
	return sum / float64(max(n, 1))
}
