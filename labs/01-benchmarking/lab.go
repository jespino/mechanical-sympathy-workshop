// Package benchmarking is the lab for module 01: benchmarking.
//
// This lab is different from the others: the code under test is fine, and
// what's broken are the benchmarks. lab.go holds the functions we want to
// measure. Don't edit it.
package benchmarking

// Mix scrambles the bits of x. It is the finalizer of the SplitMix64
// generator, a popular way to turn a counter or an ID into a well-spread
// hash value.
func Mix(x uint64) uint64 {
	x ^= x >> 30
	x *= 0xbf58476d1ce4e5b9
	x ^= x >> 27
	x *= 0x94d049bb133111eb
	x ^= x >> 31
	return x
}

// PopCount returns the number of bits set in x, using the classic SWAR
// ("SIMD within a register") trick.
func PopCount(x uint64) int {
	x = x - ((x >> 1) & 0x5555555555555555)
	x = (x & 0x3333333333333333) + ((x >> 2) & 0x3333333333333333)
	x = (x + (x >> 4)) & 0x0f0f0f0f0f0f0f0f
	return int((x * 0x0101010101010101) >> 56)
}
