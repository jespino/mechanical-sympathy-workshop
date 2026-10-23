// Package inlesc is the lab for module 07: inlining × escape analysis.
//
// before.go holds the naive implementation. Don't edit this file: it is the
// baseline that after.go is benchmarked against.
package inlesc

import (
	"encoding/binary"
	"errors"
	"hash/crc32"
)

// A block is a small header followed by delta-of-delta encoded timestamps
// (see module 06):
//
//	magic   [2]byte  "TS"
//	version byte     1
//	count   uint16   number of timestamps (little endian)
//	first   int64    first timestamp (little endian)
//	crc     uint32   CRC-32 (IEEE) of data (little endian)
//	data    count-1 zigzag varints
const (
	magic      = "TS"
	version    = 1
	headerSize = 2 + 1 + 2 + 8 + 4
)

var (
	ErrHeader  = errors.New("invalid block header")
	ErrCorrupt = errors.New("corrupt block data")
)

// DecoderBefore iterates over the timestamps of one block.
type DecoderBefore struct {
	src      []byte
	left     int
	t, delta int64
	started  bool
	err      error
}

// NewDecoderBefore validates the block header and checksum, and returns a
// decoder positioned at the first timestamp.
func NewDecoderBefore(block []byte) (*DecoderBefore, error) {
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
	return &DecoderBefore{src: block[headerSize:], left: count, t: first}, nil
}

// Next returns the next timestamp, or false at the end of the block or on
// error (check Err).
func (d *DecoderBefore) Next() (int64, bool) {
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
func (d *DecoderBefore) Err() error { return d.err }

// MaxGapBefore returns the largest gap between two consecutive timestamps in
// any of the blocks. It creates one decoder per block.
func MaxGapBefore(blocks [][]byte) (int64, error) {
	var maxGap int64
	for _, block := range blocks {
		d, err := NewDecoderBefore(block)
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
