//go:build !solution

package inlining

// This is your file. It starts as a copy of before.go: change it so that the
// hot helper gets inlined, then run the benchmarks to compare against
// before.go. Check the cost with:
//
//	go build -gcflags=-m=2 ./06-inlining/ 2>&1 | grep uvarint
//
// The reference solution is in solution.go (run with -tags solution).

// uvarintAfter decodes an unsigned LEB128 varint from the start of buf.
// TODO: its cost is just over the inlining budget, so every entry pays for a
// function call. Almost every varint in our data is a single byte (< 0x80).
// Keep this function as a tiny fast path for that case, and move the loop
// into a separate slow-path function. Look at how unicode/utf8.DecodeRune
// does it: the exact shape of the code matters for the cost.
func uvarintAfter(buf []byte) (uint64, int) {
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

// DecodeTimestampsAfter decodes a block of delta-of-delta encoded timestamps.
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
