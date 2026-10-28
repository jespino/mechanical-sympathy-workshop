//go:build !solution

package structlayout

// This is your file. It starts as a copy of before.go: change it so that
// ParticleAfter takes less memory, then run the benchmarks to compare against
// before.go. The reference solution is in solution.go (run with -tags
// solution).

// ParticleAfter holds the same fields as ParticleBefore.
// TODO: unsafe.Sizeof(ParticleAfter{}) is 56. Reorder the fields (largest
// alignment first) so the padding disappears. Don't add, remove or retype
// fields: the test checks the values are the same.
type ParticleAfter struct {
	Active bool
	X      float64
	Kind   uint16
	Y      float64
	Alive  bool
	Z      float64
	ID     uint32
}

// SpawnAfter allocates n particles, one heap object each.
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

// FillAfter fills ps with the same particles SpawnAfter creates, but stored
// by value in one contiguous slice.
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

// SumActiveAfter adds up X+Y+Z for every active particle.
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
