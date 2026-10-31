//go:build !solution

package branchprediction

// This is your file. It starts as a copy of before.go: change it so that the
// hot loops have no data-dependent branches, then run the benchmarks to
// compare against before.go. The reference solution is in solution.go (run
// with -tags solution).

// SumAboveAfter returns the sum and the count of the product prices in d that are
// >= t.
// TODO: the compiler keeps this if as a real branch (two variables change in
// it). On random data, the CPU guesses wrong half of the time. Compute a 0/1
// value from the comparison, then update both sum and count with arithmetic.
func SumAboveAfter(d []int32, t int32) (sum int64, count int) {
	for _, v := range d {
		if v >= t {
			sum += int64(v)
			count++
		}
	}
	return sum, count
}

// FilterAfter copies the product prices in d that are >= t to the front of out and
// returns how many it copied. out must have room for len(d) values.
// TODO: a store inside an if can never become a conditional move. Store
// every value unconditionally at out[n], and only advance n when v >= t.
func FilterAfter(d, out []int32, t int32) int {
	n := 0
	for _, v := range d {
		if v >= t {
			out[n] = v
			n++
		}
	}
	return n
}
