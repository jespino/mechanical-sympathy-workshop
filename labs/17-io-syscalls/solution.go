//go:build solution

package iosyscalls

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"strconv"
)

// WriteRecordsAfter formats every record into a reusable scratch slice with
// strconv (no fmt, no interface boxing) and hands it to a bufio.Writer, which
// only calls write(2) when its 4 KiB buffer fills up, and once more in Flush.
func WriteRecordsAfter(f *os.File, recs []Record) error {
	w := bufio.NewWriter(f)
	var line []byte
	for _, r := range recs {
		line = strconv.AppendInt(line[:0], int64(r.ID), 10)
		line = append(line, ',')
		line = append(line, r.Name...)
		line = append(line, ',')
		line = strconv.AppendInt(line, int64(r.Latency), 10)
		line = append(line, '\n')
		if _, err := w.Write(line); err != nil {
			return err
		}
	}
	// Without this, whatever is still in the buffer is silently lost.
	return w.Flush()
}

// CountLinesAfter reads 32 KiB per syscall and lets bytes.Count (which is
// vectorized) find the newlines.
func CountLinesAfter(r io.Reader) (int, error) {
	buf := make([]byte, 32*1024)
	lines := 0
	for {
		n, err := r.Read(buf)
		lines += bytes.Count(buf[:n], []byte{'\n'})
		if err == io.EOF {
			return lines, nil
		}
		if err != nil {
			return lines, err
		}
	}
}
