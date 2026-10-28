//go:build !solution

package syncpool

import (
	"bytes"
	"io"
)

// This is your file. It starts as a copy of before.go: change it so that it
// reuses buffers instead of allocating a new one per call, then run the
// benchmarks to compare against before.go.
// The reference solution is in solution.go (run with -tags solution).

// WriteEventAfter formats e as one log line and writes it to w.
//
// TODO:
//  1. Declare a package-level sync.Pool whose New returns a *bytes.Buffer
//     (a pointer: storing a non-pointer value in a Pool allocates).
//  2. Get a buffer, Reset it, encode, write, and Put it back.
//  3. Don't Put back buffers that grew huge (look at fmt's pp.free).
//  4. w.Write must not keep buf.Bytes() after it returns: io.Writer
//     promises that, which is what makes reusing the buffer safe.
func WriteEventAfter(w io.Writer, e *Event) error {
	var buf bytes.Buffer
	encodeEvent(&buf, e)
	_, err := w.Write(buf.Bytes())
	return err
}
