package inlesc

import (
	"encoding/binary"
	"hash/crc32"
	"testing"
)

const (
	numBlocks = 1_000
	perBlock  = 16
)

// makeBlock encodes timestamps into a block (see the format in before.go).
func makeBlock(ts []int64) []byte {
	var data []byte
	var prevDelta int64
	for i := 1; i < len(ts); i++ {
		delta := ts[i] - ts[i-1]
		dod := delta - prevDelta
		data = binary.AppendUvarint(data, uint64(dod<<1)^uint64(dod>>63))
		prevDelta = delta
	}
	block := []byte(magic)
	block = append(block, version)
	block = binary.LittleEndian.AppendUint16(block, uint16(len(ts)))
	block = binary.LittleEndian.AppendUint64(block, uint64(ts[0]))
	block = binary.LittleEndian.AppendUint32(block, crc32.ChecksumIEEE(data))
	return append(block, data...)
}

// testBlocks returns many small blocks of timestamps scraped every ~10s.
func testBlocks() [][]byte {
	blocks := make([][]byte, numBlocks)
	t := int64(1_700_000_000_000)
	for i := range blocks {
		ts := make([]int64, perBlock)
		for j := range ts {
			t += 10_000 + int64((i*perBlock+j)%7) - 3
			if (i*perBlock+j)%1000 == 999 {
				t += 60_000 // a missed scrape now and then
			}
			ts[j] = t
		}
		blocks[i] = makeBlock(ts)
	}
	return blocks
}

func TestSameResults(t *testing.T) {
	blocks := testBlocks()
	b, errB := MaxGapBefore(blocks)
	a, errA := MaxGapAfter(blocks)
	if errB != nil || errA != nil || a != b || b != 70_003 {
		t.Errorf("before=%d (%v) after=%d (%v), want 70003", b, errB, a, errA)
	}
	corrupt := testBlocks()
	corrupt[3][headerSize] ^= 0xff // breaks the checksum
	if _, err := MaxGapAfter(corrupt); err != ErrCorrupt {
		t.Errorf("after: corrupt block: got %v, want ErrCorrupt", err)
	}
	short := [][]byte{[]byte("TS")}
	if _, err := MaxGapAfter(short); err != ErrHeader {
		t.Errorf("after: short block: got %v, want ErrHeader", err)
	}
}

func BenchmarkMaxGap(b *testing.B) {
	blocks := testBlocks()
	for _, impl := range []struct {
		name string
		fn   func([][]byte) (int64, error)
	}{{"before", MaxGapBefore}, {"after", MaxGapAfter}} {
		b.Run("impl="+impl.name, func(b *testing.B) {
			for b.Loop() {
				impl.fn(blocks)
			}
		})
	}
}
