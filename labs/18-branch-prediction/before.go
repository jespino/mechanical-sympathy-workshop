// Package branchprediction is the lab for module 18: branch prediction.
//
// before.go holds the naive implementations. Don't edit this file: it is the
// baseline that after.go is benchmarked against.
package branchprediction

// SumAboveBefore returns the sum and the count of the product prices in d that are
// >= t.
func SumAboveBefore(d []int32, t int32) (sum int64, count int) {
	for _, v := range d {
		if v >= t {
			sum += int64(v)
			count++
		}
	}
	return sum, count
}

// FilterBefore copies the product prices in d that are >= t to the front of out and
// returns how many it copied. out must have room for len(d) values.
func FilterBefore(d, out []int32, t int32) int {
	n := 0
	for _, v := range d {
		if v >= t {
			out[n] = v
			n++
		}
	}
	return n
}

// SumAboveSimple is not benchmarked: it's here so you can look at its assembly
// (see the module text). It returns the sum of the values in d that are >= t.
func SumAboveSimple(d []int32, t int32) int64 {
	var sum int64
	for _, v := range d {
		if v >= t {
			sum += int64(v)
		}
	}
	return sum
}
