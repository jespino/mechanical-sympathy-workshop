//go:build solution

package syncpool

import (
	"bytes"
	"io"
	"sync"
)

// maxPooledBuffer is the largest buffer we are willing to keep in the pool.
// Same idea (and same limit) as fmt's pp.free: one huge event must not pin
// a huge buffer per P forever.
const maxPooledBuffer = 64 << 10

// bufPool stores *bytes.Buffer, not bytes.Buffer: converting a pointer to
// an interface is free, converting a struct allocates.
var bufPool = sync.Pool{
	New: func() any { return new(bytes.Buffer) },
}

// WriteEventAfter borrows a buffer from the pool, formats into it, writes it
// and gives it back. In steady state there are no allocations at all.
func WriteEventAfter(w io.Writer, e *Event) error {
	buf := bufPool.Get().(*bytes.Buffer)
	buf.Reset()
	encodeEvent(buf, e)
	_, err := w.Write(buf.Bytes())
	if buf.Cap() <= maxPooledBuffer {
		bufPool.Put(buf)
	}
	return err
}
