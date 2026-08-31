//go:build darwin

package syscol

// #include <sys/types.h>
// #include <sys/sysctl.h>
// #include <stdint.h>
import "C"

import (
	"fmt"
	"unsafe"

	"orzdba/internal/metric"
)

// struct loadavg mirrors <sys/sysctl.h>. ldavg are fixpt_t fixed-point values
// scaled by fscale (typically 2048), NOT doubles. The Perl original reads
// /proc/loadavg on Linux; macOS exposes the same three load averages via
// sysctl vm.loadavg.
type loadavg struct {
	ldavg [3]C.fixpt_t
	fscale C.long
}

// Load reads vm.loadavg via sysctl. Load is an instantaneous value, so it
// prints real values on every tick including the first (matching the Linux
// implementation).
type Load struct {
	ncpu int
}

// NewLoad returns a load collector; ncpu is the CPU count used for the
// load>ncpu red threshold.
func NewLoad(ncpu int) *Load { return &Load{ncpu: ncpu} }

func (*Load) Name() string { return "load" }

// Headline matches the Linux load header fragments.
func (*Load) Headline() (string, string) {
	return "-----load-avg---- ", "  1m    5m   15m |"
}

// Collect reads vm.loadavg via sysctl and formats the three load averages.
// Each value is RED when it exceeds ncpu, else WHITE.
func (l *Load) Collect() []metric.Cell {
	la, ok := readLoadavg()
	if !ok {
		return zeroLoad()
	}
	ncpu := float64(l.ncpu)
	return []metric.Cell{
		{Text: fmt.Sprintf("%5.2f", la[0]), Color: loadColor(la[0], ncpu)},
		{Text: fmt.Sprintf(" %5.2f", la[1]), Color: loadColor(la[1], ncpu)},
		{Text: fmt.Sprintf(" %5.2f", la[2]), Color: loadColor(la[2], ncpu)},
	}
}

// readLoadavg reads the system load averages. Returns the three loads and ok.
func readLoadavg() ([3]float64, bool) {
	var la loadavg
	sz := C.size_t(unsafe.Sizeof(la))
	if r := C.sysctlbyname(C.CString("vm.loadavg"), unsafe.Pointer(&la), &sz, nil, 0); r != 0 {
		return [3]float64{}, false
	}
	if la.fscale == 0 {
		return [3]float64{}, false
	}
	scale := float64(int64(la.fscale))
	return [3]float64{
		float64(int32(la.ldavg[0])) / scale,
		float64(int32(la.ldavg[1])) / scale,
		float64(int32(la.ldavg[2])) / scale,
	}, true
}

// loadColor mirrors Perl: $val > $ncpu ? RED : WHITE. Defined in the Linux
// load.go for !darwin; duplicated here to keep darwin files self-contained.
func loadColor(v, ncpu float64) metric.Color {
	if v > ncpu {
		return metric.Red
	}
	return metric.White
}

// zeroLoad returns three zero-valued load cells (used on read failure),
// matching the Linux implementation.
func zeroLoad() []metric.Cell {
	return []metric.Cell{
		{Text: fmt.Sprintf("%5.2f", 0.0), Color: metric.White},
		{Text: fmt.Sprintf(" %5.2f", 0.0), Color: metric.White},
		{Text: fmt.Sprintf(" %5.2f", 0.0), Color: metric.White},
	}
}
