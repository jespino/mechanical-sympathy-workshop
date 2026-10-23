// Package iosyscalls is the lab for module 17: I/O and syscalls.
//
// before.go holds the naive implementations. Don't edit this file: it is the
// baseline that after.go is benchmarked against.
package iosyscalls

import (
	"fmt"
	"io"
	"os"
)

// Record is one row of a metrics export.
type Record struct {
	ID      int
	Name    string
	Latency int // microseconds
}

// WriteRecordsBefore writes one CSV line per record straight to the file.
// Every fmt.Fprintf call ends in its own write(2) syscall: *os.File has no
// buffer of its own.
func WriteRecordsBefore(f *os.File, recs []Record) error {
	for _, r := range recs {
		if _, err := fmt.Fprintf(f, "%d,%s,%d\n", r.ID, r.Name, r.Latency); err != nil {
			return err
		}
	}
	return nil
}

// CountLinesBefore counts the '\n' bytes in r, reading one byte at a time.
// When r is an *os.File, every Read is a read(2) syscall.
func CountLinesBefore(r io.Reader) (int, error) {
	var one [1]byte
	lines := 0
	for {
		n, err := r.Read(one[:])
		if n == 1 && one[0] == '\n' {
			lines++
		}
		if err == io.EOF {
			return lines, nil
		}
		if err != nil {
			return lines, err
		}
	}
}
