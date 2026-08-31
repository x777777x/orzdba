//go:build !darwin

package syscol

import (
	"fmt"
	"strings"

	"orzdba/internal/metric"
)

// Swap reads /proc/vmstat for pswpin/pswpout and reports per-second rates.
//
// Unlike cpu/disk, swap has an explicit first-tick guard: the Perl original
// prints "0 0" on the first tick (it only diffs when swap_not_first is set).
type Swap struct {
	interval float64
	notFirst bool
	pswpin   uint64
	pswpout  uint64
}

// NewSwap returns a swap collector; interval is the sampling interval in
// seconds used to convert deltas to per-second rates (matching Perl's
// /$interval).
func NewSwap(interval int) *Swap {
	return &Swap{interval: float64(interval)}
}

func (*Swap) Name() string { return "swap" }

func (*Swap) Headline() (string, string) {
	return "---swap--- ", "   si   so|"
}

// Collect reads /proc/vmstat and formats si/so.
func (s *Swap) Collect() []metric.Cell {
	data, err := readFile("/proc/vmstat")
	if err != nil {
		// Read failure (e.g. non-Linux dev host): degrade to zeros.
		return s.consume(nil)
	}
	return s.consume(data)
}

// consume processes one /proc/vmstat sample and formats si/so. On the first
// tick it emits zeros (Perl behavior). Color is RED when the raw delta
// (pre-division) is positive, else WHITE.
func (s *Swap) consume(data []byte) []metric.Cell {
	pswpin, pswpout := parseVMStatSwap(data)
	if !s.notFirst {
		s.pswpin = pswpin
		s.pswpout = pswpout
		s.notFirst = true
		return []metric.Cell{
			{Text: fmt.Sprintf(" %4d", 0), Color: metric.White},
			{Text: fmt.Sprintf(" %4d", 0), Color: metric.White},
		}
	}
	dIn := int64(pswpin) - int64(s.pswpin)
	dOut := int64(pswpout) - int64(s.pswpout)
	s.pswpin = pswpin
	s.pswpout = pswpout
	return []metric.Cell{
		{Text: fmt.Sprintf(" %4d", int(float64(dIn)/s.interval)), Color: swapColor(dIn)},
		{Text: fmt.Sprintf(" %4d", int(float64(dOut)/s.interval)), Color: swapColor(dOut)},
	}
}

// swapColor: delta>0 RED else WHITE (Perl keys on the raw delta, not the rate).
func swapColor(delta int64) metric.Color {
	if delta > 0 {
		return metric.Red
	}
	return metric.White
}

// parseVMStatSwap extracts pswpin and pswpout from /proc/vmstat. Lines look
// like "pswpin 123" / "pswpout 456".
func parseVMStatSwap(data []byte) (pswpin, pswpout uint64) {
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		switch f[0] {
		case "pswpin":
			pswpin = parseUint(f[1])
		case "pswpout":
			pswpout = parseUint(f[1])
		}
	}
	return
}
