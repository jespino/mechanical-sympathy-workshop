// Package structlayout is the lab for module 10: struct layout and size
// classes.
//
// before.go holds the naive implementations. Don't edit this file: it is the
// baseline that after.go is benchmarked against.
package structlayout

// ParticleBefore has its fields in "logical" order: each flag sits next to
// the value it describes. The compiler keeps that order, so every small field
// in front of an 8-byte field drags padding along with it.
type ParticleBefore struct {
	Active bool
	X      float64
	Kind   uint16
	Y      float64
	Alive  bool
	Z      float64
	ID     uint32
}

// SpawnBefore allocates n particles, one heap object each.
func SpawnBefore(n int) []*ParticleBefore {
	out := make([]*ParticleBefore, n)
	for i := range out {
		out[i] = &ParticleBefore{
			Active: i%3 != 0,
			Alive:  true,
			Kind:   uint16(i % 7),
			X:      float64(i),
			Y:      float64(i) * 0.5,
			Z:      float64(i) * 0.25,
			ID:     uint32(i),
		}
	}
	return out
}

// FillBefore fills ps with the same particles SpawnBefore creates, but
// stored by value in one contiguous slice.
func FillBefore(ps []ParticleBefore) {
	for i := range ps {
		ps[i] = ParticleBefore{
			Active: i%3 != 0,
			Alive:  true,
			Kind:   uint16(i % 7),
			X:      float64(i),
			Y:      float64(i) * 0.5,
			Z:      float64(i) * 0.25,
			ID:     uint32(i),
		}
	}
}

// SumActiveBefore adds up X+Y+Z for every active particle.
func SumActiveBefore(ps []ParticleBefore) float64 {
	var sum float64
	for i := range ps {
		p := &ps[i]
		if p.Active {
			sum += p.X + p.Y + p.Z
		}
	}
	return sum
}
