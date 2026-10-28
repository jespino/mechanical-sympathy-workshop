// Package syncpool is the lab for module 11: sync.Pool.
//
// before.go holds the naive implementation. Don't edit this file: it is the
// baseline that after.go is benchmarked against.
package syncpool

import (
	"bytes"
	"io"
	"strconv"
)

// Event is one structured log event.
type Event struct {
	Time    int64
	Level   string
	Service string
	Message string
	UserID  int64
	Latency float64
	Tags    []string
}

// WriteEventBefore formats e as one JSON-ish log line and writes it to w.
//
// It builds the line in a fresh bytes.Buffer on every call. The buffer starts
// empty, grows a few times while we append to it (64 -> 128 -> 256 -> 512
// bytes), and becomes garbage as soon as we return.
func WriteEventBefore(w io.Writer, e *Event) error {
	var buf bytes.Buffer
	encodeEvent(&buf, e)
	_, err := w.Write(buf.Bytes())
	return err
}

// encodeEvent appends the line for e to buf. It is shared by before and after:
// the lab is about where the buffer comes from, not about the formatting.
func encodeEvent(buf *bytes.Buffer, e *Event) {
	buf.WriteString(`{"time":`)
	buf.Write(strconv.AppendInt(buf.AvailableBuffer(), e.Time, 10))
	buf.WriteString(`,"level":`)
	buf.Write(strconv.AppendQuote(buf.AvailableBuffer(), e.Level))
	buf.WriteString(`,"service":`)
	buf.Write(strconv.AppendQuote(buf.AvailableBuffer(), e.Service))
	buf.WriteString(`,"msg":`)
	buf.Write(strconv.AppendQuote(buf.AvailableBuffer(), e.Message))
	buf.WriteString(`,"user":`)
	buf.Write(strconv.AppendInt(buf.AvailableBuffer(), e.UserID, 10))
	buf.WriteString(`,"latency_ms":`)
	buf.Write(strconv.AppendFloat(buf.AvailableBuffer(), e.Latency, 'f', 3, 64))
	buf.WriteString(`,"tags":[`)
	for i, t := range e.Tags {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.Write(strconv.AppendQuote(buf.AvailableBuffer(), t))
	}
	buf.WriteString("]}\n")
}
