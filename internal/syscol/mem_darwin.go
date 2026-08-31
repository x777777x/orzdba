//go:build darwin

package syscol

// #include <sys/types.h>
// #include <sys/sysctl.h>
// #include <mach/mach.h>
// #include <mach/mach_host.h>
// #include <mach/host_info.h>
// #include <mach/vm_statistics.h>
// #include <stdint.h>
import "C"

import (
	"fmt"
	"unsafe"

	"orzdba/internal/metric"
	"orzdba/internal/render"
)

// Mem reads memory via hw.memsize + host_statistics64. By default it emits a
// single usage% column; with full=true it emits the full field set
// (total/used/free/available/buff/cached).
//
// macOS memory semantics differ from Linux /proc/meminfo:
//   - total = hw.memsize (physical bytes)
//   - used  = (active + wired) * page_size (purgeable counted within active)
//   - free  = free_count * page_size
//   - available = (free + inactive) * page_size (approximation of MemAvailable)
//   - buff  = 0 (no direct equivalent); cached = inactive * page_size
type Mem struct {
	full bool
	unit metric.UnitMode
}

// NewMem returns a memory collector. full enables the full field set; unit
// controls byte presentation.
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

// memInfo holds the parsed macOS memory fields (in bytes).
type memInfo struct {
	total, used, free, available, buff, cached uint64
	ok                                         bool
}

// collectMemInfo reads hw.memsize + host_statistics64 and computes the fields.
func collectMemInfo() memInfo {
	var m memInfo

	// total physical memory
	var memsize C.uint64_t
	sz := C.size_t(unsafe.Sizeof(memsize))
	if r := C.sysctlbyname(C.CString("hw.memsize"), unsafe.Pointer(&memsize), &sz, nil, 0); r != 0 {
		return m
	}
	m.total = uint64(memsize)

	// page count statistics
	var vmstat C.vm_statistics64_data_t
	var count C.mach_msg_type_number_t = C.HOST_VM_INFO64_COUNT
	host := C.mach_host_self()
	if r := C.host_statistics64(host, C.HOST_VM_INFO64, (*C.integer_t)(unsafe.Pointer(&vmstat)), &count); r != C.KERN_SUCCESS {
		return m
	}
	// vm_statistics64 has no page_size field; the page size comes from
	// hw.pagesize (typically 16384 on Apple Silicon).
	var ps C.uint64_t
	sz = C.size_t(unsafe.Sizeof(ps))
	if r := C.sysctlbyname(C.CString("hw.pagesize"), unsafe.Pointer(&ps), &sz, nil, 0); r != 0 {
		return m
	}
	page := uint64(ps)
	free := uint64(vmstat.free_count) * page
	active := uint64(vmstat.active_count) * page
	inactive := uint64(vmstat.inactive_count) * page
	wired := uint64(vmstat.wire_count) * page

	m.free = free
	m.used = active + wired
	if m.used > m.total {
		m.used = m.total
	}
	m.available = free + inactive
	if m.available > m.total {
		m.available = m.total
	}
	m.buff = 0
	m.cached = inactive
	m.ok = true
	return m
}

// usage computes usage% (0 when total is 0).
func (m memInfo) usage() float64 {
	if m.total == 0 {
		return 0
	}
	return float64(m.used) / float64(m.total) * 100
}

// Collect reads macOS memory stats and formats the columns.
func (m *Mem) Collect() []metric.Cell {
	info := collectMemInfo()
	if !info.ok || info.total == 0 {
		return []metric.Cell{{Text: fmt.Sprintf(" %6.1f", 0.0), Raw: 0, Color: metric.White}}
	}
	usage := info.usage()

	if !m.full {
		return []metric.Cell{
			{Text: fmt.Sprintf(" %6.1f", usage), Raw: usage, Color: memUsageColor(usage)},
		}
	}
	// Full: usage total used free available buff cached.
	// Byte values as float64 for the renderer; buff is always 0.
	return []metric.Cell{
		{Text: fmt.Sprintf(" %6.1f", usage), Raw: usage, Color: memUsageColor(usage)},
		{Text: " " + render.FormatBytesValue(float64(info.total), m.unit, 8, 8), Raw: float64(info.total), Color: metric.White},
		{Text: " " + render.FormatBytesValue(float64(info.used), m.unit, 8, 8), Raw: float64(info.used), Color: metric.White},
		{Text: " " + render.FormatBytesValue(float64(info.free), m.unit, 8, 8), Raw: float64(info.free), Color: metric.White},
		{Text: " " + render.FormatBytesValue(float64(info.available), m.unit, 8, 8), Raw: float64(info.available), Color: metric.White},
		{Text: " " + render.FormatBytesValue(float64(info.buff), m.unit, 8, 8), Raw: float64(info.buff), Color: metric.White},
		{Text: " " + render.FormatBytesValue(float64(info.cached), m.unit, 8, 8), Raw: float64(info.cached), Color: metric.White},
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
