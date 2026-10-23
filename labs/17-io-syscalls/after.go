//go:build !solution

package iosyscalls

// This is your file. It starts as a copy of before.go: change it so that it
// makes fewer syscalls, then run the benchmarks to compare against before.go.
// The reference solution is in solution.go (run with -tags solution).

import (
	"fmt"
	"io"
	"os"
)

// WriteRecordsAfter writes one CSV line per record to f.
// TODO: wrap f in a bufio.Writer so many records share one write(2).
// TODO: don't forget to Flush before returning (and check its error).
// TODO (bonus): replace fmt.Fprintf with strconv.AppendInt into a reusable
// []byte, then write that slice to the buffered writer.
func WriteRecordsAfter(f *os.File, recs []Record) error {
	for _, r := range recs {
		if _, err := fmt.Fprintf(f, "%d,%s,%d\n", r.ID, r.Name, r.Latency); err != nil {
			return err
		}
	}
	return nil
}

// CountLinesAfter counts the '\n' bytes in r.
// TODO: read big chunks (a 32 KiB []byte, or a bufio.Reader) instead of one
// byte per syscall, and count with bytes.Count.
func CountLinesAfter(r io.Reader) (int, error) {
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
