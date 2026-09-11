package syscol

import "orzdba/internal/metric"

// Platform-shared color thresholds. The darwin and Linux collector files each
// carried byte-identical copies of these functions across the build-tag split;
// they live here once so a threshold change cannot drift between platforms.
// Callers: cpu/load/mem/net/swap/disk collectors on both platforms.

// cpuUsrColor: usr > 10 RED else GREEN (Perl).
func cpuUsrColor(v int) metric.Color {
	if v > 10 {
		return metric.Red
	}
	return metric.Green
}

// cpuSysColor: sys > 10 RED else WHITE (Perl).
func cpuSysColor(v int) metric.Color {
	if v > 10 {
		return metric.Red
	}
	return metric.White
}

// cpuIowColor: iowait > 10 RED else GREEN (Perl).
func cpuIowColor(v int) metric.Color {
	if v > 10 {
		return metric.Red
	}
	return metric.Green
}

// loadColor mirrors Perl: $val > $ncpu ? RED : WHITE — load is judged per-CPU.
func loadColor(v, ncpu float64) metric.Color {
	if v > ncpu {
		return metric.Red
	}
	return metric.White
}

// memUsageColor: >90 RED, >80 YELLOW, else GREEN (Perl-style escalation).
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

// netColor: rate > 1 MiB/s RED else WHITE (Perl, strict >).
func netColor(rate float64) metric.Color {
	if rate/1024/1024 > 1 {
		return metric.Red
	}
	return metric.White
}

// netErrColor: any errors/drops in the window RED else WHITE.
func netErrColor(d float64) metric.Color {
	if d > 0 {
		return metric.Red
	}
	return metric.White
}

// diskBytesColor: KiB/s > 1024 RED else WHITE (Perl).
func diskBytesColor(v float64) metric.Color {
	if v > 1024 {
		return metric.Red
	}
	return metric.White
}

// swapColor: v > 0 RED else WHITE. One predicate, two call semantics by
// platform: Linux passes the raw swap-in/out RATE DELTA (Perl keys on the
// pre-division delta), darwin passes the CURRENT swap usage in bytes (draws
// attention while swap is in use).
func swapColor(v int64) metric.Color {
	if v > 0 {
		return metric.Red
	}
	return metric.White
}
