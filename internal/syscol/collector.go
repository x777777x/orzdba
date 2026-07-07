// Package syscol implements /proc-based system collectors: load, cpu, swap,
// net, disk.
//
// Each collector reads /proc directly with the Go standard library — no
// cat/grep/sed/awk shell-out (plan §9.4, fixing orzdba-go P0-2/P2-20). The
// parsing of each /proc file lives in a pure function taking []byte so it can
// be unit-tested against golden samples (plan §12.1).
package syscol

import (
	"bufio"
	"io"
	"os"
	"strconv"
	"strings"

	"orzdba/internal/metric"
)

// Collector is the syscol sampling contract. It matches render.Collector so
// collectors plug directly into the Renderer without adapters.
type Collector interface {
	Name() string
	Headline() (line1, line2 string)
	Collect() []metric.Cell
}

// readFile reads a /proc file fresh on each call. /proc files are small; this
// keeps the per-tick open/read/close pattern (plan §9.4) without holding
// long-lived handles.
func readFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

// CountCPU counts logical CPUs from /proc/cpuinfo by counting lines beginning
// with "processor:" — the Go equivalent of `grep processor /proc/cpuinfo | wc
// -l` in the Perl original, done without forking (plan §9.4).
func CountCPU(r io.Reader) int {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	n := 0
	for sc.Scan() {
		if strings.HasPrefix(sc.Text(), "processor") {
			n++
		}
	}
	if n == 0 {
		return 1 // never divide by zero
	}
	return n
}

// parseUint parses a counter value, returning 0 (not an error) on failure.
// Non-numeric /proc fields should degrade to 0 rather than abort the tick
// (plan §2.4 P2-15, but applied to /proc too).
func parseUint(s string) uint64 {
	v, err := strconv.ParseUint(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// parseInt parses a signed counter, 0 on failure.
func parseInt(s string) int64 {
	v, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	if err != nil {
		return 0
	}
	return v
}

// parseFloat parses a float, 0 on failure.
func parseFloat(s string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return v
}
