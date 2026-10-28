package structlayout

import (
	"testing"
	"unsafe"
)

const (
	spawnN = 10_000    // individually allocated particles
	sumN   = 1_000_000 // 56 MB before, 32 MB after: bigger than the caches
)

func TestSameResults(t *testing.T) {
	t.Logf("sizeof: before=%d after=%d", unsafe.Sizeof(ParticleBefore{}), unsafe.Sizeof(ParticleAfter{}))
	b, a := SpawnBefore(100), SpawnAfter(100)
	for i := range b {
		pb, pa := b[i], a[i]
		if pb.Active != pa.Active || pb.Alive != pa.Alive || pb.Kind != pa.Kind ||
			pb.X != pa.X || pb.Y != pa.Y || pb.Z != pa.Z || pb.ID != pa.ID {
			t.Fatalf("particle %d differs: %+v vs %+v", i, *pb, *pa)
		}
	}
	bs, as := make([]ParticleBefore, 1000), make([]ParticleAfter, 1000)
	FillBefore(bs)
	FillAfter(as)
	if SumActiveBefore(bs) != SumActiveAfter(as) {
		t.Error("SumActive: after differs from before")
	}
}

// The sinks keep the results alive so the compiler can't stack-allocate them.
var (
	sinkBefore []*ParticleBefore
	sinkAfter  []*ParticleAfter
)

func BenchmarkSpawn(b *testing.B) {
	b.Run("impl=before", func(b *testing.B) {
		for b.Loop() {
			sinkBefore = SpawnBefore(spawnN)
		}
	})
	b.Run("impl=after", func(b *testing.B) {
		for b.Loop() {
			sinkAfter = SpawnAfter(spawnN)
		}
	})
}

func BenchmarkSumActive(b *testing.B) {
	b.Run("impl=before", func(b *testing.B) {
		ps := make([]ParticleBefore, sumN)
		FillBefore(ps)
		for b.Loop() {
			SumActiveBefore(ps)
		}
	})
	b.Run("impl=after", func(b *testing.B) {
		ps := make([]ParticleAfter, sumN)
		FillAfter(ps)
		for b.Loop() {
			SumActiveAfter(ps)
		}
	})
}
