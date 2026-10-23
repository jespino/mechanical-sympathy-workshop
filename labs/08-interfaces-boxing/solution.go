//go:build solution

package ifaces

import (
	"strconv"
	"time"
)

// ---------------------------------------------------------------------------
// Part 1: a tiny structured logger.

// Kind says which field of a Value is in use.
type Kind uint8

const (
	KindString Kind = iota
	KindInt
	KindDuration
)

// Value holds a string or an integer without boxing it in an interface:
// integers live in num, strings in str. log/slog.Value uses the same idea
// (more compactly, by also storing the kind and the string pointer in an
// `any` field it controls).
type Value struct {
	kind Kind
	num  uint64
	str  string
}

// FieldAfter is a key/value pair with a concrete Value.
type FieldAfter struct {
	Key   string
	Value Value
}

func StringAfter(key, v string) FieldAfter {
	return FieldAfter{key, Value{kind: KindString, str: v}}
}

func IntAfter(key string, v int) FieldAfter {
	return FieldAfter{key, Value{kind: KindInt, num: uint64(v)}}
}

func DurationAfter(key string, v time.Duration) FieldAfter {
	return FieldAfter{key, Value{kind: KindDuration, num: uint64(v)}}
}

// Record is one log entry. It is passed to the Handler by value, with the
// fields stored inline, so there is no slice to escape. Extra fields beyond
// the inline array go to the (rarely used) back slice.
type Record struct {
	Msg   string
	n     int
	front [6]FieldAfter
	back  []FieldAfter
}

// Fields calls yield for every field of the record.
func (r *Record) Fields(yield func(FieldAfter)) {
	for _, f := range r.front[:r.n] {
		yield(f)
	}
	for _, f := range r.back {
		yield(f)
	}
}

// HandlerAfter formats and writes log entries.
type HandlerAfter interface {
	Handle(r Record)
}

// TextHandlerAfter appends entries as "msg key=value key=value\n" to Buf.
type TextHandlerAfter struct {
	Buf []byte
}

func (h *TextHandlerAfter) Handle(r Record) {
	h.Buf = append(h.Buf, r.Msg...)
	r.Fields(func(f FieldAfter) {
		h.Buf = append(h.Buf, ' ')
		h.Buf = append(h.Buf, f.Key...)
		h.Buf = append(h.Buf, '=')
		switch f.Value.kind {
		case KindString:
			h.Buf = append(h.Buf, f.Value.str...)
		case KindInt:
			h.Buf = strconv.AppendInt(h.Buf, int64(f.Value.num), 10)
		case KindDuration:
			h.Buf = strconv.AppendInt(h.Buf, time.Duration(f.Value.num).Microseconds(), 10)
			h.Buf = append(h.Buf, "us"...)
		}
	})
	h.Buf = append(h.Buf, '\n')
}

// LoggerAfter sends entries to its Handler.
type LoggerAfter struct {
	h HandlerAfter
}

func NewLoggerAfter(h HandlerAfter) *LoggerAfter { return &LoggerAfter{h: h} }

// Info copies the fields into a Record. Only the contents of fields (the
// strings) reach the handler, never the slice itself, so the variadic slice
// stays on the caller's stack.
func (l *LoggerAfter) Info(msg string, fields ...FieldAfter) {
	r := Record{Msg: msg}
	r.n = copy(r.front[:], fields)
	if len(fields) > r.n {
		r.back = append([]FieldAfter(nil), fields[r.n:]...)
	}
	l.h.Handle(r)
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

// StatsAfter takes the concrete type: t.Keep is a direct call, it is
// inlined, and the comparison becomes part of the loop. Making StatsAfter
// generic ([F FilterAfter]) would not help: with GC-shape stenciling the
// method is still called indirectly, through the dictionary.
func StatsAfter(vals []float64, t ThresholdAfter) (n int, sum, hi float64) {
	for _, v := range vals {
		if !t.Keep(v) {
			continue
		}
		n++
		sum += v
		hi = max(hi, v)
	}
	return n, sum, hi
}
