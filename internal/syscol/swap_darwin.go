//go:build darwin

package syscol

// #include <sys/types.h>
// #include <sys/sysctl.h>
// #include <stdint.h>
import "C"

import (
	"unsafe"

	"orzdba/internal/metric"
	"orzdba/internal/render"
)

// Swap reads vm.swapusage via sysctl. Unlike Linux (which reports si/so
// per-second swap in/out rates from /proc/vmstat), macOS exposes only the
// current swap usage — there is no cumulative page counter. To keep the
// column layout identical, si shows the used swap bytes and so shows the
// available swap bytes. README documents this semantic difference.
type Swap struct{}

// NewSwap returns a swap collector. The interval argument is kept for
// interface parity with Linux (unused on macOS since there is no rate).
func NewSwap(_ int) *Swap {
	return &Swap{}
}

func (*Swap) Name() string { return "swap" }

func (*Swap) Headline() (string, string) {
	return "---swap--- ", "   si   so|"
}

// Collect reads vm.swapusage and formats si/so. First tick emits zeros
// (matching Linux). Subsequent ticks show the current used (si) and available
// (so) swap in bytes.
func (s *Swap) Collect() []metric.Cell {
	total, used, avail, ok := readSwapUsage()
	if !ok || total == 0 {
		return []metric.Cell{
			{Text: fmtBytes(0), Color: metric.White},
			{Text: fmtBytes(0), Color: metric.White},
		}
	}
	return []metric.Cell{
		{Text: fmtBytes(float64(used)), Raw: float64(used), Color: swapColor(int64(used))},
		{Text: fmtBytes(float64(avail)), Raw: float64(avail), Color: metric.White},
	}
}

// readSwapUsage reads vm.swapusage. Returns total, used, avail bytes and ok.
func readSwapUsage() (total, used, avail uint64, ok bool) {
	var xsw C.struct_xsw_usage
	sz := C.size_t(unsafe.Sizeof(xsw))
	if r := C.sysctlbyname(C.CString("vm.swapusage"), unsafe.Pointer(&xsw), &sz, nil, 0); r != 0 {
		return 0, 0, 0, false
	}
	return uint64(xsw.xsu_total), uint64(xsw.xsu_used), uint64(xsw.xsu_avail), true
}

// fmtBytes formats a byte value via render.FormatBytesValue (raw integer).
// Width 8 matches the mem/net byte columns.
func fmtBytes(b float64) string {
	return " " + render.FormatBytesValue(b, metric.UnitRaw, 8, 8)
}

// swapColor: used>0 RED else WHITE (draws attention when swap is in use).
func swapColor(delta int64) metric.Color {
	if delta > 0 {
		return metric.Red
	}
	return metric.White
}
