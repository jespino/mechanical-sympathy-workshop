//go:build solution

package structlayout

// ParticleAfter orders its fields from the largest alignment to the smallest:
// three float64 (24 bytes), a uint32 (4), a uint16 (2) and two bools (2).
// That adds up to exactly 32 bytes with no padding at all, down from 56.
type ParticleAfter struct {
	X      float64
	Y      float64
	Z      float64
	ID     uint32
	Kind   uint16
	Active bool
	Alive  bool
}

// SpawnAfter allocates n particles, one heap object each. Each object is now
// 32 bytes, which is exactly a size class, instead of 56 rounded up to 64.
func SpawnAfter(n int) []*ParticleAfter {
	out := make([]*ParticleAfter, n)
	for i := range out {
		out[i] = &ParticleAfter{
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

// FillAfter is unchanged: the struct literal uses field names, so the order
// of the fields in the type doesn't matter here.
func FillAfter(ps []ParticleAfter) {
	for i := range ps {
		ps[i] = ParticleAfter{
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

// SumActiveAfter is unchanged too. It is faster only because two particles
// now fit in each 64-byte cache line, so the loop reads 43% less memory.
func SumActiveAfter(ps []ParticleAfter) float64 {
	var sum float64
	for i := range ps {
		p := &ps[i]
		if p.Active {
			sum += p.X + p.Y + p.Z
		}
	}
	return sum
}
