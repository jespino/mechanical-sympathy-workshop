//go:build !solution

package ifaces

import (
	"strconv"
	"time"
)

// This is your file. It starts as a copy of before.go: change it so that it
// allocates less and dispatches less, then run the benchmarks to compare
// against before.go. Use `go build -gcflags=-m ./08-interfaces-boxing/` to
// see what escapes. The reference solution is in solution.go (run with
// -tags solution).
//
// Part 1 TODOs:
//   - Every non-pointer value stored in Value (an `any`) is boxed, and since
//     the fields go through an interface method call, the boxes escape and
//     are heap allocated. Replace `any` with a small concrete Value type that
//     can hold a string or an integer without boxing (log/slog.Value is
//     the real-world version of this idea).
//   - The variadic []FieldAfter escapes too, for the same reason. Copy the
//     fields into a fixed-size record passed by value instead.
//
// Part 2 TODO:
//   - f.Keep is an indirect call that can't be inlined. Try making StatsAfter
//     generic first ([F FilterAfter]): is it faster? Then try the concrete
//     type. The benchmark calls StatsAfter(vals, ThresholdAfter{...}), which
//     compiles with any of the three signatures.

// ---------------------------------------------------------------------------
// Part 1: a tiny structured logger.

// FieldAfter is a key/value pair. The value is stored in an interface.
type FieldAfter struct {
	Key   string
	Value any
}

func StringAfter(key, v string) FieldAfter                 { return FieldAfter{key, v} }
func IntAfter(key string, v int) FieldAfter                { return FieldAfter{key, v} }
func DurationAfter(key string, v time.Duration) FieldAfter { return FieldAfter{key, v} }

// HandlerAfter formats and writes log entries. Like log/slog.Handler, it's an
// interface so that users can plug in their own.
type HandlerAfter interface {
	Handle(msg string, fields []FieldAfter)
}

// TextHandlerAfter appends entries as "msg key=value key=value\n" to Buf.
type TextHandlerAfter struct {
	Buf []byte
}

func (h *TextHandlerAfter) Handle(msg string, fields []FieldAfter) {
	h.Buf = append(h.Buf, msg...)
	for _, f := range fields {
		h.Buf = append(h.Buf, ' ')
		h.Buf = append(h.Buf, f.Key...)
		h.Buf = append(h.Buf, '=')
		switch v := f.Value.(type) {
		case string:
			h.Buf = append(h.Buf, v...)
		case int:
			h.Buf = strconv.AppendInt(h.Buf, int64(v), 10)
		case time.Duration:
			h.Buf = strconv.AppendInt(h.Buf, v.Microseconds(), 10)
			h.Buf = append(h.Buf, "us"...)
		}
	}
	h.Buf = append(h.Buf, '\n')
}

// LoggerAfter sends entries to its Handler.
type LoggerAfter struct {
	h HandlerAfter
}

func NewLoggerAfter(h HandlerAfter) *LoggerAfter { return &LoggerAfter{h: h} }

func (l *LoggerAfter) Info(msg string, fields ...FieldAfter) {
	l.h.Handle(msg, fields)
}

// LogRequestsAfter logs one line per request.
func LogRequestsAfter(l *LoggerAfter, reqs []Request) {
	for _, r := range reqs {
		l.Info("request",
			StringAfter("method", r.Method),
			StringAfter("path", r.Path),
			IntAfter("status", r.Status),
			IntAfter("bytes", r.Bytes),
			DurationAfter("latency", r.Latency),
		)
	}
}

// ---------------------------------------------------------------------------
// Part 2: dynamic dispatch in a hot loop.

// FilterAfter decides whether a value is kept.
type FilterAfter interface {
	Keep(v float64) bool
}

// ThresholdAfter keeps values >= Min.
type ThresholdAfter struct{ Min float64 }

func (t ThresholdAfter) Keep(v float64) bool { return v >= t.Min }

// StatsAfter returns how many values f keeps, and their sum and maximum.
func StatsAfter(vals []float64, f FilterAfter) (n int, sum, hi float64) {
	for _, v := range vals {
		if !f.Keep(v) {
			continue
		}
		n++
		sum += v
		hi = max(hi, v)
	}
	return n, sum, hi
}
