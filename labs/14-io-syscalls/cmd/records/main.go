// records writes N CSV records to a file using one of the lab's
// implementations, so you can count the syscalls with strace:
//
//	go build -o /tmp/records ./14-io-syscalls/cmd/records
//	strace -c -f -e trace=write /tmp/records -mode=before
//	strace -c -f -e trace=write /tmp/records -mode=after
//	/tmp/records -mode=noflush   # the classic bufio bug
package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	iosyscalls "labs/14-io-syscalls"
)

func main() {
	mode := flag.String("mode", "before", "before, after or noflush")
	n := flag.Int("n", 5000, "number of records")
	out := flag.String("o", filepath.Join(os.TempDir(), "records.csv"), "output file")
	flag.Parse()

	recs := make([]iosyscalls.Record, *n)
	for i := range recs {
		recs[i] = iosyscalls.Record{ID: i, Name: fmt.Sprintf("svc-%03d", i%250), Latency: (i * 7919) % 100_000}
	}

	f, err := os.Create(*out)
	if err != nil {
		log.Fatal(err)
	}
	switch *mode {
	case "before":
		err = iosyscalls.WriteRecordsBefore(f, recs)
	case "after":
		err = iosyscalls.WriteRecordsAfter(f, recs)
	case "noflush":
		// Buffered, but we "forget" to call w.Flush(). Whatever is left
		// in the buffer when we close the file is lost, with no error.
		w := bufio.NewWriter(f)
		for _, r := range recs {
			fmt.Fprintf(w, "%d,%s,%d\n", r.ID, r.Name, r.Latency)
		}
	default:
		log.Fatalf("unknown mode %q", *mode)
	}
	if err != nil {
		log.Fatal(err)
	}
	if err := f.Close(); err != nil {
		log.Fatal(err)
	}
	st, err := os.Stat(*out)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Fprintf(os.Stderr, "mode=%s: wrote %d records, file is %d bytes (%s)\n", *mode, *n, st.Size(), *out)
}
