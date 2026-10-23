// Package ifaces is the lab for module 08: interfaces, boxing and generics.
//
// before.go holds the naive implementations. Don't edit this file: it is the
// baseline that after.go is benchmarked against.
package ifaces

import (
	"strconv"
	"time"
)

// Request is what our toy HTTP server logs after serving a request.
type Request struct {
	Method  string
	Path    string
	Status  int
	Bytes   int
	Latency time.Duration
}

// ---------------------------------------------------------------------------
// Part 1: a tiny structured logger.

// Field is a key/value pair. The value is stored in an interface.
type Field struct {
	Key   string
	Value any
}

func String(key, v string) Field                 { return Field{key, v} }
func Int(key string, v int) Field                { return Field{key, v} }
func Duration(key string, v time.Duration) Field { return Field{key, v} }

// Handler formats and writes log entries. Like log/slog.Handler, it's an
// interface so that users can plug in their own.
type Handler interface {
	Handle(msg string, fields []Field)
}

// TextHandler appends entries as "msg key=value key=value\n" to Buf.
type TextHandler struct {
	Buf []byte
}

func (h *TextHandler) Handle(msg string, fields []Field) {
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

// LoggerBefore sends entries to its Handler.
type LoggerBefore struct {
	h Handler
}

func NewLoggerBefore(h Handler) *LoggerBefore { return &LoggerBefore{h: h} }

func (l *LoggerBefore) Info(msg string, fields ...Field) {
	l.h.Handle(msg, fields)
}

// LogRequestsBefore logs one line per request.
func LogRequestsBefore(l *LoggerBefore, reqs []Request) {
	for _, r := range reqs {
		l.Info("request",
			String("method", r.Method),
			String("path", r.Path),
			Int("status", r.Status),
			Int("bytes", r.Bytes),
			Duration("latency", r.Latency),
		)
	}
}

// ---------------------------------------------------------------------------
// Part 2: dynamic dispatch in a hot loop.

// Filter decides whether a value is kept.
type Filter interface {
	Keep(v float64) bool
}

// Threshold keeps values >= Min.
type Threshold struct{ Min float64 }

func (t Threshold) Keep(v float64) bool { return v >= t.Min }

// StatsBefore returns how many values f keeps, and their sum and maximum.
func StatsBefore(vals []float64, f Filter) (n int, sum, hi float64) {
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
