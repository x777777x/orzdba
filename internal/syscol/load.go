package syscol

import (
	"fmt"
	"strings"

	"orzdba/internal/metric"
)

// Load reads /proc/loadavg. Load is an instantaneous value (not a diff), so it
// prints real values on every tick including the first.
type Load struct {
	ncpu int
}

// NewLoad returns a load collector; ncpu is the CPU count used for the
// load>ncpu red threshold (matching the Perl original).
func NewLoad(ncpu int) *Load { return &Load{ncpu: ncpu} }

func (*Load) Name() string { return "load" }

// Headline matches the Perl original's load header fragments.
func (*Load) Headline() (string, string) {
	return "-----load-avg---- ", "  1m    5m   15m |"
}

// Collect reads /proc/loadavg and formats the three load averages. Each value
// is RED when it exceeds ncpu, else WHITE — matching Perl's per-value color.
func (l *Load) Collect() []metric.Cell {
	data, err := readFile("/proc/loadavg")
	if err != nil {
		return zeroLoad()
	}
	f := strings.Fields(string(data))
	if len(f) < 3 {
		return zeroLoad()
	}
	l1 := parseFloat(f[0])
	l5 := parseFloat(f[1])
	l15 := parseFloat(f[2])
	ncpu := float64(l.ncpu)
	return []metric.Cell{
		{Text: fmt.Sprintf("%5.2f", l1), Color: loadColor(l1, ncpu)},
		{Text: fmt.Sprintf(" %5.2f", l5), Color: loadColor(l5, ncpu)},
		{Text: fmt.Sprintf(" %5.2f", l15), Color: loadColor(l15, ncpu)},
	}
}

// loadColor mirrors Perl: $val > $ncpu ? RED : WHITE.
func loadColor(v, ncpu float64) metric.Color {
	if v > ncpu {
		return metric.Red
	}
	return metric.White
}

func zeroLoad() []metric.Cell {
	return []metric.Cell{
		{Text: fmt.Sprintf("%5.2f", 0.0), Color: metric.White},
		{Text: fmt.Sprintf(" %5.2f", 0.0), Color: metric.White},
		{Text: fmt.Sprintf(" %5.2f", 0.0), Color: metric.White},
	}
}
