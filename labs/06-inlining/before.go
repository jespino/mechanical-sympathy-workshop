// Package inlining is the lab for module 06: inlining.
//
// before.go holds the naive implementation. Don't edit this file: it is the
// baseline that after.go is benchmarked against.
package inlining

import "errors"

// ErrCorrupt is returned when the input is not a valid sequence of varints.
var ErrCorrupt = errors.New("corrupt timestamp block")

// uvarintBefore decodes an unsigned LEB128 varint from the start of buf, like
// encoding/binary.Uvarint, but also rejects non-minimal encodings.
// It returns the value and the number of bytes read. n == 0 means buf was too
// short, n < 0 means the varint is invalid.
func uvarintBefore(buf []byte) (uint64, int) {
	var x uint64
	var s uint
	for i, b := range buf {
		if i == 10 {
			return 0, -(i + 1) // too long
		}
		if b < 0x80 {
			if i == 9 && b > 1 {
				return 0, -(i + 1) // overflows uint64
			}
			if b == 0 && i > 0 {
				return 0, -(i + 1) // non-minimal encoding
			}
			return x | uint64(b)<<s, i + 1
		}
		x |= uint64(b&0x7f) << s
		s += 7
	}
	return 0, 0 // buffer too short
}

// DecodeTimestampsBefore decodes a block of delta-of-delta encoded
// timestamps, appending them to dst. Each entry is a zigzag-encoded varint
// holding (t[i] - t[i-1]) - (t[i-1] - t[i-2]). For metrics scraped at a
// regular interval almost every entry is a tiny number that fits in 1 byte.
func DecodeTimestampsBefore(dst []int64, src []byte) ([]int64, error) {
	var t, delta int64
	for len(src) > 0 {
		u, n := uvarintBefore(src)
		if n <= 0 {
			return dst, ErrCorrupt
		}
		src = src[n:]
		delta += int64(u>>1) ^ -int64(u&1) // zigzag decode
		t += delta
		dst = append(dst, t)
	}
	return dst, nil
}
