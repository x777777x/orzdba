//go:build darwin

package main

import (
	"fmt"

	"orzdba/internal/syscol"
)

// detectCPU returns the logical CPU count via sysctl hw.ncpu.
func detectCPU() int {
	return syscol.DarwinNCPU()
}

// checkDiskDevices verifies every device in the list exists as a whole disk
// via IOKit (e.g. "disk0"). Returns nil when all are found or when the IOKit
// data source is unavailable (degrade at runtime); errors only when a named
// device is genuinely absent.
func checkDiskDevices(devices []string) error {
	disks := syscol.DarwinDiskNames()
	for _, dev := range devices {
		if !contains(disks, dev) {
			return fmt.Errorf("disk device %q not found on this macOS host (available: %s)", dev, join(disks, ", "))
		}
	}
	return nil
}

// checkNetDevice verifies the interface appears via getifaddrs. Returns nil
// when present or when the data source is unavailable; errors when the named
// interface is genuinely absent.
func checkNetDevice(dev string) error {
	if !syscol.InterfaceExists(dev) {
		return fmt.Errorf("network interface %q not found on this macOS host", dev)
	}
	return nil
}

// contains reports whether s is in the slice.
func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// join concatenates a slice with sep.
func join(s []string, sep string) string {
	out := ""
	for i, x := range s {
		if i > 0 {
			out += sep
		}
		out += x
	}
	return out
}
