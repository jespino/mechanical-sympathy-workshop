// Command sizes prints the memory layout of the lab's structs: the offset,
// size and alignment of every field, and where the padding goes.
package main

import (
	"fmt"
	"reflect"

	structlayout "labs/10-struct-layout"
)

// sizeClasses are the first allocator size classes, from
// internal/runtime/gc/sizeclasses.go.
var sizeClasses = []uintptr{8, 16, 24, 32, 48, 64, 80, 96, 112, 128}

func sizeClass(n uintptr) uintptr {
	for _, c := range sizeClasses {
		if n <= c {
			return c
		}
	}
	return 0
}

func layout(v any) {
	t := reflect.TypeOf(v)
	fmt.Printf("%s: size=%d align=%d (heap size class %d)\n", t.Name(), t.Size(), t.Align(), sizeClass(t.Size()))
	var end uintptr
	for i := range t.NumField() {
		f := t.Field(i)
		if f.Offset > end {
			fmt.Printf("  %3d  %-8s %d bytes\n", end, "padding", f.Offset-end)
		}
		fmt.Printf("  %3d  %-8s %-8s size=%d align=%d\n", f.Offset, f.Name, f.Type, f.Type.Size(), f.Type.Align())
		end = f.Offset + f.Type.Size()
	}
	if t.Size() > end {
		fmt.Printf("  %3d  %-8s %d bytes\n", end, "padding", t.Size()-end)
	}
	fmt.Println()
}

// Trailing is the zero-size-field gotcha: a zero-size field at the end of a
// struct gets padded so that &t.End can't point past the object.
type Trailing struct {
	N   int64
	End struct{}
}

// Leading has the same fields, with the zero-size one first.
type Leading struct {
	Start struct{}
	N     int64
}

func main() {
	layout(structlayout.OrderBefore{})
	layout(structlayout.OrderAfter{})
	layout(Trailing{})
	layout(Leading{})
}
