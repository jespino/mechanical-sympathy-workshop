package capstone

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

const (
	testLines  = 3_000   // small: the test must be fast, even for before.go
	benchLines = 100_000 // ~14 MB of log
	seed       = 42
)

func analyze(t testing.TB, fn func([]byte) ([]byte, error), data []byte) []byte {
	t.Helper()
	out, err := fn(data)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func run(analyze func(r *bytes.Reader, w *bytes.Buffer) error) func([]byte) ([]byte, error) {
	return func(data []byte) ([]byte, error) {
		var out bytes.Buffer
		err := analyze(bytes.NewReader(data), &out)
		return out.Bytes(), err
	}
}

func TestSameReport(t *testing.T) {
	for _, s := range []uint64{seed, 7, 2026} {
		data := GenerateLog(testLines, s)
		want := analyze(t, run(func(r *bytes.Reader, w *bytes.Buffer) error { return AnalyzeBefore(r, w) }), data)
		got := analyze(t, run(func(r *bytes.Reader, w *bytes.Buffer) error { return AnalyzeAfter(r, w) }), data)
		if !bytes.Equal(want, got) {
			t.Fatalf("seed %d: reports differ\n--- before ---\n%s\n--- after ---\n%s", s, want, got)
		}
	}
}

func TestReportSanity(t *testing.T) {
	data := GenerateLog(testLines, seed)
	var out bytes.Buffer
	if err := AnalyzeBefore(bytes.NewReader(data), &out); err != nil {
		t.Fatal(err)
	}
	report := out.String()
	for _, want := range []string{"lines: 3000 ", "GET /api/users/:id ", "/api/search ", "chrome", "bot"} {
		if !strings.Contains(report, want) {
			t.Errorf("report doesn't contain %q:\n%s", want, report)
		}
	}
	if strings.Contains(report, "malformed: 0 ") {
		t.Errorf("expected some malformed lines:\n%s", report)
	}
}

func BenchmarkAnalyze(b *testing.B) {
	data := GenerateLog(benchLines, seed)
	// Write the report to a real file descriptor, like a program writing to
	// stdout would: /dev/null still costs a write(2) per unbuffered Write.
	out, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		b.Skip(err)
	}
	defer out.Close()
	for _, impl := range []struct {
		name string
		fn   func(*bytes.Reader, *os.File) error
	}{
		{"before", func(r *bytes.Reader, w *os.File) error { return AnalyzeBefore(r, w) }},
		{"after", func(r *bytes.Reader, w *os.File) error { return AnalyzeAfter(r, w) }},
	} {
		b.Run("impl="+impl.name, func(b *testing.B) {
			b.SetBytes(int64(len(data)))
			for b.Loop() {
				if err := impl.fn(bytes.NewReader(data), out); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// TestEdgeCases feeds both implementations lines that the generator never
// produces, to make sure an optimized parser accepts and rejects exactly
// what before.go's regexp and time.Parse do.
func TestEdgeCases(t *testing.T) {
	lines := []string{
		`2026-10-23T12:00:00.000Z GET /a/1 200 10us "curl/8"`,
		`2026-10-23T11:59:59.999Z GET /a/+7/-3/x9?y=1 404 99us "Firefox Bot"`,
		`2026-02-29T12:00:00.000Z GET /leap 200 10us "x"`, // 2026 isn't a leap year
		`2024-02-29T12:00:00.000Z GET /leap 200 10us "x"`,
		`2026-13-01T12:00:00.000Z GET /month 200 10us "x"`,
		`2026-10-23T24:00:00.000Z GET /hour 200 10us "x"`,
		`2026-10-23T12:00:60.000Z GET /sec 200 10us "x"`,
		`2026-10-23T13:00:00+02:00 POST /tz 201 5us "Edg/1 Chrome"`,
		`2026-10-23T12:00:00Z PUT /short 200 5us "Safari"`,
		`not-a-time GET /x 200 5us "x"`,
		`2026-10-23T12:00:00.000Z GET  /double-space 200 5us "x"`,
		"2026-10-23T12:00:00.000Z GET /tab\tx 200 5us \"x\"",
		`2026-10-23T12:00:00.000Z GET /quote 200 5us "a"b"`,
		`2026-10-23T12:00:00.000Z GET /status 20 5us "x"`,
		`2026-10-23T12:00:00.000Z GET /status 2000 5us "x"`,
		`2026-10-23T12:00:00.000Z GET /nolat 200 us "x"`,
		`2026-10-23T12:00:00.000Z GET /ms 200 5ms "x"`,
		`2026-10-23T12:00:00.000Z GET /trailing 200 5us "x" `,
		`2026-10-23T12:00:00.000Z GET /empty-ua 200 5us ""`,
		`2026-10-23T12:00:00.000Z GET /99999999999999999999/big 200 123456789012345678901us "x"`,
		`2026-10-23T12:00:00.000Z GET // 200 5us "x"`,
		`2026-10-23T12:00:00.000Z GET ? 200 5us "x"`,
		`2026-10-23T12:00:00.000Z GET /ünï/çødé 200 5us "ÜA"`,
		``,
		`2026-10-23T12:00:00.000Z GET /last 503 1us "Mozilla Chrome Safari"`,
	}
	data := []byte(strings.Join(lines, "\n") + "\n")
	want := analyze(t, run(func(r *bytes.Reader, w *bytes.Buffer) error { return AnalyzeBefore(r, w) }), data)
	got := analyze(t, run(func(r *bytes.Reader, w *bytes.Buffer) error { return AnalyzeAfter(r, w) }), data)
	if !bytes.Equal(want, got) {
		t.Fatalf("reports differ\n--- before ---\n%s\n--- after ---\n%s", want, got)
	}
}
