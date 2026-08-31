package syscol

import (
	"fmt"
	"strings"

	"orzdba/internal/metric"
	"orzdba/internal/render"
)

// Mem reads /proc/meminfo and reports memory usage. By default it emits a
// single usage% column; with full=true it emits the full field set
// (total/used/free/available/buff/cached), each byte value in Raw as bytes
// (--unit switches the display to k/m/g).
//
// Usage% = (MemTotal - MemAvailable) / MemTotal * 100, using MemAvailable
// (Linux 3.14+). On kernels without MemAvailable, falls back to
// MemFree + Buffers + Cached as "available" (Perl-era approximation).
type Mem struct {
	full bool
	unit metric.UnitMode
}

// NewMem returns a memory collector. full enables the full field set; unit
// controls byte presentation (Raw numbers vs k/m/g).
func NewMem(full bool, unit metric.UnitMode) *Mem {
	return &Mem{full: full, unit: unit}
}

func (*Mem) Name() string { return "mem" }

func (m *Mem) Headline() (string, string) {
	if m.full {
		return "-----------------memory---------------- ", " usage   total    used    free    avail    buff  cached|"
	}
	return "---mem--- ", " usage|"
}

// memInfo holds the parsed /proc/meminfo fields we need (in kB).
type memInfo struct {
	total, free, available, buffers, cached uint64
	ok                                      bool
}

// parseMemInfo extracts the memory fields from /proc/meminfo. Values are in kB.
func parseMemInfo(data []byte) memInfo {
	var m memInfo
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		v := parseUint(f[1])
		switch f[0] {
		case "MemTotal:":
			m.total = v
			m.ok = true
		case "MemFree:":
			m.free = v
		case "MemAvailable:":
			m.available = v
		case "Buffers:":
			m.buffers = v
		case "Cached:":
			m.cached = v
		}
	}
	return m
}

// memUsage computes usage% (0 when total is 0).
func (m memInfo) usage() float64 {
	if m.total == 0 {
		return 0
	}
	avail := m.available
	if avail == 0 { // no MemAvailable (pre-3.14): approximate with free+buff+cached
		avail = m.free + m.buffers + m.cached
	}
	if avail > m.total {
		avail = m.total
	}
	return float64(m.total-avail) / float64(m.total) * 100
}

// Collect reads /proc/meminfo and formats the memory columns. Raw bytes are
// MemTotal*kB*1024 style; usage is a percentage.
func (m *Mem) Collect() []metric.Cell {
	data, err := readFile("/proc/meminfo")
	if err != nil {
		return m.consume(nil)
	}
	return m.consume(data)
}

func (m *Mem) consume(data []byte) []metric.Cell {
	info := parseMemInfo(data)
	if !info.ok || info.total == 0 {
		return []metric.Cell{{Text: fmt.Sprintf(" %6.1f", 0.0), Raw: 0, Color: metric.White}}
	}
	usage := info.usage()
	// bytes = kB * 1024
	totalB := float64(info.total) * 1024
	usedB := float64(info.total-info.free) * 1024
	freeB := float64(info.free) * 1024
	availB := float64(info.total) * 1024 * (1 - usage/100)
	buffB := float64(info.buffers) * 1024
	cachedB := float64(info.cached) * 1024

	if !m.full {
		return []metric.Cell{
			{Text: fmt.Sprintf(" %6.1f", usage), Raw: usage, Color: memUsageColor(usage)},
		}
	}
	// Full: usage total used free available buff cached.
	// Each byte column is right-aligned with a leading space so columns stay
	// visually separated in BOTH raw and human modes (raw integers can exceed
	// the nominal width; the leading space prevents run-on concatenation).
	return []metric.Cell{
		{Text: fmt.Sprintf(" %6.1f", usage), Raw: usage, Color: memUsageColor(usage)},
		{Text: " " + render.FormatBytesValue(totalB, m.unit, 8, 8), Raw: totalB, Color: metric.White},
		{Text: " " + render.FormatBytesValue(usedB, m.unit, 8, 8), Raw: usedB, Color: metric.White},
		{Text: " " + render.FormatBytesValue(freeB, m.unit, 8, 8), Raw: freeB, Color: metric.White},
		{Text: " " + render.FormatBytesValue(availB, m.unit, 8, 8), Raw: availB, Color: metric.White},
		{Text: " " + render.FormatBytesValue(buffB, m.unit, 8, 8), Raw: buffB, Color: metric.White},
		{Text: " " + render.FormatBytesValue(cachedB, m.unit, 8, 8), Raw: cachedB, Color: metric.White},
	}
}

// memUsageColor: >90 red, >80 yellow, else green (Perl-style escalation).
func memUsageColor(v float64) metric.Color {
	switch {
	case v > 90:
		return metric.Red
	case v > 80:
		return metric.Yellow
	default:
		return metric.Green
	}
}
