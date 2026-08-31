//go:build !darwin

package syscol

import (
	"fmt"
	"math"
	"strings"

	"orzdba/internal/metric"
)

// CPU parses /proc/stat. It serves two roles: when -c is enabled it emits cpu
// usage cells; it also exposes per-tick CPU jiffies diffs to the disk
// collector (disk's deltams formula needs user/system/idle/iowait deltas —
// plan §5.2). To avoid reading /proc/stat twice, Sample() is called once per
// tick (by the main loop, when cpu or disk is enabled) and Collect() only
// formats the already-sampled diffs.
//
// First-tick behavior matches the Perl original: prev is zero-initialized so
// the first sample yields since-boot averages (NOT zeros). The Perl code has
// no first-tick guard for cpu.
type CPU struct {
	enabled bool // emit cells when -c is set
	full    bool // --full: emit 9 detail columns instead of 4
	ncpu    int

	// /proc/stat fields [1..7] from the previous tick: user,nice,system,idle,
	// iowait,irq,softirq. Zero on the first tick.
	prev      [7]uint64
	prevTotal uint64

	// Output of the most recent Sample(), consumed by Collect and by disk.
	LastUserDiff float64
	LastSysDiff  float64
	LastIdleDiff float64
	LastIowDiff  float64
	pct          [7]float64 // per-field percentages (0..6): usr,nice,sys,idl,iow,irq,soft
}

// NewCPU returns a CPU collector. enabled controls whether Collect emits
// cells (false when cpu is only sampled to feed disk); full enables the
// 9-column detail output.
func NewCPU(ncpu int, enabled, full bool) *CPU {
	return &CPU{enabled: enabled, full: full, ncpu: ncpu}
}

func (*CPU) Name() string { return "cpu" }

func (c *CPU) Headline() (string, string) {
	if c.full {
		return "------------cpu-usage----------------- ", "  usr nice  sys idle iow  irq soft steal|"
	}
	return "---cpu-usage--- ", "usr sys idl iow|"
}

// Sample reads /proc/stat and updates the collector's state for this tick.
// The main loop calls this exactly once per tick (when cpu or disk is
// enabled).
func (c *CPU) Sample() {
	data, err := readFile("/proc/stat")
	if err != nil {
		return
	}
	c.consume(data)
}

// consume processes one /proc/stat sample (pure logic; testable with golden
// data) and updates the collector's diffs, percentages, and previous state.
func (c *CPU) consume(data []byte) {
	v2, ok := parseCPUStat(data)
	if !ok {
		return
	}
	total2 := v2[0] + v2[1] + v2[2] + v2[3] + v2[4] + v2[5] + v2[6]
	totalDiff := float64(total2) - float64(c.prevTotal)

	// Perl: user_diff = (user+nice)_2 - (user+nice)_1
	c.LastUserDiff = float64(v2[0]+v2[1]) - float64(c.prev[0]+c.prev[1])
	// Perl: system_diff = (sys+irq+softirq)_2 - (sys+irq+softirq)_1
	c.LastSysDiff = float64(v2[2]+v2[5]+v2[6]) - float64(c.prev[2]+c.prev[5]+c.prev[6])
	c.LastIdleDiff = float64(v2[3]) - float64(c.prev[3])
	c.LastIowDiff = float64(v2[4]) - float64(c.prev[4])

	// On the first tick prev is zero, so totalDiff == total2 and the
	// percentages are since-boot averages (matching Perl). Guard the
	// (unreachable-in-practice) zero-denominator case.
	for i := 0; i < 7; i++ {
		if totalDiff == 0 {
			c.pct[i] = 0
		} else {
			c.pct[i] = float64(v2[i]) - float64(c.prev[i])
			c.pct[i] = c.pct[i] / totalDiff * 100
		}
	}
	c.prev = v2
	c.prevTotal = total2
}

