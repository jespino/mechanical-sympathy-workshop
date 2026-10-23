// Package bce is the lab for module 11: bounds check elimination.
//
// before.go holds the naive implementations. Don't edit this file: it is the
// baseline that after.go is benchmarked against.
package bce

// RecordSize is the size of one encoded order record:
//
//	offset  size  field
//	     0     4  id     (uint32, little endian)
//	     4     4  price  (uint32, little endian)
//	     8     2  qty    (uint16, little endian)
//	    10     2  flags  (uint16, little endian; bit 0 = paid)
//	    12     4  ts     (uint32, little endian)
const RecordSize = 16

// SumOrdersBefore decodes every record in b and returns a checksum: the sum
// of price*qty of the paid orders, mixed with the id and timestamp of every
// order. Trailing bytes that don't make a whole record are ignored.
func SumOrdersBefore(b []byte) uint64 {
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

// BlendBefore sets dst[i] = 3*a[i] + 5*b[i] + c[i] for i in [0, n). It
// panics if any of the slices is shorter than n.
func BlendBefore(dst, a, b, c []uint32, n int) {
	for i := 0; i < n; i++ {
		dst[i] = a[i]*3 + b[i]*5 + c[i]
	}
}
