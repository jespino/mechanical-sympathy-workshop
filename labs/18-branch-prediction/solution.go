//go:build solution

package branchprediction

// SumAboveAfter turns the comparison into a 0/1 value and the 0/1 value into
// a mask: -1 (all ones) when v >= t, 0 otherwise. Both updates become plain
// arithmetic, so there is nothing left to predict.
func SumAboveAfter(d []int32, t int32) (sum int64, count int) {
	for _, v := range d {
		b := 0
		if v >= t { // one variable, constant arms: compiled to SETcc, not a branch
			b = 1
		}
		sum += int64(v) & -int64(b)
		count += b
	}
	return sum, count
}

// FilterAfter always writes v to out[n] and advances n only when v >= t. A
// value that doesn't pass is overwritten by the next one. The loop still has
// a branch (the loop condition, and a bounds check on out[n]), but those are
// always predicted right.
func FilterAfter(d, out []int32, t int32) int {
	n := 0
	out = out[:len(d)]
	for _, v := range d {
		out[n] = v
		if v >= t {
			n++
		}
	}
	return n
}
