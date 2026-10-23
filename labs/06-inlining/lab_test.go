package inlining

import (
	"encoding/binary"
	"slices"
	"testing"
)

const size = 10_000

// testBlock returns timestamps scraped every 10s with a little jitter, and
// their delta-of-delta encoding.
func testBlock() ([]int64, []byte) {
	ts := make([]int64, size)
	var block []byte
	t := int64(1_700_000_000_000) // milliseconds
	var prevT, prevDelta int64
	for i := range ts {
		t += 10_000 + int64(i%5) - 2
		ts[i] = t
		delta := t - prevT
		dod := delta - prevDelta
		block = binary.AppendUvarint(block, uint64(dod<<1)^uint64(dod>>63)) // zigzag
		prevT, prevDelta = t, delta
	}
	return ts, block
}

func TestSameResults(t *testing.T) {
	want, block := testBlock()
	for _, impl := range []struct {
		name string
		fn   func([]int64, []byte) ([]int64, error)
	}{{"before", DecodeTimestampsBefore}, {"after", DecodeTimestampsAfter}} {
		got, err := impl.fn(nil, block)
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("%s: wrong result (err=%v)", impl.name, err)
		}
		// Invalid inputs: truncated, too long, non-minimal.
		for _, bad := range [][]byte{{0x80}, slices.Repeat([]byte{0xff}, 11), {0x81, 0x00}} {
			if _, err := impl.fn(nil, bad); err != ErrCorrupt {
				t.Errorf("%s: %x: got err=%v, want ErrCorrupt", impl.name, bad, err)
			}
		}
	}
}

func BenchmarkDecode(b *testing.B) {
	_, block := testBlock()
	for _, impl := range []struct {
		name string
		fn   func([]int64, []byte) ([]int64, error)
	}{{"before", DecodeTimestampsBefore}, {"after", DecodeTimestampsAfter}} {
		b.Run("impl="+impl.name, func(b *testing.B) {
			dst := make([]int64, 0, size)
			for b.Loop() {
				impl.fn(dst[:0], block)
			}
		})
	}
}
