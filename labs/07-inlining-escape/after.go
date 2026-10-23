//go:build !solution

package inlesc

import (
	"encoding/binary"
	"hash/crc32"
)

// This is your file. It starts as a copy of before.go: change it so that
// MaxGapAfter stops allocating one decoder per block, then run the
// benchmarks to compare against before.go. Check your progress with:
//
//	go build -gcflags=-m ./07-inlining-escape/ 2>&1 | grep -i decoderafter
//
// The reference solution is in solution.go (run with -tags solution).

// DecoderAfter iterates over the timestamps of one block.
type DecoderAfter struct {
	src      []byte
	left     int
	t, delta int64
	started  bool
	err      error
}

// NewDecoderAfter validates the block header and checksum, and returns a
// decoder positioned at the first timestamp.
// TODO: this constructor is too expensive to inline, so &DecoderAfter{} is
// allocated here and escapes through the return value. Make the
// constructor a tiny inlinable wrapper that allocates the decoder and calls
// an init method doing the expensive part. Once inlined, the allocation is
// part of MaxGapAfter, where the decoder does not escape.
func NewDecoderAfter(block []byte) (*DecoderAfter, error) {
	if len(block) < headerSize {
		return nil, ErrHeader
	}
	if string(block[:2]) != magic || block[2] != version {
		return nil, ErrHeader
	}
	count := int(binary.LittleEndian.Uint16(block[3:5]))
	if count == 0 {
		return nil, ErrHeader
	}
	first := int64(binary.LittleEndian.Uint64(block[5:13]))
	if crc32.ChecksumIEEE(block[headerSize:]) != binary.LittleEndian.Uint32(block[13:17]) {
		return nil, ErrCorrupt
	}
	return &DecoderAfter{src: block[headerSize:], left: count, t: first}, nil
}

// Next returns the next timestamp, or false at the end of the block or on
// error (check Err).
func (d *DecoderAfter) Next() (int64, bool) {
	if d.left == 0 || d.err != nil {
		return 0, false
	}
	d.left--
	if !d.started {
		d.started = true
		return d.t, true
	}
	u, n := binary.Uvarint(d.src)
	if n <= 0 {
		d.err = ErrCorrupt
		return 0, false
	}
	d.src = d.src[n:]
	d.delta += int64(u>>1) ^ -int64(u&1)
	d.t += d.delta
	return d.t, true
}

// Err returns the first error found while decoding.
func (d *DecoderAfter) Err() error { return d.err }

// MaxGapAfter returns the largest gap between two consecutive timestamps in
// any of the blocks.
func MaxGapAfter(blocks [][]byte) (int64, error) {
	var maxGap int64
	for _, block := range blocks {
		d, err := NewDecoderAfter(block)
		if err != nil {
			return 0, err
		}
		prev, _ := d.Next()
		for {
			t, ok := d.Next()
			if !ok {
				break
			}
			maxGap = max(maxGap, t-prev)
			prev = t
		}
		if err := d.Err(); err != nil {
			return 0, err
		}
	}
	return maxGap, nil
}
