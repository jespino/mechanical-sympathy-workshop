//go:build solution

package inlining

// uvarintAfter is the inlinable fast path: a single byte below 0x80 is the
// whole varint. It uses the same shape as unicode/utf8.DecodeRune (a range
// loop that always breaks, and named results assigned from the slow call),
// which keeps its cost at 79, just under the budget of 80.
func uvarintAfter(buf []byte) (x uint64, n int) {
	for _, b := range buf {
		if b < 0x80 {
			return uint64(b), 1
		}
		break
	}
	x, n = uvarintSlow(buf)
	return
}

// uvarintSlow is the full decoder. It's too expensive to inline, but it's
// only called for the rare multi-byte (or invalid) varints.
func uvarintSlow(buf []byte) (uint64, int) {
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

// DecodeTimestampsAfter is unchanged: once uvarintAfter is inlined, its fast
// path becomes part of this loop.
func DecodeTimestampsAfter(dst []int64, src []byte) ([]int64, error) {
	var t, delta int64
	for len(src) > 0 {
		u, n := uvarintAfter(src)
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
