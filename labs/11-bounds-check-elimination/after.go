//go:build !solution

package bce

// This is your file. It starts as a copy of before.go: change it so that the
// compiler can prove the indexes are in bounds, then run the benchmarks to
// compare against before.go. The reference solution is in solution.go (run
// with -tags solution).
//
// List the bounds checks that are left with:
//
//	go build -gcflags='-d=ssa/check_bce/debug=1' ./11-bounds-check-elimination/

// SumOrdersAfter decodes every record in b and returns a checksum.
// TODO: the compiler can't prove that i+15 < len(b) (i+RecordSize could
// overflow), so every one of the 16 byte reads is checked. Re-slice instead:
// loop while len(b) >= RecordSize, read b[0]..b[15], then b = b[RecordSize:].
func SumOrdersAfter(b []byte) uint64 {
	var total uint64
	for i := 0; i+RecordSize <= len(b); i += RecordSize {
		id := uint32(b[i]) | uint32(b[i+1])<<8 | uint32(b[i+2])<<16 | uint32(b[i+3])<<24
		price := uint32(b[i+4]) | uint32(b[i+5])<<8 | uint32(b[i+6])<<16 | uint32(b[i+7])<<24
		qty := uint16(b[i+8]) | uint16(b[i+9])<<8
		flags := uint16(b[i+10]) | uint16(b[i+11])<<8
		ts := uint32(b[i+12]) | uint32(b[i+13])<<8 | uint32(b[i+14])<<16 | uint32(b[i+15])<<24
		if flags&1 != 0 {
			total += uint64(price) * uint64(qty)
		}
		total ^= uint64(id) + uint64(ts)
	}
	return total
}

// BlendAfter sets dst[i] = 3*a[i] + 5*b[i] + c[i] for i in [0, n). It
// panics if any of the slices is shorter than n.
// TODO: i < n says nothing about len(dst), len(a), len(b) or len(c), so the
// loop does four bounds checks per element. Re-slice all four to length n
// before the loop, and range over dst: then the compiler knows every index is
// in bounds, and the checks happen once, outside the loop. Careful: a[:n] is
// checked against cap(a), not len(a). The test checks that a short slice
// still panics.
func BlendAfter(dst, a, b, c []uint32, n int) {
	for i := 0; i < n; i++ {
		dst[i] = a[i]*3 + b[i]*5 + c[i]
	}
}
