//go:build solution

package bce

// SumOrdersAfter re-slices b after every record. Inside the loop the compiler
// knows len(b) >= 16, so b[0]..b[15] need no checks at all, and b[16:] is
// safe too. check_bce reports nothing for this function.
func SumOrdersAfter(b []byte) uint64 {
	var total uint64
	for len(b) >= RecordSize {
		id := uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
		price := uint32(b[4]) | uint32(b[5])<<8 | uint32(b[6])<<16 | uint32(b[7])<<24
		qty := uint16(b[8]) | uint16(b[9])<<8
		flags := uint16(b[10]) | uint16(b[11])<<8
		ts := uint32(b[12]) | uint32(b[13])<<8 | uint32(b[14])<<16 | uint32(b[15])<<24
		if flags&1 != 0 {
			total += uint64(price) * uint64(qty)
		}
		total ^= uint64(id) + uint64(ts)
		b = b[RecordSize:]
	}
	return total
}

// BlendAfter checks the lengths once, up front, and then re-slices the four
// slices to length n. Inside the loop, i < len(dst) == len(a) == len(b) ==
// len(c), so the prove pass removes every per-element check.
//
// The explicit length check matters: a[:n] alone is checked against cap(a),
// not len(a), so it would silently read past the end of a short slice that
// has spare capacity, where BlendBefore panics.
func BlendAfter(dst, a, b, c []uint32, n int) {
	if n > len(dst) || n > len(a) || n > len(b) || n > len(c) {
		panic("BlendAfter: slice shorter than n")
	}
	dst, a, b, c = dst[:n], a[:n], b[:n], c[:n]
	for i := range dst {
		dst[i] = a[i]*3 + b[i]*5 + c[i]
	}
}
