// growth prints every time append has to move a slice to a bigger backing
// array, and how many bytes were copied along the way.
package main

import "fmt"

func main() {
	var s []int
	copied := 0
	prevCap := cap(s)
	for i := range 2_000 {
		if len(s) == cap(s) {
			copied += len(s) * 8 // growslice copies every existing element
		}
		s = append(s, i)
		if cap(s) != prevCap {
			fmt.Printf("len=%5d  cap %5d -> %5d  (x%.2f)\n", len(s), prevCap, cap(s), float64(cap(s))/float64(max(prevCap, 1)))
			prevCap = cap(s)
		}
	}
	fmt.Printf("\nappended %d ints (%d bytes), copied %d bytes while growing\n", len(s), len(s)*8, copied)
}