// Collect formats the sampled percentages. Returns nil when not enabled so
// the renderer skips this segment. In default mode it emits the Perl-compatible
// 4 columns (usr sys idl iow); with --full it emits 9 columns
// (usr nice sys idle iow irq soft steal). Raw percentages are float values.
func (c *CPU) Collect() []metric.Cell {
	if !c.enabled {
		return nil
	}
	// usr = user+nice (Perl), displayed as first column.
	usr := c.pct[0] + c.pct[1]
	sys := c.pct[2] + c.pct[5] + c.pct[6] // system+irq+softirq (Perl)
	idl := c.pct[3]
	iow := c.pct[4]

	if !c.full {
		return []metric.Cell{
			{Text: fmt.Sprintf("%3d", int(math.Round(usr))), Raw: usr, Color: cpuUsrColor(int(math.Round(usr)))},
			{Text: fmt.Sprintf(" %3d", int(math.Round(sys))), Raw: sys, Color: cpuSysColor(int(math.Round(sys)))},
			{Text: fmt.Sprintf(" %3d", int(math.Round(idl))), Raw: idl, Color: metric.White},
			{Text: fmt.Sprintf(" %3d", int(math.Round(iow))), Raw: iow, Color: cpuIowColor(int(math.Round(iow)))},
		}
	}
	// Full 9 columns: usr nice sys idle iow irq soft steal.
	// steal/guest are beyond the 7 parsed fields — parsed as 0 (matching the
	// Perl original, which only indexes fields [1..7]).
	steal := 0.0
	cells := make([]metric.Cell, 0, 9)
	cells = append(cells,
		metric.Cell{Text: fmt.Sprintf("%5.1f", usr), Raw: usr, Color: cpuUsrColor(int(math.Round(usr)))},
		metric.Cell{Text: fmt.Sprintf("%5.1f", c.pct[1]), Raw: c.pct[1], Color: metric.White},
		metric.Cell{Text: fmt.Sprintf("%5.1f", sys), Raw: sys, Color: cpuSysColor(int(math.Round(sys)))},
		metric.Cell{Text: fmt.Sprintf("%5.1f", idl), Raw: idl, Color: metric.White},
		metric.Cell{Text: fmt.Sprintf("%5.1f", iow), Raw: iow, Color: cpuIowColor(int(math.Round(iow)))},
		metric.Cell{Text: fmt.Sprintf("%5.1f", c.pct[5]), Raw: c.pct[5], Color: metric.White},
		metric.Cell{Text: fmt.Sprintf("%5.1f", c.pct[6]), Raw: c.pct[6], Color: metric.White},
		metric.Cell{Text: fmt.Sprintf("%5.1f", steal), Raw: steal, Color: metric.White},
	)
	return cells
}

// cpuUsrColor: usr>10 RED else GREEN (Perl).
func cpuUsrColor(v int) metric.Color {
	if v > 10 {
		return metric.Red
	}
	return metric.Green
}

// cpuSysColor: sys>10 RED else WHITE (Perl).
func cpuSysColor(v int) metric.Color {
	if v > 10 {
		return metric.Red
	}
	return metric.White
}

// cpuIowColor: iow>10 RED else GREEN (Perl).
func cpuIowColor(v int) metric.Color {
	if v > 10 {
		return metric.Red
	}
	return metric.Green
}

// parseCPUStat parses the first ("cpu ") line of /proc/stat into the seven
// jiffies counters [user,nice,system,idle,iowait,irq,softirq]. The Perl
// original sums fields 1..7 (steal/guest beyond field 7 are ignored, matching
// Perl which only indexes [1]..[7]).
func parseCPUStat(data []byte) ([7]uint64, bool) {
	var vals [7]uint64
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 8 || f[0] != "cpu" {
			continue
		}
		for i := 0; i < 7; i++ {
			vals[i] = parseUint(f[i+1])
		}
		return vals, true
	}
	return vals, false
}
