//go:build darwin

package syscol

// #include <sys/types.h>
// #include <sys/sysctl.h>
// #include <mach/mach.h>
// #include <mach/mach_host.h>
// #include <mach/host_info.h>
// #include <stdint.h>
import "C"

import (
	"fmt"
	"math"
	"unsafe"

	"orzdba/internal/metric"
)

// CPU reads host_cpu_load_info via host_statistics. It serves two roles:
// when -c is enabled it emits cpu usage cells; it also exposes per-tick
// jiffies diffs to the disk collector (same contract as the Linux CPU).
//
// macOS's cpu_ticks are cumulative counters (user/nice/system/idle) exactly
// like Linux /proc/stat jiffies, so the diff formula is identical. macOS has
// no iowait/irq/softirq counters, so iow/irq/soft are always 0 (matching the
// Perl original's missing-field behavior).
type CPU struct {
	enabled bool
	full    bool
	ncpu    int

	// Previous tick's counters [user,nice,system,idle]. Zero on the first tick.
	prev [4]uint64
	// Sum of the four counters from the previous tick (for total diff).
	prevTotal uint64

	// Output of the most recent Sample(), consumed by Collect and by disk.
	LastUserDiff float64
	LastSysDiff  float64
	LastIdleDiff float64
	LastIowDiff  float64
	pct          [7]float64 // usr,nice,sys,idl,iow,irq,soft (irq/soft always 0)
}

// NewCPU returns a CPU collector. enabled controls whether Collect emits
// cells; full enables the 9-column detail output.
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

// Sample reads host_cpu_load_info and updates the collector's state for this
// tick. The main loop calls this exactly once per tick (when cpu or disk is
// enabled), matching the Linux contract.
func (c *CPU) Sample() {
	c.consume(readCPULoadInfo())
}

// readCPULoadInfo reads the cumulative CPU ticks via host_statistics.
func readCPULoadInfo() [4]uint64 {
	var info C.host_cpu_load_info_data_t
	var count C.mach_msg_type_number_t = C.HOST_CPU_LOAD_INFO_COUNT
	host := C.mach_host_self()
	if r := C.host_statistics(host, C.HOST_CPU_LOAD_INFO, (*C.integer_t)(unsafe.Pointer(&info)), &count); r != C.KERN_SUCCESS {
		return [4]uint64{}
	}
	// macOS cpu_ticks is a natural_t[CPU_STATE_MAX]; map USER/NICE/SYSTEM/IDLE.
	// irq/softirq/iowait have no macOS equivalent.
	return [4]uint64{
		uint64(info.cpu_ticks[C.CPU_STATE_USER]),
		uint64(info.cpu_ticks[C.CPU_STATE_NICE]),
		uint64(info.cpu_ticks[C.CPU_STATE_SYSTEM]),
		uint64(info.cpu_ticks[C.CPU_STATE_IDLE]),
	}
}

// consume processes one host_cpu_load_info sample (pure-ish; testable with
// synthetic tick arrays) and updates the collector's diffs, percentages, and
// previous state. Mirrors the Linux CPU.consume diff math.
func (c *CPU) consume(v2 [4]uint64) {
	total2 := v2[0] + v2[1] + v2[2] + v2[3]
	totalDiff := float64(total2) - float64(c.prevTotal)

	// user = user+nice (Perl), system = system (no irq/soft on macOS).
	c.LastUserDiff = float64(v2[0]+v2[1]) - float64(c.prev[0]+c.prev[1])
	c.LastSysDiff = float64(v2[2]) - float64(c.prev[2])
	c.LastIdleDiff = float64(v2[3]) - float64(c.prev[3])
	c.LastIowDiff = 0

	// Map into the 7-slot pct layout: [usr,nice,sys,idl,iow,irq,soft].
	// c.prev holds the 4 macOS counters; slots 4..6 (iow/irq/soft) have no
	// macOS counterpart and their previous values are 0.
	if totalDiff == 0 {
		for i := range c.pct {
			c.pct[i] = 0
		}
	} else {
		vals := [7]uint64{v2[0], v2[1], v2[2], v2[3], 0, 0, 0}
		for i := 0; i < 7; i++ {
			var prev uint64
			if i < 4 {
				prev = c.prev[i]
			}
			c.pct[i] = (float64(vals[i]) - float64(prev)) / totalDiff * 100
		}
	}
	c.prev = [4]uint64{v2[0], v2[1], v2[2], v2[3]}
	c.prevTotal = total2
}

// Collect formats the sampled percentages. Same layout as the Linux CPU.
func (c *CPU) Collect() []metric.Cell {
	if !c.enabled {
		return nil
	}
	usr := c.pct[0] + c.pct[1]
	sys := c.pct[2]
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

// cpuUsrColor/cpuSysColor/cpuIowColor mirror the Linux color thresholds.
func cpuUsrColor(v int) metric.Color {
	if v > 10 {
		return metric.Red
	}
	return metric.Green
}
func cpuSysColor(v int) metric.Color {
	if v > 10 {
		return metric.Red
	}
	return metric.White
}
func cpuIowColor(v int) metric.Color {
	if v > 10 {
		return metric.Red
	}
	return metric.Green
}

// DarwinNCPU returns the logical CPU count via sysctl hw.ncpu.
func DarwinNCPU() int {
	var n C.int
	sz := C.size_t(unsafe.Sizeof(n))
	if r := C.sysctlbyname(C.CString("hw.ncpu"), unsafe.Pointer(&n), &sz, nil, 0); r != 0 {
		return 1
	}
	if n <= 0 {
		return 1
	}
	return int(n)
}
