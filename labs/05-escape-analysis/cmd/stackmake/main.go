// stackmake shows when a make() that does not escape still ends up on the
// heap: a constant size lives on the stack (up to 64 KiB), but a size only
// known at run time gets a 32-byte stack buffer, and anything bigger goes to
// the heap.
package main

import (
	"fmt"
	"testing"
)

var sink byte

//go:noinline
func variableSize(n int) {
	buf := make([]byte, n) // size unknown at compile time
	buf[n-1] = 1
	sink = buf[n-1]
}

//go:noinline
func constantSize(n int) {
	buf := make([]byte, 1024) // size known at compile time
	buf[n-1] = 1
	sink = buf[n-1]
}

func main() {
	for _, n := range []int{8, 32, 33, 64, 1024} {
		v := testing.AllocsPerRun(1000, func() { variableSize(n) })
		c := testing.AllocsPerRun(1000, func() { constantSize(n) })
		fmt.Printf("n=%4d   make([]byte, n): %.0f allocs   make([]byte, 1024): %.0f allocs\n", n, v, c)
	}
}
