//go:build solution

package inlesc

import (
	"encoding/binary"
	"hash/crc32"
)

// DecoderAfter iterates over the timestamps of one block.
type DecoderAfter struct {
	src      []byte
	left     int
	t, delta int64
	started  bool
	err      error
}

// NewDecoderAfter is small enough to be inlined. When it is, the
// &DecoderAfter{} below becomes an allocation in the caller, and escape
// analysis decides there whether it can live on the caller's stack.
func NewDecoderAfter(block []byte) (*DecoderAfter, error) {
	d := &DecoderAfter{}
	if err := d.init(block); err != nil {
		return nil, err
	}
	return d, nil
}

// init does the expensive part of the construction. It only writes through
// d, so d does not escape, and inlining NewDecoderAfter is all it takes.
func (d *DecoderAfter) init(block []byte) error {
	if len(block) < headerSize {
		return ErrHeader
	}
	if string(block[:2]) != magic || block[2] != version {
		return ErrHeader
	}
	count := int(binary.LittleEndian.Uint16(block[3:5]))
	if count == 0 {
		return ErrHeader
	}
	first := int64(binary.LittleEndian.Uint64(block[5:13]))
	if crc32.ChecksumIEEE(block[headerSize:]) != binary.LittleEndian.Uint32(block[13:17]) {
		return ErrCorrupt
	}
	*d = DecoderAfter{src: block[headerSize:], left: count, t: first}
	return nil
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

// MaxGapAfter is unchanged: the fix is entirely in the constructor.
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
