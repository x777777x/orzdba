//go:build unix && !darwin

package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"

	"orzdba/internal/syscol"
)

// detectCPU returns the logical CPU count from /proc/cpuinfo, falling back to
// runtime.NumCPU() when /proc is unavailable (non-Linux dev hosts). The
// fallback is dev-only; production runs on Linux with /proc.
func detectCPU() int {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err == nil {
		if n := syscol.CountCPU(strings.NewReader(string(data))); n > 0 {
			return n
		}
	}
	return runtime.NumCPU()
}

// checkDiskDevices verifies every device in the list appears in
// /proc/diskstats. It returns nil when all are found, OR when /proc/diskstats
// is unreadable (non-Linux dev hosts have no /proc) — in the latter case we
// can't check, so we skip the startup error and let the collector degrade to
// zeros at runtime (plan §9.7). It returns an error only when /proc/diskstats
// IS readable and some device is absent (plan §11.1).
func checkDiskDevices(devices []string) error {
	data, err := os.ReadFile("/proc/diskstats")
	if err != nil {
		return nil // /proc absent (non-Linux) — skip check, degrade at runtime
	}
	for _, dev := range devices {
		if !findDiskDevice(data, dev) {
			return fmt.Errorf("disk device %q not found in /proc/diskstats", dev)
		}
	}
	return nil
}

// findDiskDevice reports whether dev appears as a device name (field[2]) in
// /proc/diskstats content. Pure so it can be tested with a sample.
func findDiskDevice(data []byte, dev string) bool {
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) >= 3 && f[2] == dev {
			return true
		}
	}
	return false
}

// checkNetDevice verifies the interface appears in /proc/net/dev. Like
// checkDiskDevice, it returns nil when /proc/net/dev is unreadable (non-Linux
// dev hosts) and errors only when /proc IS readable but the interface is
// absent — a genuine bad interface name on Linux (plan §11.1, symmetric with
// disk). P2-4: previously -n silently output zeros forever on a typo.
func checkNetDevice(dev string) error {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return nil // /proc absent (non-Linux) — skip check, degrade at runtime
	}
	if !findNetDevice(data, dev) {
		return fmt.Errorf("network interface %q not found in /proc/net/dev", dev)
	}
	return nil
}

// findNetDevice reports whether dev appears as an interface name in
// /proc/net/dev content (first field, trailing colon stripped).
func findNetDevice(data []byte, dev string) bool {
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if strings.TrimSuffix(f[0], ":") == dev {
			return true
		}
	}
	return false
}
