// dispatch compares four ways of calling the same tiny method in a hot loop:
// through an interface, through a type parameter instantiated with a value
// type and with a pointer type, and directly on the concrete type.
package main

import (
	"fmt"
	"testing"
)

type Filter interface{ Keep(v float64) bool }

type Threshold struct{ Min float64 }

func (t Threshold) Keep(v float64) bool { return v >= t.Min }

type PtrThreshold struct{ Min float64 }

func (t *PtrThreshold) Keep(v float64) bool { return v >= t.Min }

//go:noinline
func countIface(vals []float64, f Filter) (n int) {
	for _, v := range vals {
		if f.Keep(v) {
			n++
		}
	}
	return n
}

//go:noinline
func countGeneric[F Filter](vals []float64, f F) (n int) {
	for _, v := range vals {
		if f.Keep(v) {
			n++
		}
	}
	return n
}

//go:noinline
func countConcrete(vals []float64, f Threshold) (n int) {
	for _, v := range vals {
		if f.Keep(v) {
			n++
		}
	}
	return n
}

var minFlag = 0.5

func main() {
	vals := make([]float64, 10_000)
	for i := range vals {
		vals[i] = float64((i*7919)%1000) / 1000
	}
	run := func(name string, fn func()) {
		r := testing.Benchmark(func(b *testing.B) {
			for b.Loop() {
				fn()
			}
		})
		fmt.Printf("%-28s %8.2f µs/op\n", name, float64(r.NsPerOp())/1000)
	}
	run("interface", func() { countIface(vals, Threshold{minFlag}) })
	run("generic, value type arg", func() { countGeneric(vals, Threshold{minFlag}) })
	run("generic, pointer type arg", func() { countGeneric(vals, &PtrThreshold{minFlag}) })
	run("concrete type", func() { countConcrete(vals, Threshold{minFlag}) })
}
