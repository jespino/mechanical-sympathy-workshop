package iosyscalls

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
)

const numRecords = 5_000

func testRecords() []Record {
	recs := make([]Record, numRecords)
	for i := range recs {
		recs[i] = Record{ID: i, Name: fmt.Sprintf("svc-%03d", i%250), Latency: (i * 7919) % 100_000}
	}
	return recs
}

// writeToFile runs fn against a fresh file and returns what ended up on disk.
func writeToFile(t *testing.T, fn func(*os.File, []Record) error, recs []Record) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "out.csv")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := fn(f, recs); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSameResults(t *testing.T) {
	recs := testRecords()
	before := writeToFile(t, WriteRecordsBefore, recs)
	after := writeToFile(t, WriteRecordsAfter, recs)
	if !bytes.Equal(before, after) {
		t.Fatalf("WriteRecords: after wrote %d bytes, before wrote %d (did you Flush?)", len(after), len(before))
	}

	for _, impl := range []struct {
		name string
		fn   func(io.Reader) (int, error)
	}{{"before", CountLinesBefore}, {"after", CountLinesAfter}} {
		got, err := impl.fn(bytes.NewReader(before))
		if err != nil {
			t.Fatal(err)
		}
		if got != numRecords {
			t.Errorf("CountLines %s: got %d lines, want %d", impl.name, got, numRecords)
		}
	}
}

func BenchmarkWriteRecords(b *testing.B) {
	recs := testRecords()
	// /dev/null still costs a full write(2) per call, but no disk I/O, so we
	// measure the syscall overhead and not the storage device.
	f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		b.Skip("no", os.DevNull, err)
	}
	defer f.Close()
	for _, impl := range []struct {
		name string
		fn   func(*os.File, []Record) error
	}{{"before", WriteRecordsBefore}, {"after", WriteRecordsAfter}} {
		b.Run("impl="+impl.name, func(b *testing.B) {
			for b.Loop() {
				if err := impl.fn(f, recs); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkCountLines(b *testing.B) {
	// A real file, so every Read is a read(2). After the first pass it sits
	// in the page cache: we measure syscalls, not the disk.
	path := filepath.Join(b.TempDir(), "in.csv")
	f, err := os.Create(path)
	if err != nil {
		b.Fatal(err)
	}
	if err := WriteRecordsBefore(f, testRecords()); err != nil {
		b.Fatal(err)
	}
	f.Close()
	for _, impl := range []struct {
		name string
		fn   func(io.Reader) (int, error)
	}{{"before", CountLinesBefore}, {"after", CountLinesAfter}} {
		b.Run("impl="+impl.name, func(b *testing.B) {
			f, err := os.Open(path)
			if err != nil {
				b.Fatal(err)
			}
			defer f.Close()
			for b.Loop() {
				if _, err := f.Seek(0, io.SeekStart); err != nil {
					b.Fatal(err)
				}
				if _, err := impl.fn(f); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
